# The TinyKV Course

This is a series of projects on a key-value storage system built with the Raft consensus algorithm. These projects are inspired by the famous [MIT 6.824](http://nil.csail.mit.edu/6.824/2018/index.html) course but aim to be closer to industry implementations. The whole course is pruned from [TiKV](https://github.com/tikv/tikv) and re-written in Go. After completing this course, you will have the knowledge to implement a horizontally scalable, highly available, key-value storage service with distributed transaction support and a better understanding of TiKV implementation.

> 这是一系列关于用Raft共识算法构建的键值存储系统的项目。这些项目的灵感来自著名的MIT6.824课程，但目标是更接近行业实施。整个课程从TiKV中拿出来，用go重新编写。完成本课程后，您将有知识实现水平可扩展，高可用性，键值存储服务与分布式事务支持，并更好地理解TiKV实现。
>
> 

The whole project is a skeleton code for a kv server and a scheduler server at the beginning, and you need to finish the core logic step by step:

- Project1: build a standalone key-value server
- Project2: build a highly available key-value server with Raft
- Project3: support multi Raft group and balance scheduling on top of Project2
- Project4: support distributed transaction on top of Project3

>整个项目一开始是一个kv服务器和一个调度器服务器的框架代码，你需要一步一步完成核心逻辑:
>
>- Project1:构建一个独立的键值服务器
>- Project2:用Raft构建一个高可用的键值服务器
>- Project3:在项目2上支持多个raft组和平衡调度
>- Project4:在Project3之上支持分布式事务

**Important note: This course is still under development, and the documentation is incomplete.** Any feedback and contribution is greatly appreciated. Please see help wanted issues if you want to join in the development.



## Course

Here is a [reading list](doc/reading_list.md) for the knowledge of distributed storage system. Though not all of them are highly related with this course, they can help you construct the knowledge system in this field.

> 以下是分布式存储系统相关知识的阅读清单。虽然不是所有的都与本课程密切相关，但它们可以帮助你构建这个领域的知识体系。

Also, you’d better read the overview of TiKV and PD's design to get a general impression on what you will build:

> 此外，你最好阅读TiKV和PD的设计概述，以获得你将构建的总体印象:

- TiKV
  - <https://pingcap.com/blog-cn/tidb-internal-1/> (Chinese Version)
  - <https://pingcap.com/blog/2017-07-11-tidbinternal1/> (English Version)
- PD
  - <https://pingcap.com/blog-cn/tidb-internal-3/> (Chinese Version)
  - <https://pingcap.com/blog/2017-07-20-tidbinternal3/> (English Version)

### Getting started

First, please clone the repository with git to get the source code of the project.

``` bash
git clone https://github.com/pingcap-incubator/tinykv.git
```

Then make sure you have [go](https://golang.org/doc/install) >= 1.13 toolchains installed. You should also have `make` installed.
Now you can run `make` to check that everything is working as expected. You should see it runs successfully.

> 然后确保你已经安装了go >= 1.13工具链。您还应该安装了make。现在您可以运行make来检查一切是否如预期的那样工作。您应该看到它成功地运行。

### Overview of the code

![overview](doc/imgs/overview.png)

Similar to the architecture of TiDB + TiKV + PD that separates the storage and computation, TinyKV only focuses on the storage layer of a distributed database system. If you are also interested in the SQL layer, please see [TinySQL](https://github.com/pingcap-incubator/tinysql). Besides that, there is a component called TinyScheduler acting as a center control of the whole TinyKV cluster, which collects information from the heartbeats of TinyKV. After that, the TinyScheduler can generate some scheduling tasks and distribute them to the TinyKV instances. All of them are communicated via RPC.

> 类似于TiDB + TiKV + PD分离存储和计算的架构，TinyKV只关注分布式数据库系统的存储层。如果您对SQL层也感兴趣，请参阅TinySQL。除此之外，还有一个名为TinyScheduler的组件作为**整个TinyKV集群的中心控制**，它从TinyKV的心跳中收集信息。之后，TinyScheduler可以生成一些调度任务，并将它们分发给TinyKV实例。所有这些都通过RPC进行通信。

The whole project is organized into the following directories:

- `kv`: implementation of the TinyKV key/value store.
- `proto`: all communication between nodes and processes uses Protocol Buffers over gRPC. This package contains the protocol definitions used by TinyKV, and the generated Go code that you can use.
- `raft`: implementation of the Raft distributed consensus algorithm, which is used in TinyKV.
- `scheduler`: implementation of the TinyScheduler which is responsible for managing TinyKV nodes and generating timestamps.
- `log`: log utility to output log based on level.

> 整个项目被组织到以下目录:
>
> - `kv`: 实现TinyKV键/值存储
> - `proto`:所有节点和进程之间的通信都使用gRPC上的protocol buffers。这个包包含TinyKV使用的协议定义，以及您可以使用的生成的Go代码。
> - `raft`:在TinyKV中使用的raft分布式一致性算法的实现。
> - `scheduler`: TinyScheduler的实现，它负责管理TinyKV节点和生成时间戳。
> - `log`:日志实用程序，根据级别输出日志。

### Course material

> 请按照课程材料学习背景知识，一步一步完成代码。

Please follow the course material to learn the background knowledge and finish code step by step.

- [Project1 - StandaloneKV](doc/project1-StandaloneKV.md)
- [Project2 - RaftKV](doc/project2-RaftKV.md)
- [Project3 - MultiRaftKV](doc/project3-MultiRaftKV.md)
- [Project4 - Transaction](doc/project4-Transaction.md)

## Deploy to a cluster

After you finish the whole implementation, it becomes runnable. You can try TinyKV by deploying it onto a real cluster, and interact with it through TinySQL.

>完成整个实现后，它就可以运行了。您可以尝试将TinyKV部署到一个真正的集群上，并通过TinySQL与它交互。

### Build

```
make
```

It builds the binary of `tinykv-server` and `tinyscheduler-server` to `bin` dir.

> 它将tinykv-server和tinyscheduler-server的二进制文件构建到bin dir。

### Run

Put the binary of `tinyscheduler-server`, `tinykv-server` and `tinysql-server` into a single dir.

Under the binary dir, run the following commands:

> 将tinyscheduler-server、tinykv-server和tinysql-server的二进制文件放到一个目录中。在二进制目录dir下，执行如下命令:

```
mkdir -p data
```

```
./tinyscheduler-server
```

```
./tinykv-server -path=data
```

```
./tinysql-server --store=tikv --path="127.0.0.1:2379"
```

### Play

```
mysql -u root -h 127.0.0.1 -P 4000
```
