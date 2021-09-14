# Project 4: Transactions

In the previous projects, you have built a key/value database which, by using Raft, is consistent across multiple nodes. To be truly scalable, the database must be able to handle multiple clients. With multiple clients, there is a problem: what happens if two clients try to write the same key at 'the same time'? If a client writes and then immediately reads that key, should they expect the read value to be the same as the written value? In project4, you will address such issues by building a transaction system into our database.

> 在以前的项目中，您已经构建了一个键/值数据库，通过使用Raft，该数据库在多个节点上保持一致。要实现真正的可伸缩性，数据库必须能够处理多个客户端。对于多个客户端，有一个问题：如果两个客户端试图“同时”写入同一个Key，会发生什么情况？如果客户端写入然后立即读取该Key，他们是否应该期望读取值与写入值相同？在project4中，您将通过在我们的数据库中构建一个事务系统来解决这些问题。

The transaction system will be a collaborative protocol between the client (TinySQL) and server (TinyKV). Both partners must be implemented correctly for the transactional properties to be ensured. We will have a complete API for transactional requests, independent of the raw request that you implemented in project1 (in fact, if a client uses both raw and transactional APIs, we cannot guarantee the transactional properties).

>事务系统是客户端(TinySQL)和服务器(TinyKV)之间的协作协议。必须正确实现这两者，才能确保事务属性。我们将为事务性请求提供一个完整的API，独立于您在project1中实现的原始请求（事实上，如果客户端同时使用原始API和事务性API，我们无法保证事务性属性）。

Transactions  promise [*snapshot isolation* (SI)](https://en.wikipedia.org/wiki/Snapshot_isolation). That means that within a transaction, a client will read from the database as if it were frozen in time at the start of the transaction (the transaction sees a *consistent* view of the database). Either all of a transaction is written to the database or none of it is (if it conflicts with another transaction).

> 事务承诺快照隔离（SI）。这意味着在一个事务中，客户机将从数据库中读取数据，就像它在事务开始时被冻结一样（事务会看到数据库的一致视图）。要么将所有事务写入数据库，要么不写入数据库（如果与另一个事务冲突）。

To provide SI, you need to change the way that data is stored in the backing storage. Rather than storing a value for each key, you'll store a value for a key and a time (represented by a timestamp). This is called multi-version concurrency control (MVCC) because multiple different versions of a value are stored for every key.

> 要提供SI，您需要更改备份存储器中存储数据的方式。您将存储一个键和一个时间（由时间戳表示）的值，而不是为每个键存储一个值。这称为多版本并发控制（MVCC），因为每个键都存储了一个值的多个不同版本。

You'll implement MVCC in part A. In parts B and C you’ll implement the transactional API.

> 您将在A部分中实现MVCC。在B和C部分中，您将实现事务API。

## Transactions in TinyKV

TinyKV's transaction design follows [Percolator](https://storage.googleapis.com/pub-tools-public-publication-data/pdf/36726.pdf); it is a two-phase commit protocol (2PC).

> TinyKV的事务设计遵循Percolator；它是一个两阶段提交协议（2PC）。

A transaction is a list of reads and writes. A transaction has a start timestamp and, when a transaction is committed, it has a commit timestamp (which must be greater than the starting timestamp). The whole transaction reads from the version of a key that is valid at the start timestamp. After committing, all writes appear to have been written at the commit timestamp. Any key to be written must not be written by any other transaction between the start and commit timestamps, otherwise, the whole transaction is canceled (this is called a write conflict).

> 事务是读取和写入的列表。事务具有开始时间戳，并且在提交事务时，它具有提交时间戳（必须大于开始时间戳）。整个事务从在开始时间戳有效的key版本中读取。提交后，所有写入操作似乎都已在提交时间戳处写入。任何要写入的key都不能由开始和提交时间戳之间的任何其他事务写入，否则，整个事务将被取消（这称为写入冲突）。

The protocol starts with the client getting a start timestamp from TinyScheduler. It then builds the transaction locally, reading from the database (using a `KvGet` or `KvScan` request which includes the start timestamp, in contrast to  `RawGet` or `RawScan` requests), but only recording writes locally in memory. Once the transaction is built, the client will select one key as the *primary key* (note that this has nothing to do with an SQL primary key). The client sends `KvPrewrite` messages to TinyKV. A `KvPrewrite` message contains all the writes in the transaction. A TinyKV server will attempt to lock all keys required by the transaction. If locking any key fails, then TinyKV responds to the client that the transaction has failed. The client can retry the transaction later (i.e., with a different start timestamp). If all keys are locked, the prewrite succeeds. Each lock stores the primary key of the transaction and a time to live (TTL).

> 该协议从客户端获取TinyScheduler中的开始时间戳开始。然后，它在本地构建事务，从数据库中读取（使用包含开始时间戳的`KvGet`或`KvScan`请求，与`RawGet`或`RawScan`请求不同），但只在内存中本地记录写操作。构建事务后，**客户端将选择一个键作为主键**（请注意，这与SQL主键无关）。客户端向TinyKV发送KvPrewrite消息。KvPrewrite消息包含事务中的所有写入。TinyKV服务器将尝试锁定事务所需的所有key。如果锁定任何key失败，TinyKV会向客户端响应事务失败。客户端可以稍后重试事务（即，使用不同的开始时间戳）。如果所有key都已锁定，则预写成功。每个锁存储事务的主键和生存时间（TTL）。

In fact, since the keys in a transaction may be in multiple regions and thus be stored in different Raft groups, the client will send multiple `KvPrewrite` requests, one to each region leader. Each prewrite contains only the modifications for that region. If all prewrites succeed, then the client will send a commit request for the region containing the primary key. The commit request will contain a commit timestamp (which the client also gets from TinyScheduler) which is the time at which the transaction's writes are committed and thus become visible to other transactions.

> 事实上，由于事务中的key可能位于多个region，因此存储在不同的RaftGroup中，因此客户端将发送多个KvPrewrite请求，每个region leader一个。每个预写仅包含该区域的修改。如果所有预写都成功，那么客户端将向包含primary key的region发送commit请求。commit请求将包含一个提交时间戳（客户端也从TinyScheduler获取），该时间戳是提交事务写入的时间，因此对其他事务可见。

If any prewrite fails, then the transaction is rolled back by the client by sending a `KvBatchRollback` request to all regions (to unlock all keys in the transaction and remove any prewritten values).

> 如果任何预写失败，则客户端将通过向所有region发送KvBatchRollback请求回滚事务（以解锁事务中的所有key并删除任何预写值)。

