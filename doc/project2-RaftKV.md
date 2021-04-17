# Project2 RaftKV

Raft is a consensus algorithm that is designed to be easy to understand. You can read material about the Raft itself at [the Raft site](https://raft.github.io/), an interactive visualization of the Raft, and other resources, including [the extended Raft paper](https://raft.github.io/raft.pdf).

> Raft是一种易于理解的共识算法。你可以在raft的网站阅读有关raft本身的资料，一个互动的可视化的raft，以及其他资源，包括论文。

In this project, you will implement a high available kv server based on raft,  which needs you not only to implement the Raft algorithm but also use it practically, and bring you more challenges like managing raft’s persisted state with `badger`, add flow control for snapshot message, etc.

>在本项目中，您将实现一个基于raft的高可用kv服务器，它不仅需要您实现raft算法，还需要您实际使用它，并给您带来更多的挑战，如使用badger管理raft的持久状态，添加快照消息的流控制等。

The project has 3 parts you need to do, including:

- Implement the basic Raft algorithm
- Build a fault-tolerant KV server on top of Raft
- Add the support of raftlog GC and snapshot

>项目有三个部分你需要做，包括:
>
>- 实现基本的raft算法
>- 在raft之上构建一个容错KV服务器
>- 增加对raftlog GC和快照的支持

## Part A

### The Code

In this part, you will implement the basic raft algorithm. The code you need to implement is under `raft/`. Inside `raft/`, there are some skeleton code and test cases waiting for you. The raft algorithm you're gonna implement here has a well-designed interface with the upper application. Moreover, it uses a logical clock (named tick here) to measure the election and heartbeat timeout instead of a physical clock. That is to say, do not set a timer in the Raft module itself, and the upper application is responsible to advance the logical clock by calling `RawNode.Tick()`. Apart from that, messages sending and receiving along with other things are processed asynchronously, it is also up to the upper application when to actually do these things (see below for more detail). For example, Raft will not block waiting on the response of any request message.

> 在这一部分中，您将实现基本的raft算法。你需要实现的代码在`raft/`下。在`raft/`内，有一些框架代码和测试用例等着你。你要在这里实现的raft算法与上层应用程序之间有一个设计良好的接口。此外，它使用**逻辑时钟**(这里称为tick)来测量选举超时和心跳超时，而不是物理时钟。也就是说，不要在Raft模块本身设置计时器，上层应用程序通过调用RawNode.Tick()负责推进逻辑时钟。除此之外，**消息的发送和接收以及其他事情都是异步处理的，何时真正做这些事情也取决于上面的应用程序**(参见下面的详细信息)。例如，Raft不会阻塞对任何请求消息响应的等待。

Before implementing, please checkout the hints for this part first. Also, you should take a rough look at the proto file `proto/proto/eraftpb.proto`. Raft sends and receives messages and related structs are defined there, you’ll use them for the implementation. Note that, unlike Raft paper, it divides Heartbeat and AppendEntries into different messages to make the logic more clear.

> 在执行之前，请先检查这部分的提示。另外，您应该大致查看一下proto文件`proto/proto/eraftpb`。raft发送和接收消息和相关的结构是在那里定义的，你将使用它们。需要主要的是，与raft原文不同，本实现将`HeartBeat`和`AppendEntries`分成不同的消息来使逻辑更清晰

This part can be broken down into 3 steps, including:

- Leader election
- Log replication
- Raw node interface

> 这部分可以分为3个步骤，包括:
>
> - Leader选举
> - 日志复制
> - Raw节点接口

### Implement the Raft algorithm

`raft.Raft` in `raft/raft.go` provides the core of the Raft algorithm including message handling, driving the logic clock, etc. For more implementation guides, please check `raft/doc.go` which contains an overview design and what these `MessageTypes` are responsible for.

>`raft.Raft` in `raft/raft.go`提供了Raft算法的核心，包括消息处理、驱动逻辑时钟等。更多实现指南，请查看`raft/doc.go`其中包含概述设计以及这些`messageTypes`负责什么。

#### Leader election

To implement leader election, you may want to start with `raft.Raft.tick()` which is used to advance the internal logical clock by a single tick and hence drive the election timeout or heartbeat timeout. You don’t need to care about the message sending and receiving logic now. If you need to send out a message,  just push it to `raft.Raft.msgs` and all messages the raft received will be passed to `raft.Raft.Step()`. The test code will get the messages from `raft.Raft.msgs` and pass response messages through `raft.Raft.Step()`. The `raft.Raft.Step()` is the entrance of message handling, you should handle messages like `MsgRequestVote`, `MsgHeartbeat` and their response. And please also implement test stub functions and get them called properly like `raft.Raft.becomeXXX` which is used to update the raft internal state when the raft’s role changes.

>要实现leader选举，您可能想要从`raft.Raft.tick()`开始，它用于将内部逻辑时钟提前一个tick，从而驱动选举超时或心跳超时。
>
>您现在不需要关心消息发送和接收逻辑。如果你需要发送消息，只需把它推到`raft.Raft.msgs`，上传收到的所有raft相关信息都会通过调用`raft.Raft.step()`告诉Raft实现。
>
>测试代码将从`raft.Raft.msgs`中获取消息，并通过`raft.Raft.Step()`传递消息。`raft.Raft.Step()`是消息处理的入口，你应该处理像`MsgRequestVote`, `MsgHeartbeat`这样的消息。
>
>并请实现测试stub函数并正确地调用它们，比如`raft.Raft.becomeXXX`，它用于在raft的角色改变时更新raft的内部状态。

You can run `make project2aa` to test the implementation and see some hints at the end of this part.

> 您可以运行`make project2aa`来测试实现，并在本部分末尾看到一些提示。

#### Log replication

To implement log replication, you may want to start with handling `MsgAppend` and `MsgAppendResponse` on both the sender and receiver sides. Checkout `raft.RaftLog` in `raft/log.go` which is a helper struct that helps you manage the raft log, in here you also need to interact with the upper application by the `Storage` interface defined in `raft/storage.go` to get the persisted data like log entries and snapshot.

> 要实现日志复制，您可能需要从处理发送方和接收方的`MsgAppend`和`MsgAppendResponse`开始。 你可以查看`raft/log.go`中的`raft.RaftLog` ，它是帮助你管理raft日志的辅助结构。在这里，您还需要通过`raft/Storage`中定义的`Storage`接口与上层应用程序交互，来获取像日志条目和快照等已持久化的数据。

You can run `make project2ab` to test the implementation and see some hints at the end of this part.

> 您可以运行make project2ab来测试实现，并在本部分末尾看到一些提示。

### Implement the raw node interface

`raft.RawNode` in `raft/rawnode.go` is the interface we interact with the upper application, `raft.RawNode` contains `raft.Raft` and provide some wrapper functions like `RawNode.Tick()`and `RawNode.Step()`. It also provides `RawNode.Propose()` to let the upper application propose new raft logs.

>`raft/rawNode.go`中的`raft.RawNode`是我们与上层应用通信的接口，`raft.RawNode`包含了`raft.Raft`并且提供了一些像`RawNode.Tick()`和`RawNode.Step()`之类的包装函数。它还提供了`RawNode.Propose`来让上层应用提供新的raft日志

Another important struct `Ready` is also defined here. When handling messages or advancing the logical clock, the `raft.Raft` may need to interact with the upper application, like:

- send messages to other peers
- save log entries to stable storage
- save hard state like the term, commit index, and vote to stable storage
- apply committed log entries to the state machine
- etc

> 另一个重要结构`Ready`也在这里定义了。当处理消息或者推进逻辑时钟时，`raft.Raft`可能需要和上层应用通信，比如
>
> - 给其他peers发送消息
> - 保存log entries到持久化存储
> - 保存hard state像 the term, commit index, and vote到持久化存储
> - 应用已提交日志到状态机
> - 等等

But these interactions do not happen immediately, instead, they are encapsulated in `Ready` and returned by `RawNode.Ready()` to the upper application. It is up to the upper application when to call `RawNode.Ready()` and handle it.  After handling the returned `Ready`, the upper application also needs to call some functions like `RawNode.Advance()` to update `raft.Raft`'s internal state like the applied index, stabled log index, etc.

> 但是这些交互不会立即发生，相反，它们被封装在`Ready`中，并由`RawNode.Ready()`返回给上层应用程序。何时调用`RawNode.Ready()`并处理它取决于上面的应用程序。处理完返回的`Ready`后，上面的应用程序还需要调用一些函数，如`RawNode.Advance()`来更新raft的内部状态like the applied 
> index, stabled log index, etc。

You can run `make project2ac` to test the implementation and run `make project2a` to test the whole part A.

> 您可以运行`make project2ac`来测试实现，运行`make project2a`来测试整个A部分。

> Hints:
>
> - Add any state you need to `raft.Raft`, `raft.RaftLog`, `raft.RawNode` and message on `eraftpb.proto`
> - The tests assume that the first time start raft should have term 0
> - The tests assume that the newly elected leader should append a noop entry on its term
> - The tests doesn’t set term for the local messages, `MessageType_MsgHup`, `MessageType_MsgBeat` and `MessageType_MsgPropose`.
> - The log entries append are quite different between leader and non-leader, there are different sources, checking and handling, be careful with that.
> - Don’t forget the election timeout should be different between peers.
> - Some wrapper functions in `rawnode.go` can implement with `raft.Step(local message)`
> - When starting a new raft, get the last stabled state from `Storage` to initialize `raft.Raft` and `raft.RaftLog`

>Hints:
>
>- Add any state you need to `raft.Raft`, `raft.RaftLog`, `raft.RawNode` and message on `eraftpb.proto` 可以向这几个结构体中加任何你需要的状态，可以向pb中加你需要的消息类型
>- 假定第一次启动raft应有term 0
>- 假定新当选的领导人应在其任期内附加noop entry(no op没操作)
>- 没有为本地消息设置term， `MessageType_MsgHup`, `MessageType_MsgBeat` and `MessageType_MsgPropose`.
>- leader和non-leader的**日志条目的附加**有很大的不同，有不同的来源，检查和处理，要小心。
>- 别忘了选举超时时间应该是不同的。
>- Some wrapper functions in `rawnode.go` can implement with `raft.Step(local message)`
>- When starting a new raft, get the last stabled state from `Storage` to initialize `raft.Raft` and `raft.RaftLog` 当启动一个新的raft时，可以从`Storage`中获取上次退出时的持久化状态来初始`raft.Raft`和`raft.RaftLog`

### 看代码总结

raft本身不负责直接发送rpc消息，也不负责直接写到持久化，也不负责应用到状态机

客户会调用`step()`来让raft干各种事情

客户会定期调用`ready()`获取raft的内部状态来干各种事情

别人New的时候New的是指针



> 最终我们可以确认的是Go语言中所有的传参都是值传递（传值），都是一个副本，一个拷贝。因为拷贝的内容有时候是非引用类型（int、string、struct等这些），这样就在函数中就无法修改原内容数据；有的是引用类型（指针、map、slice、chan等这些），这样就可以修改原内容数据。



> 可以看到三个的区别
>
> 1. 传struct是传值，把原数据做完整拷贝，作为参数传递给callee
> 2. 传pointer，传递的是原数据的一个指针，从而在callee里面如果对原数据做了改动，会反映到caller
> 3. 传interface，传递的是一个interface对象，这个对象占用16字节长度，包含一个指向原数据的指针，和一个指向运行时类型信息的指针



内部消息是啥意思的理解

```
// 'MessageType_MsgHup' is a local message used for election. If an election timeout happened,
// the node should pass 'MessageType_MsgHup' to its Step method and start a new election.
```

也就是说`tick`方法会检查内部的时钟，超时的时候会去自己调用`Step(MessageType_MsgHup)`

然后根据它的说法`tick`在不同角色下`tick`的东西也不太一样，分别去更新`heartbeatElapsed`和`electionElapsed` 

## Part B

In this part, you will build a fault-tolerant key-value storage service using the Raft module implemented in part A.  Your key/value service will be a replicated state machine, consisting of several key/value servers that use Raft for replication. Your key/value service should continue to process client requests as long as a majority of the servers are alive and can communicate, despite other failures or network partitions.

> 在本部分中，您将使用a部分中实现的Raft模块构建一个容错的键值存储服务。你的键/值服务将是一个复制的状态机，由几个使用Raft进行复制的键/值服务器组成。只要大多数服务器都是活的并且可以通信，您的key/value服务应该继续处理客户端请求，不管其他故障或网络分区。

In project1 you have implemented a standalone kv server, so you should already be familiar with the kv server API and `Storage` interface.  

> 在project1中，您已经实现了一个独立的kv服务器，因此您应该已经熟悉了kv服务器API和`Storage`接口。

Before introducing the code, you need to understand three terms first: `Store`, `Peer` and `Region` which are defined in `proto/proto/metapb.proto`.

- Store stands for an instance of tinykv-server
- Peer stands for a Raft node which is running on a Store
- Region is a collection of Peers, also called Raft group

![region](imgs/region.png)

For simplicity, there would be only one Peer on a Store and one Region in a cluster for project2. So you don’t need to consider the range of Region now. Multiple regions will be further introduced in project3.

### The Code

First, the code that you should take a look at is  `RaftStorage` located in `kv/storage/raft_storage/raft_server.go` which also implements the `Storage` interface. Unlike `StandaloneStorage` which directly writes or reads from the underlying engine, it first sends every write or read request to Raft, and then does actual write and read to the underlying engine after Raft commits the request. Through this way, it can keep the consistency between multiple Stores.

`RaftStorage` mainly creates a `Raftstore` to drive Raft.  When calling the `Reader` or `Write` function,  it actually sends a `RaftCmdRequest` defined in `proto/proto/raft_cmdpb.proto` with four basic command types(Get/Put/Delete/Snap) to raftstore by channel(the receiver of the channel is `raftCh` of  `raftWorker`) and returns the response after Raft commits and applies the command. And the `kvrpc.Context` parameter of `Reader` and `Write` function is useful now, it carries the Region information from the perspective of the client and is passed as the header of  `RaftCmdRequest`. Maybe the information is wrong or stale, so raftstore needs to check them and decide whether to propose the request.

Then, here comes the core of TinyKV — raftstore. The structure is a little complicated, you can read the reference of TiKV to give you a better understanding of the design:

- <https://pingcap.com/blog-cn/the-design-and-implementation-of-multi-raft/#raftstore>  (Chinese Version)
- <https://pingcap.com/blog/2017-08-15-multi-raft/#raftstore> (English Version)

The entrance of raftstore is `Raftstore`, see `kv/raftstore/raftstore.go`.  It starts some workers to handle specific tasks asynchronously,  and most of them aren’t used now so you can just ignore them. All you need to focus on is `raftWorker`.(kv/raftstore/raft_worker.go)

The whole process is divided into two parts: raft worker polls `raftCh` to get the messages, the messages include the base tick to drive Raft module and Raft commands to be proposed as Raft entries; it gets and handles ready from Raft module, including send raft messages, persist the state, apply the committed entries to the state machine. Once applied, return the response to clients.

### Implement peer storage

Peer storage is what you interact with through the `Storage` interface in part A, but in addition to the raft log, peer storage also manages other persisted metadata which is very important to restore the consistent state machine after a restart. Moreover, there are three important states defined in `proto/proto/raft_serverpb.proto`:

- RaftLocalState: Used to store HardState of the current Raft and the last Log Index.
- RaftApplyState: Used to store the last Log index that Raft applies and some truncated Log information.
- RegionLocalState: Used to store Region information and the corresponding Peer state on this Store. Normal indicates that this Peer is normal, Applying means this Peer hasn’t finished the apply snapshot operation and Tombstone shows that this Peer has been removed from Region and cannot join in Raft Group.

These states are stored in two badger instances: raftdb and kvdb:

- raftdb stores raft log and `RaftLocalState`
- kvdb stores key-value data in different column families, `RegionLocalState` and `RaftApplyState`. You can regard kvdb as the state machine mentioned in Raft paper

The format is as below and some helper functions are provided in `kv/raftstore/meta`, and set them to badger with `writebatch.SetMeta()`.

| Key            | KeyFormat                      | Value          | DB |
|:----           |:----                           |:----           |:---|
|raft_log_key    |0x01 0x02 region_id 0x01 log_idx|Entry           |raft|
|raft_state_key  |0x01 0x02 region_id 0x02        |RaftLocalState  |raft|
|apply_state_key |0x01 0x02 region_id 0x03        |RaftApplyState  |kv  |
|region_state_key|0x01 0x03 region_id 0x01        |RegionLocalState|kv  |

> You may wonder why TinyKV needs two badger instances. Actually, it can use only one badger to store both raft log and state machine data. Separating into two instances is just to be consistent with TiKV design.

These metadatas should be created and updated in `PeerStorage`. When creating PeerStorage, see `kv/raftstore/peer_storage.go`. It initializes RaftLocalState, RaftApplyState of this Peer, or gets the previous value from the underlying engine in the case of restart. Note that the value of both RAFT_INIT_LOG_TERM and RAFT_INIT_LOG_INDEX is 5 (as long as it's larger than 1) but not 0. The reason why not set it to 0 is to distinguish with the case that peer created passively after conf change. You may not quite understand it now, so just keep it in mind and the detail will be described in project3b when you are implementing conf change.

The code you need to implement in this part is only one function:  `PeerStorage.SaveReadyState`, what this function does is to save the data in `raft.Ready` to badger, including append log entries and save the Raft hard state.

To append log entries, simply save all log entries at `raft.Ready.Entries` to raftdb and delete any previously appended log entries which will never be committed. Also, update the peer storage’s `RaftLocalState` and save it to raftdb.

To save the hard state is also very easy, just update peer storage’s `RaftLocalState.HardState` and save it to raftdb.

> Hints:
>
> - Use `WriteBatch` to save these states at once.
> - See other functions at `peer_storage.go` for how to read and write these states.

### Implement Raft ready process

In project2 part A, you have built a tick-based Raft module. Now you need to write the outer process to drive it. Most of the code is already implemented under `kv/raftstore/peer_msg_handler.go` and `kv/raftstore/peer.go`.  So you need to learn the code and finish the logic of `proposeRaftCommand` and `HandleRaftReady`. Here are some interpretations of the framework.

The Raft `RawNode` is already created with `PeerStorage` and stored in `peer`. In the raft worker, you can see that it takes the `peer` and wraps it by `peerMsgHandler`.  The `peerMsgHandler` mainly has two functions: one is `HandleMsgs` and the other is `HandleRaftReady`.

`HandleMsgs` processes all the messages received from raftCh, including `MsgTypeTick` which calls `RawNode.Tick()`  to drive the Raft, `MsgTypeRaftCmd` which wraps the request from clients and `MsgTypeRaftMessage` which is the message transported between Raft peers. All the message types are defined in `kv/raftstore/message/msg.go`. You can check it for detail and some of them will be used in the following parts.

After the message is processed, the Raft node should have some state updates. So `HandleRaftReady` should get the ready from Raft module and do corresponding actions like persisting log entries, applying committed entries
and sending raft messages to other peers through the network.

In a pseudocode, the raftstore uses Raft like:

``` go
for {
  select {
  case <-s.Ticker:
    Node.Tick()
  default:
    if Node.HasReady() {
      rd := Node.Ready()
      saveToStorage(rd.State, rd.Entries, rd.Snapshot)
      send(rd.Messages)
      for _, entry := range rd.CommittedEntries {
        process(entry)
      }
      s.Node.Advance(rd)
    }
}
```

After this the whole process of a read or write would be like this:

- Clients calls RPC RawGet/RawPut/RawDelete/RawScan
- RPC handler calls `RaftStorage` related method
- `RaftStorage` sends a Raft command request to raftstore, and waits for the response
- `RaftStore` proposes the Raft command request as a Raft log
- Raft module appends the log, and persist by `PeerStorage`
- Raft module commits the log
- Raft worker executes the Raft command when handing Raft ready, and returns the response by callback
- `RaftStorage` receive the response from callback and returns to RPC handler
- RPC handler does some actions and returns the RPC response to clients.

You should run `make project2b` to pass all the tests. The whole test is running a mock cluster including multiple TinyKV instances with a mock network. It performs some read and write operations and checks whether the return values are as expected.

To be noted, error handling is an important part of passing the test. You may have already noticed that there are some errors defined in `proto/proto/errorpb.proto` and the error is a field of the gRPC response. Also, the corresponding errors which implement the`error` interface are defined in `kv/raftstore/util/error.go`, so you can use them as a return value of functions.

These errors are mainly related to Region. So it is also a member of `RaftResponseHeader` of `RaftCmdResponse`. When proposing a request or applying a command, there may be some errors. If that, you should return the raft command response with the error, then the error will be further passed to gRPC response. You can use `BindErrResp` provided in `kv/raftstore/cmd_resp.go` to convert these errors to errors defined in `errorpb.proto` when returning the response with an error.

In this stage, you may consider these errors, and others will be processed in project3:

- ErrNotLeader: the raft command is proposed on a follower. so use it to let the client try other peers.
- ErrStaleCommand: It may due to leader changes that some logs are not committed and overrided with new leaders’ logs. But the client doesn’t know that and is still waiting for the response. So you should return this to let the client knows and retries the command again.

> Hints:
>
> - `PeerStorage` implements the `Storage` interface of the Raft module, you should use the provided method  `SaveRaftReady()` to persist the Raft related states.
> - Use `WriteBatch` in `engine_util` to make multiple writes atomically, for example, you need to make sure to apply the committed entries and update the applied index in one write batch.
> - Use `Transport` to send raft messages to other peers, it’s in the `GlobalContext`,
> - The server should not complete a get RPC if it is not part of a majority and does not has up-to-date data. You can just put the get operation into the raft log, or implement the optimization for read-only operations that is described in Section 8 in the Raft paper.
> - Do not forget to update and persist the apply state when applying the log entries.
> - You can apply the committed Raft log entries in an asynchronous way just like TiKV does. It’s not necessary, though a big challenge to improve performance.
> - Record the callback of the command when proposing, and return the callback after applying.
> - For the snap command response, should set badger Txn to callback explicitly.

## Part C

As things stand now with your code, it's not practical for a long-running server to remember the complete Raft log forever. Instead, the server will check the number of Raft log, and discard log entries exceeding the threshold from time to time.

> 从现在代码的情况来看，让一个长时间运行的服务器永远记住完整的Raft日志是不现实的。相反，服务器将检查raft日志的数量，并不时丢弃超过阈值的日志条目。

In this part, you will implement the Snapshot handling based on the above two part implementation. Generally, Snapshot is just a raft message like AppendEntries used to replicate data to followers, what makes it different is its size, Snapshot contains the whole state machine data at some point of time, and to build and send such a big message at once will consume many resource and time, which may block the handling of other raft messages, to amortize this problem, Snapshot message will use an independent connect, and split the data into chunks to transport. That’s the reason why there is a snapshot RPC API for TinyKV service. If you are interested in the detail of sending and receiving, check `snapRunner` and the reference <https://pingcap.com/blog-cn/tikv-source-code-reading-10/>

> 在本部分中，您将基于上述两部分实现实现快照处理。

### The Code

All you need to change is based on the code written in part A and part B.

### Implement in Raft

Although we need some different handling for Snapshot messages, in the perspective of raft algorithm there should be no difference. See the definition of `eraftpb.Snapshot` in the proto file, the `data` field on `eraftpb.Snapshot` does not represent the actual state machine data but some metadata is used for the upper application you can ignore it for now. When the leader needs to send a Snapshot message to a follower, it can call `Storage.Snapshot()` to get a `eraftpb.Snapshot`, then send the snapshot message like other raft messages. How the state machine data is actually built and sent are implemented by the raftstore, it will be introduced in the next step. You can assume that once `Storage.Snapshot()` returns successfully, it’s safe for Raft leader to the snapshot message to the follower, and follower should call `handleSnapshot` to handle it, which namely just restore the raft internal state like the term, commit index and membership information, etc, from the
`eraftpb.SnapshotMetadata` in the message, after that, the procedure of snapshot handling is finish.

### Implement in raftstore

In this step, you need to learn two more workers of raftstore — raftlog-gc worker and region worker.

Raftstore checks whether it needs to gc log from time to time based on the config `RaftLogGcCountLimit`, see `onRaftGcLogTick()`. If yes, it will propose a raft admin command `CompactLogRequest` which is wrapped in `RaftCmdRequest` just like four basic command types(Get/Put/Delete/Snap) implemented in project2 part B. Then you need to process this admin command when it’s committed by Raft. But unlike Get/Put/Delete/Snap commands write or read state machine data, `CompactLogRequest` modifies metadata, namely updates the `RaftTruncatedState` which is in the `RaftApplyState`. After that, you should schedule a task to raftlog-gc worker by `ScheduleCompactLog`. Raftlog-gc worker will do the actual log deletion work asynchronously.

Then due to the log compaction, Raft module maybe needs to send a snapshot. `PeerStorage` implements `Storage.Snapshot()`. TinyKV generates snapshots and applies snapshots in the region worker. When calling `Snapshot()`, it actually sends a task `RegionTaskGen` to the region worker. The message handler of the region worker is located in `kv/raftstore/runner/region_task.go`. It scans the underlying engines to generate a snapshot, and sends snapshot metadata by channel. At the next time Raft calling `Snapshot`, it checks whether the snapshot generating is finished. If yes, Raft should send the snapshot message to other peers, and the snapshot sending and receiving work is handled by `kv/storage/raft_storage/snap_runner.go`. You don’t need to dive into the details, only should know the snapshot message will be handled by `onRaftMsg` after the snapshot is received.

Then the snapshot will reflect in the next Raft ready, so the task you should do is to modify the raft ready process to handle the case of a snapshot. When you are sure to apply the snapshot, you can update the peer storage’s memory state like `RaftLocalState`, `RaftApplyState`, and `RegionLocalState`. Also, don’t forget to persist these states to kvdb and raftdb and remove stale state from kvdb and raftdb. Besides, you also need to update
`PeerStorage.snapState` to `snap.SnapState_Applying` and send `runner.RegionTaskApply` task to region worker through `PeerStorage.regionSched` and wait until region worker finish.

You should run `make project2c` to pass all the tests.
