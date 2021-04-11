# Project1 StandaloneKV

In this project, you will build a standalone key/value storage [gRPC](https://grpc.io/docs/guides/) service with the support of the column family. Standalone means only a single node, not a distributed system. [Column family]( <https://en.wikipedia.org/wiki/Standard_column_family> ), it will abbreviate to CF below) is a term like key namespace, namely the values of the same key in different column families is not the same. You can simply regard multiple column families as separate mini databases. It’s used to support the transaction model in the project4, you will know why TinyKV needs the support of CF then.

>在这个项目中，您将在列族的支持下构建一个standalone的键/值存储gRPC服务。standalone意味着只有一个节点，而不是一个分布式系统。Column family(下面将缩写为CF)是一个类似键名称空间的术语，即不同列族中相同键的值是不相同的。您可以简单地将多个列族视为单独的迷你数据库。CF在project4中是用来支持事务模型的，那么您就会知道为什么TinyKV需要CF的支持了。

The service supports four basic operations: Put/Delete/Get/Scan. It maintains a simple database of key/value pairs. Keys and values are strings. `Put` replaces the value for a particular key for the specified CF in the database, `Delete` deletes the key's value for the specified CF, `Get` fetches the current value for a key for the specified CF, and `Scan` fetches the current value for a series of keys for the specified CF.

> 该服务支持4种基本操作:Put/Delete/Get/Scan。它维护一个简单的键/值对数据库。键和值都是字符串。
>
> - `Put`替换数据库中指定CF的特定键的值
> - `Delete`删除指定CF的键的值
> - `Get`指定CF的键的当前值
> - `Scan`获取指定CF的一系列键的当前值

The project can be broken down into 2 steps, including:

1. Implement a standalone storage engine.
2. Implement raw key/value service handlers.

> 项目可分为两个步骤，包括:
>
> 1. 实现一个standalone的存储引擎。
> 2. 实现原始的键/值服务处理器。

### The Code

The `gRPC` server is initialized in `kv/main.go` and it contains a `tinykv.Server` which provides a `gRPC` service named `TinyKv`. It was defined by [protocol-buffer]( https://developers.google.com/protocol-buffers ) in `proto/proto/tinykvpb.proto`, and the detail of rpc requests and responses are defined in `proto/proto/kvrpcpb.proto`.

> `gRPC`服务器在`kv/main.go`中被初始化，并且它包含了一个`tinykv.Server`，它提供一个叫做`TinyKv`的`gRPC`服务。它用pb被定义在`proto/proto/tinykvpb.proto`中，并且rpc请求和回复的细节定义在了`proto/proto/kvrpcpb.proto`

Generally, you don’t need to change the proto files because all necessary fields have been defined for you. But if you still need to change, you can modify the proto file and run `make proto` to update related generated go code in `proto/pkg/xxx/xxx.pb.go`.

>通常，你不需要更改proto文件，因为已经为您定义了所有必要的字段。如果还需要修改，可以修改proto文件，然后运行make  proto命令更新`proto/pkg/xxx/xxx.pb`中生成的相关go代码

In addition, `Server` depends on a `Storage`, an interface you need to implement for the standalone storage engine located in `kv/storage/standalone_storage/standalone_storage.go`. Once the interface `Storage` is implemented in `StandaloneStorage`, you could implement the raw key/value service for the `Server` with it.

> 此外，`Server`依赖于一个`Storage`，这是一个standalone存储引擎，你需要实现它，它被放置在`kv/storage/standalone_storage/standalone_storage.go`。当你实现了`Storage`之后，可以为`Server`提供raw kv服务。

#### Implement standalone storage engine 实现standalone存储引擎

The first mission is implementing a wrapper of [badger](https://github.com/dgraph-io/badger) key/value API. The service of gRPC server depends on an `Storage` which is defined in `kv/storage/storage.go`. In this context, the standalone storage engine is just a wrapper of badger key/value API which is provided by two methods:

>第一个任务是实现对badger kv api的封装。The service of gRPC server depends on an `Storage` which is defined in `kv/storage/storage.go`.在这种情况下，独立存储引擎只是一个badger kv API的包装，它由两个方法提供:

``` go
type Storage interface {
    // Other stuffs
    Write(ctx *kvrpcpb.Context, batch []Modify) error
    Reader(ctx *kvrpcpb.Context) (StorageReader, error)
}
```

`Write` should provide a way that applies a series of modifications to the inner state which is, in this situation, a badger instance.

> `Write`应该提供一种方法，对内部状态(在这种情况下，是一个badger实例)应用一系列修改

`Reader` should return a `StorageReader` that supports key/value's point get and scan operations on a snapshot.

> `Reader`应该返回一个支持键/值点获取和快照扫描操作的StorageReader
>
> tips: 就是返回一个`Reader`以支持`get`和`scan`

And you don’t need to consider the `kvrpcpb.Context` now, it’s used in the following projects.

> 你现在不需要考虑`kvrpcpb.Context`，它将在以下项目中使用。

> Hints:
>
> - You should use [badger.Txn]( https://godoc.org/github.com/dgraph-io/badger#Txn ) to implement the `Reader` function because the transaction handler provided by badger could provide a consistent snapshot of the keys and values.
> - Badger doesn’t give support for column families. engine_util package (`kv/util/engine_util`) simulates column families by adding a prefix to keys. For example, a key `key` that belongs to a specific column family `cf` is stored as `${cf}_${key}`. It wraps `badger` to provide operations with CFs, and also offers many useful helper functions. So you should do all read/write operations through `engine_util` provided methods. Please read `util/engine_util/doc.go` to learn more.
> - TinyKV uses a fork of the original version of `badger` with some fix, so just use `github.com/Connor1996/badger` instead of `github.com/dgraph-io/badger`.
> - Don’t forget to call `Discard()` for badger.Txn and close all iterators before discarding.

>Hints:
>
>- 你应该用`badger.Txn`来实现`Reader`函数，因为badger提供的事务处理程序可以提供键和值的一致快照。
>- `Badger`不支持列族。engine_util包(`kv/util/engine_util`)通过向键添加前缀来模拟列族。例如，一个属于特定列族`cf`的键键被存储为`${cf}_${key}`。它封装了`badger`以使用CFs提供操作，还提供了许多有用的`helper`函数。因此，您应该通过engine_util提供的方法来执行所有的读/写操作。请阅读`util/engine_util/doc.go` 去了解更多。
>- TinyKV uses a fork of the original version of `badger` with some fix, so just use `github.com/Connor1996/badger` instead of `github.com/dgraph-io/badger`.
>- 不要忘记为`badger.Txn`调用`Discard()`，并在丢弃之前关闭所有迭代器。

#### Implement service handlers

The final step of this project is to use the implemented storage engine to build raw key/value service handlers including RawGet/ RawScan/ RawPut/ RawDelete. The handler is already defined for you, you only need to fill up the implementation in `kv/server/server.go`. Once done, remember to run `make project1` to pass the test suite.

> 本项目的最后一步是使用实现的存储引擎来构建raw键/值服务处理程序，包括RawGet/ rawcan / RawPut/ RawDelete。处理程序已经为您定义，您只需要填写`kv/server/server.go`中的实现。一旦完成，请记住运行`make project1`以通过测试组。