In TinyKV, TTL checking is not performed spontaneously. To initiate a timeout check, the client sends the current time to TinyKV in a `KvCheckTxnStatus` request. The request identifies the transaction by its primary key and start timestamp. The lock may be missing or already be committed; if not, TinyKV compares the TTL on the lock with the timestamp in the `KvCheckTxnStatus` request. If the lock has timed out, then TinyKV rolls back the lock. In any case, TinyKV responds with the status of the lock so that the client can take action by sending a `KvResolveLock` request. The client typically checks transaction status when it fails to prewrite a transaction due to another transaction's lock.

> 在TinyKV中，存活时间检查不是自发执行的。要启动超时检查，客户端将当前时间通过KvCheckTxnStatus请求发送到TinyKV。请求通过其主键和开始时间戳标识事务。锁可能丢失或已提交；如果没有，TinyKV将锁上的TTL与KvCheckTxnStatus请求中的时间戳进行比较。如果锁超时，TinyKV将回滚锁。在任何情况下，TinyKV都会以锁的状态进行响应，以便客户端可以通过发送KvResolveLock请求来采取行动。当由于另一个事务的锁而无法预写事务时，客户端通常会检查事务状态。

If the primary key commit succeeds, then the client will commit all other keys in the other regions. These requests should always succeed because by responding positively to a prewrite request, the server is promising that if it gets a commit request for that transaction then it will succeed. Once the client has all its prewrite responses, the only way for the transaction to fail is if it times out, and in that case, committing the primary key should fail. Once the primary key is committed, then the other keys can no longer time out.

> 如果主键提交成功，那么客户端将提交其他区域中的所有其他键。这些请求应该总是成功的，因为通过积极响应预写请求，服务器承诺，如果它获得该事务的提交请求，那么它将成功。一旦客户端有了所有的预写的成功响应，事务失败的唯一方法就是超时，在这种情况下，提交主键应该失败。一旦提交了主键，其他键就不能再超时。（因为如果超时了，主键也会超时，主键也提交不了）

If the primary commit fails, then the client will rollback the transaction by sending `KvBatchRollback` requests.

> 如果主键提交失败，则客户端将通过发送KvBatchRollback请求回滚事务。

## Part A

The raw API you implemented in earlier projects maps user keys and values directly to keys and values stored in the underlying storage (Badger). Since Badger is not aware of the distributed transaction layer, you must handle transactions in TinyKV, and *encode* user keys and values into the underlying storage. This is implemented using multi-version concurrency control (MVCC). In this project, you will implement the MVCC layer in TinyKV.

> 您在项目的前面部分中实现的原始API将用户key和value直接映射到存储在基础存储（Badger）中的key和value。由于Badger不知道分布式事务层，因此必须在TinyKV中处理事务，并将用户key和value编码到底层存储中。这是使用多版本并发控制（MVCC）实现的。在本项目中，您将在TinyKV中实现MVCC层。

Implementing MVCC means representing the transactional API using a simple key/value API. Rather than store one value per key, TinyKV stores every version of a value for each key. For example, if a key has value `10`, then gets set to `20`, TinyKV will store both values (`10` and `20`) along with timestamps for when they are valid.

> 实现MVCC意味着使用简单的key/value API表示事务API。TinyKV不是为每个键存储一个值，而是为每个键存储每个版本的值。例如，如果一个键的值为10，然后set为20，TinyKV将存储这两个值（10和20）以及它们生效的时间戳。

TinyKV uses three column families (CFs): `default` to hold user values, `lock` to store locks, and `write` to record changes. The `lock` CF is accessed using the user key; it stores a serialized `Lock` data structure (defined in [lock.go](/kv/transaction/mvcc/lock.go)). The `default` CF is accessed using the user key and the *start* timestamp of the transaction in which it was written; it stores the user value only. The `write` CF is accessed using the user key and the *commit* timestamp of the transaction in which it was written; it stores a `Write` data structure (defined in [write.go](/kv/transaction/mvcc/write.go)).

> TinyKV使用三个列族（CFs）：`default`用于保存用户值、`lock`以存储锁和`write`以记录更改。使用用户key访问`lock`CF；它存储一个序列化的`Lock`数据结构（在`Lock.go`中定义）。使用用户key和写入CF的事务的开始时间戳访问`default`CF；它只存储用户值。使用用户key和写入CF的事务的提交时间戳访问`write`CF；它存储一个`Write`数据结构（在`write.go`中定义）。

A user key and timestamp are combined into an *encoded key*. Keys are encoded in such a way that the ascending order of encoded keys orders first by user key (ascending), then by timestamp (descending). This ensures that iterating over encoded keys will give the most recent version first. Helper functions for encoding and decoding keys are defined in [transaction.go](/kv/transaction/mvcc/transaction.go).

> 用户key和timestamp被组合成一个encoded key。密钥的编码方式是，编码密钥先按用户密钥（升序）排序，然后按时间戳（降序）排序。这确保了对编码密钥的迭代将首先给出最新版本。`transaction.go`中定义了用于编码和解码key的辅助函数

This exercise requires implementing a single struct called `MvccTxn`. In parts B and C, you'll use the `MvccTxn` API to implement the transactional API. `MvccTxn` provides read and write operations based on the user key and logical representations of locks, writes, and values. Modifications are collected in `MvccTxn` and once all modifications for a command are collected, they will be written all at once to the underlying database. This ensures that commands succeed or fail atomically. Note that an MVCC transaction is not the same as a TinySQL transaction. An MVCC transaction contains the modifications for a single command, not a sequence of commands.

> 本练习需要实现一个名为`MvccTxn`的单独结构。在第B部分和第C部分中，您将使用`MvccTxn`API来实现事务API。`MvccTxn`提供基于用户key和锁、写和值的逻辑表示的读写操作。修改将在MvccTxn中收集，一旦收集到命令的所有修改，它们将立即写入底层数据库。这可以确保命令在原子上成功或失败。请注意，MVCC事务与TinySQL事务不同。MVCC事务包含对单个命令的修改，而不是对命令序列的修改。

`MvccTxn` is defined in [transaction.go](/kv/transaction/mvcc/transaction.go). There is a stub implementation, and some helper functions for encoding and decoding keys. Tests are in [transaction_test.go](/kv/transaction/mvcc/transaction_test.go). For this exercise, you should implement each of the `MvccTxn` methods so that all tests pass. Each method is documented with its intended behavior.

> MvccTxn在`transaction.go`中定义。有一个存根实现，以及一些用于编码和解码key的帮助函数。测试用例在`transaction_test.go`中。对于本练习，您应该实现每个`MvccTxn`方法，以便所有测试都通过。每种方法都注释清楚了其预期行为。

> Hints:
>
> - An `MvccTxn` should know the start timestamp of the request it is representing.
> - The most challenging methods to implement are likely to be `GetValue` and the methods for retrieving writes. You will need to use `StorageReader` to iterate over a CF. Bear in mind the ordering of encoded keys, and remember that when deciding when a value is valid depends on the commit timestamp, not the start timestamp, of a transaction.
>
> > - MvccTxn应该知道它所表示的请求的开始时间戳
> > - 要实现的最具挑战性的方法可能是`GetValue`和用于检索写操作的方法。您将需要使用`StorageReader`在CF上进行迭代。请记住编码键的顺序，并记住，在决定值何时有效时，取决于事务的提交时间戳，而不是开始时间戳。

## Part B

In this part, you will use `MvccTxn` from part A to implement handling of `KvGet`, `KvPrewrite`, and `KvCommit` requests. As described above, `KvGet` reads a value from the database at a supplied timestamp. If the key to be read is locked by another transaction at the time of the `KvGet` request, then TinyKV should return an error. Otherwise, TinyKV must search the versions of the key to find the most recent, valid value.

`KvPrewrite` and `KvCommit` write values to the database in two stages. Both requests operate on multiple keys, but the implementation can handle each key independently.

`KvPrewrite` is where a value is actually written to the database. A key is locked and a value stored. We must check that another transaction has not locked or written to the same key.

`KvCommit` does not change the value in the database, but it does record that the value is committed. `KvCommit` will fail if the key is not locked or is locked by another transaction.

You'll need to implement the `KvGet`, `KvPrewrite`, and `KvCommit` methods defined in [server.go](/kv/server/server.go). Each method takes a request object and returns a response object, you can see the contents of these objects by looking at the protocol definitions in [kvrpcpb.proto](/proto/kvrpcpb.proto) (you shouldn't need to change the protocol definitions).

TinyKV can handle multiple requests concurrently, so there is the possibility of local race conditions. For example, TinyKV might receive two requests from different clients at the same time, one of which commits a key, and the other rolls back the same key. To avoid race conditions, you can *latch* any key in the database. This latch works much like a per-key mutex. One latch covers all CFs. [latches.go](/kv/transaction/latches/latches.go) defines a `Latches` object which provides API for this.

> Hints:
>
> - All commands will be a part of a transaction. Transactions are identified by a start timestamp (aka start version).
> - Any request might cause a region error, these should be handled in the same way as for the raw requests. Most responses have a way to indicate non-fatal errors for situations like a key being locked. By reporting these to the client, it can retry a transaction after some time.

## Part C

In this part, you will implement `KvScan`, `KvCheckTxnStatus`, `KvBatchRollback`, and `KvResolveLock`. At a high-level, this is similar to part B - implement the gRPC request handlers in [server.go](/kv/server/server.go) using `MvccTxn`.

`KvScan` is the transactional equivalent of `RawScan`, it reads many values from the database. But like `KvGet`, it does so at a single point in time. Because of MVCC, `KvScan` is significantly more complex than `RawScan` - you can't rely on the underlying storage to iterate over values because of multiple versions and key encoding.

`KvCheckTxnStatus`, `KvBatchRollback`, and `KvResolveLock` are used by a client when it encounters some kind of conflict when trying to write a transaction. Each one involves changing the state of existing locks.

`KvCheckTxnStatus` checks for timeouts, removes expired locks and returns the status of the lock.

`KvBatchRollback` checks that a key is locked by the current transaction, and if so removes the lock, deletes any value and leaves a rollback indicator as a write.

`KvResolveLock` inspects a batch of locked keys and either rolls them all back or commits them all.

> Hints:
>
> - For scanning, you might find it helpful to implement your own scanner (iterator) abstraction which iterates over logical values, rather than the raw values in underlying storage. `kv/transaction/mvcc/scanner.go` is a framework for you.
> - When scanning, some errors can be recorded for an individual key and should not cause the whole scan to stop. For other commands, any single key causing an error should cause the whole operation to stop.
> - Since `KvResolveLock` either commits or rolls back its keys, you should be able to share code with the `KvBatchRollback` and `KvCommit` implementations.
> - A timestamp consists of a physical and a logical component. The physical part is roughly a monotonic version of wall-clock time. Usually, we use the whole timestamp, for example when comparing timestamps for equality. However, when calculating timeouts, we must only use the physical part of the timestamp. To do this you may find the `PhysicalTime` function in [transaction.go](/kv/transaction/mvcc/transaction.go) useful.
