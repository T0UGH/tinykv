# Project3 MultiRaftKV

In project2, you have built a high available kv server based on Raft, good work! But not enough, such kv server is backed by a single raft group which is not unlimited scalable, and every write request will wait until committed and then write to badger one by one, which is a key requirement to ensure consistency, but also kill any concurrency.

> 在project2, 你已经构建了一个基于Raft的高可用kv服务器，干得好！但这还不够，这样的kv服务器是由一个单个的raft group支持的，并不能达到无限的可伸缩行。并且每个写请求都会等待提交，然后一个一个地写给badger；这确保了一致性，但扼杀了任何并发性。

![multiraft](imgs/multiraft.png)

In this project you will implement a multi raft-based kv server with balance scheduler, which consist of multiple raft groups, each raft group is responsible for a single key range which is named region here, the layout will be looked like the above diagram. Requests to a single region are handled just like before, yet multiple regions can handle requests concurrently which improves performance but also bring some new challenges like balancing the request to each region, etc.

> 在这个项目中，您将实现一个基于multi raft的带有平衡调度程序的kv服务器。它包括多个raft group，每个raft group负责一个key范围，称为Region，架构如上图。对于单个Region的请求像以前一样被处理，但多个Region可以同时处理请求，这提高了性能，但也带来了一些新的挑战，如将请求平衡分配到每个region等。

This project has 3 part, including:

1. Implement membership change and leadership change to Raft algorithm
2. Implement conf change and region split on raftstore
3. Introduce scheduler

> 这个project分为3个部分，包括
>
> 1. 在Raft算法中实现成员变更(membership change)和领导变更(leadership change)
> 2. 在raftstore上实现配置变更(conf change)和region分裂(region split)
> 3. 实现调度器

## Part A

In this part you will implement membership change and leadership change to the basic raft algorithm, these features are required by the next two parts. Membership change, namely conf change, is used to add or remove peers to the raft group, which can change the quorum of the raft group, so be careful. Leadership change, namely leader transfer, is used to transfer the leadership to another peer, which is very useful for balance.

> 在本部分中，您将实现基本raft算法的成员变更和领导变更，这些特性是后面两部分所需要的。成员变更，即conf change，用于向Raft group添加或删除peer，这可能会改变raft group的quorum(大多数)，所以要小心。领导变更，即leader transfer，是用来将领导权转移到另一个peer，这对于负载均衡非常有用。

### The Code

The code you need to modify is all about `raft/raft.go` and `raft/rawnode.go`, also see `proto/proto/eraft.proto` for new messages you need to handle. And both conf change and leader transfer are triggered by the upper application, so you may want to start at `raft/rawnode.go`.

> 你需要修改的所有代码都在`raft/raft.go`和`raft/rawnode.go`中，你也可以看看`proto/proto/eraft.Proto`，来了解你需要处理的新的消息类型。而conf change和leader transfer都是由上层应用程序触发，所以你可能要从`raft/rawnode.go`开始。

### Implement leader transfer

To implement leader transfer, let’s introduce two new message types: `MsgTransferLeader` and `MsgTimeoutNow`. To transfer leadership you need to first call `raft.Raft.Step` with `MsgTransferLeader` message on the current leader, and to ensure the success of the transfer, the current leader should first check the qualification of the transferee (namely transfer target) like: is the transferee’s log up to date, etc. If the transferee is not qualified, the current leader can choose to abort the transfer or help the transferee, since abort is not helping, let’s choose to help the transferee. If the transferee’s log is not up to date, the current leader should send a `MsgAppend` message to the transferee and **stop accepting new proposals in case we end up cycling**. So if the transferee is qualified (or after the current leader’s help), the leader should send a `MsgTimeoutNow` message to the transferee immediately, and after receiving a `MsgTimeoutNow` message the transferee should start a new election immediately regardless of its election timeout, with a higher term and up to date log, the transferee has great chance to step down the current leader and become the new leader.

> 为了实现leader transfer，让我们引入两个新的消息类型:`MsgTransferLeader`和`MsgTimeoutNow`。转移领导权首先需要用`MsgTransferLeader`类型的信息来调用当前的领导者的`Step`方法。为了确保成功的转移,现任领导者应该首先检查受让者的资格，比如:受让者的日志是最新的,等等。如果受让者不合格，当前领导者可以选择中止转移或者帮助受让者，我们的实现选择帮助受让者。如果受让者的日志不是最新的，当前领导应该发送一个`MsgAppend`消息给受让者，并停止accept新的proposal(以防我们结束循环 > 啥叫结束循环)。如果受让者是合格的(或在当前领导的帮助下合格了)，领导应该立即发送一个`MsgTimeoutNow`消息给受让者，在收到`MsgTimeoutNow`消息后，受让者应该立即开始新的选举，而不管它的选举时钟是否超时。有一个更高的term和最新的log，受让者有很大的机会选举成为新的领导人。

#### 扩展阅读 raft phd paper section 3.10



本节描述了Raft的一个可选扩展，它允许一个服务器将其领导权转移给另一个服务器。在两种情况下，领导权的转移可能是有用的:

1. 有时候领导必须下台。例如，它可能需要重新启动进行维护，或者可能会从集群中删除(请参见第4章)。当它退出时，集群将在选举超时前处于空闲状态，直到另一台服务器超时并赢得选举。这种短暂的不可用可以通过让领导在下台前将其领导权转移到另一台服务器来避免。
2. 在某些情况下，一台或多台服务器可能比其他服务器更适合领导集群。例如，高负载的服务器不是一个好的领导者，或者在广域网部署中，为了最大限度地减少客户端和领导者之间的延迟，主数据中心中的服务器可能是首选。其他共识算法可能能够在领导人选举期间适应这些偏好，但是Raft需要一个具有足够最新日志的服务器来成为领导人，这可能不是最受欢迎的服务器。相反，Raft中的领导者可以定期检查其可用的追随者中是否有一个更合适，如果是，将其领导转移到该服务器。



为了在Raft中转移领导权，前一个领导者将其日志条目发送到目标服务器，然后目标服务器运行选举，而不等待选举超时时钟。因此，**前一个领导者确保目标服务器在其任期开始时拥有所有已提交的条目**，并且与正常选举一样，多数派投票保证安全属性(如领导者完整性属性)得到维护。



以下步骤更详细地描述了该过程:

1. 前任领导停止接受新的客户请求
2. 前一个领导者使用第3.5节中描述的正常日志复制机制，完全更新目标服务器的日志以匹配其自己的日志。
3. 前一个领导者向目标服务器发送超时请求。该请求与目标服务器的选举计时器触发具有相同的效果:目标服务器开始新的选举(增加其任期并成为候选人)。



一旦目标服务器收到TimeoutNow请求，它极有可能比任何其他服务器先开始选举，并在下一个任期成为领导者。它给前任领导的下一个信息将包括它的新任期编号，导致前任领导下台。至此，领导交接完成。



目标服务器也有可能失败；在这种情况下，群集必须恢复客户端操作。**如果在大约一个选举超时的时候后，领导移交没有完成，前任领导将中止移交，并继续接受客户请求**。如果之前的领导者是错误的，并且目标服务器实际上是可操作的，那么在最坏的情况下，这种错误将导致一次额外的选举，之后客户端操作将被恢复。



这种方法通过在Raft集群的正常转换中运行来保持**安全**性。例如，即使时钟以任意速度运行，Raft也已经保证了安全性；当目标服务器收到TimeoutNow请求时，**相当于目标服务器的时钟快速向前跳跃**，这是安全的。



#### TODO

- [x] 完成`MsgTransferLeaderHandler`
- [x] 把`LeaderMsgProposeHandler`改成当`h.raft.leadTransferee == 0`时就直接丢弃消息
- [x] 在`becomeFollower`时，将`h.raft.leadTransferee`更新回来
- [x] 如果`leadTransferee !=0`并且 `MsgAppendResponseHandler`收到了来自这个`leadTransferee`的`Resp`，就发一个`TransferLeaderMsg`给自己，再看看现在能不能`TransferLeaderMsg`
- [x] 完成`MsgTimeoutHandler`
- [ ] 最后，就是如果超时了如何处理？按照论文中的处理吗？



### Implement conf change

Conf change algorithm you will implement here is not the joint consensus algorithm mentioned in the extended Raft paper that can add and/or remove arbitrary peers at once, instead, it can only add or remove peers one by one, which is more simple and easy to reason about. Moreover, conf change start at calling leader’s `raft.RawNode.ProposeConfChange` which will propose an entry with `pb.Entry.EntryType` set to `EntryConfChange` and `pb.Entry.Data` set to the input `pb.ConfChange`. When entries with type `EntryConfChange` are committed, you must apply it through `RawNode.ApplyConfChange` with the `pb.ConfChange` in the entry, only then you can add or remove peer to this raft node through `raft.Raft.addNode` and `raft.Raft.removeNode` according to the `pb.ConfChange`.

> 您将在这里实现的Conf change算法不是在扩展Raft论文中提到的联合共识算法，论文中的算法可以一次性添加和/或删除任意peer。相反，我们实现的算法只能一个一个地添加或删除对等点，这更简单，也更容易推理。此外，conf change开始于调用leader的`raft.RawNode.ProposeConfChange`，它会propose一个类型为`EntryConfChange`、`Data`中存放输入的`pb.ConfChange`的`entry`。当类型为`EntryConfChange`的entries被提交了，你必须传入entry中的`pb.ConfChange`作为参数来调用`RawNode.ApplyConfChange`以apply这个entry。只有在这之后，你才能通过`raft.Raft.addNode`和`raft.Raft.removeNode`来添加或删除peer到这个raft节点。
>
> 重点
>
> - 一次添加一个节点或删除一个节点
> - 先看`raft.RawNode.ProposeConfChange()`，`ent := pb.Entry{EntryType: pb.EntryType_EntryConfChange, Data: data}`
> - 然后提交的时候，判断entry的类型来做不同的操作
>
> todo:
>
> - [x] 实现`raft/raft.go/addNode`和`raft/raft.go/removeNode`，简单来说就是往`Prs`里面加key删key
> - [x] 直接改`Prs`有可能会引发各种级联效应，需要处理，
>   - [ ] 甚至会改变quorum，这样会导致committed不是单增的，不能让committed回退
>   - [x] 然后很多地方还需要加上判断`prs[id]`现在在不在的判定，如果不在也是需要一番额外操作
> - [x] 改`HandleRaftReady`的逻辑
> - [ ] q: 除了在prs上边删除这些节点还需要进行哪些操作？如果是leader是否需要将它转换为follower然后停止接收命令?
> - [ ] Only one conf change may be pending (in the log, but not yet applied) at a time. This is enforced via PendingConfIndex, which is set to a value >= the log index of the latest pending configuration change (if any). Config changes are only allowed to be proposed if the leader's applied index is greater than this value.
> - [ ] 一次可能只有一个conf change挂起(在日志中，但还没有应用)。这是通过PendingConfIndex强制执行的，该值被设置为 >= 最近挂起的配置更改(如果有的话)的日志索引。只有当leader应用的索引大于这个值时，配置更改才被允许被Proposed。



> Hints:
>
> - `MsgTransferLeader` message is local message that not come from network
> - You set the `Message.from` of the `MsgTransferLeader` message to the transferee (namely transfer target)
> - To start a new election immediately you can call `Raft.Step` with `MsgHup` message
> - Call `pb.ConfChange.Marshal` to get bytes represent of `pb.ConfChange` and put it to `pb.Entry.Data`

> Hints:
>
> - `MsgTransferLeader`消息是本地消息，不是来自网络
> - 你把`MsgTransferLeader`消息的`message.from`设置为受让人
> - 要立即开始新的选举，你可以使用`MsgHup`消息来调用`Raft.Step`
> - 调用`pb.confchange.marshal`将`pb.ConfChange`转换为bytes，然后将转换后的bytes放到`pb.Entry.Data`中

## Part B

As the Raft module supported membership change and leadership change now, in this part you need to make TinyKV support these admin commands based on part A. As you can see in `proto/proto/raft_cmdpb.proto`, there are four types of admin commands:

- CompactLog (Already implemented in project 2 part C)
- TransferLeader
- ChangePeer
- Split

> Raft模块现在支持成员变更和领导变更，在这一部分中，您需要让TinyKV基于part a支持一些管理命令(admin comands)。正如您在`proto/proto/raft_cmdpb.proto`中看到的，有四种类型的管理命令:
>
> - `CompactLog` (已经在project2 part C中实现了)
> - `TransferLeader`
> - `ChangePeer`
> - `Split`

`TransferLeader` and `ChangePeer` are the commands based on the Raft support of leadership change and membership change. These will be used as the basic operator steps for the balance scheduler. `Split` splits one Region into two Regions, that’s the base for multi raft. You will implement them step by step.

>`TransferLeader`和`ChangePeer`是Raft支持的基于领导变更和成员变更的命令。这些命令将用于支持平衡调度器。`split`将一个`Region`分割成两个`Region`，这是multi-raft的基础。你将一步一步地实现它们。

### The Code

All the changes are based on the implementation of the project2, so the code you need to modify is all about `kv/raftstore/peer_msg_handler.go` and `kv/raftstore/peer.go`.

> 所有变更都基于project2的实现，所以你需要修改的所有代码都在`kv/raftstore/peer_msg_handler.go`和`kv/raftstore/peer.go`中。

### Propose transfer leader

This step is quite simple. As a raft command, `TransferLeader` will be proposed as a Raft entry. But `TransferLeader` actually is an action with no need to replicate to other peers, so you just need to call the `TransferLeader()` method of `RawNode` instead of `Propose()` for `TransferLeader` command.

>#### 提议变更领导
>
>这一步相当简单。作为一个raft命令，`TransferLeader`将被作为一条Raft entry。但是`TransferLeader`实际上是一个不需要复制到其他对等体的动作，所以您只需要调用`RawNode`的`TransferLeader()`方法，而不是`Propose()`。

### Implement conf change in raftstore

The conf change has two different types, `AddNode` and `RemoveNode`. Just as its name implies, it adds a Peer or removes a Peer from the Region. To implement conf change, you should learn the terminology of `RegionEpoch` first. `RegionEpoch` is a part of the meta-information of `metapb.Region`. When a Region adds or removes Peer or splits, the Region’s epoch has changed. RegionEpoch’s `conf_ver` increases during ConfChange while `version` increases during a split. It will be used to guarantee the latest region information under network isolation that two leaders in one Region.

> #### 在raftstore中实现配置更改
>
> 配置更改有两种不同的类型，`AddNode`和`RemoveNode`。顾名思义，它会在Region中添加或删除一个Peer。要实现conf变更，您应该首先学习术语——`RegionEpoch`。`RegionEpoch`是`metapb.Region`元信息的一部分。当一个Region添加或删除Peer或分裂时，该Region的Epoch已经改变。RegionEpoch的`conf_ver`在`ConfChange`期间增加，而`version`在`split`的时候增加。它将用于保证一个region的两位leader在网络隔离下获得最新的region信息。

You need to make raftstore support handling conf change commands. The process would be:

1. Propose conf change admin command by `ProposeConfChange`
2. After the log is committed, change the `RegionLocalState`, including `RegionEpoch` and `Peers` in `Region`
3. Call `ApplyConfChange()` of `raft.RawNode`



>你需要确保raftstore支持处理conf change命令。过程如下
>
>1. 通过调用`ProposeConfChange`提交conf change admin命令
>2. 日志提交完成后，需要修改`RegionLocalState`，包括`RegionEpoch`和`Region`中的`Peers`
>3. 调用`raft.RawNode`上的`ApplyConfChange()`命令



> Hints:
>
> - For executing `AddNode`, the newly added Peer will be created by heartbeat from the leader, check `maybeCreatePeer()` of `storeWorker`. At that time, this Peer is uninitialized and any information of its Region is unknown to us, so we use 0 to initialize its `Log Term` and `Index`. The leader then will know this Follower has no data (there exists a Log gap from 0 to 5) and it will directly send a snapshot to this Follower.
> - For executing `RemoveNode`, you should call the `destroyPeer()` explicitly to stop the Raft module. The destroy logic is provided for you.
> - Do not forget to update the region state in `storeMeta` of `GlobalContext`
> - Test code schedules the command of one conf change multiple times until the conf change is applied, so you need to consider how to ignore the duplicate commands of the same conf change.



>提示：
>
>- 要执行`AddNode`，新添加的Peer将由Leader的`HeartBeatMsg`创建(Leader向某个还没有该Region的Store发了一个HeartBeat，然后那边发现还没有就会创建一个)，请查看`storeWorker`的`maybeCreatePeer()`。此时，该Peer未初始化，其区域的任何信息我们都不知道，因此我们使用`0`初始化其`Log Term`和`Index`。然后，Leader将知道该`Follower`没有数据（存在0到5之间的日志间隔），并将直接向该Follower发送快照。
>- 为了执行`RemoveNode`，您应该显式调用`destroyPeer()`以停止Raft模块。销毁逻辑已经为您提供。
>- 不要忘记更新`GlobalContext`的`storeMeta`中的Region状态
>- 测试代码对一个CONF更改的命令进行多次调度，直到应用CONF更改，所以您需要考虑如何忽略同一CONF更改的重复命令。
>
>



> 如何更新RegionLocalState可以看这里kv/raftstore/bootstrap.go:93
>
> 当changeType==AddNode时，就往ctx和RegionLocalState里面都更新
>
> 当changeType==RemoveNode时，首先要更新，然后还需要判断被Remove的是不是自己，如果是自己就把自己给Remove掉



>/// `is_initial_msg`检查`msg`是否可以用来初始化一个新的对等体。
>
>//可能有两种情况:
>
>/ / 1。目标同行已经存在，但尚未与领导建立沟通
>
>/ / 2。由于成员变更或区域分裂，目标peer被新添加，但不是
>
>/ /创建
>
>//在这两种情况下，区域的开始键和结束键都附加在RequestVote和
>
>//该对等体的存储的心跳消息，以检查是否创建一个新的对等体
>
>//当接收到这些消息时，或者只是等待一个挂起的区域分割执行
>
>/ /。



### Implement split region in raftstore

![raft_group](imgs/keyspace.png)

To support multi-raft, the system performs data sharding and makes each Raft group store just a portion of data. Hash and Range are commonly used for data sharding. TinyKV uses Range and the main reason is that Range can better aggregate keys with the same prefix, which is convenient for operations like scan. Besides, Range outperforms in split than Hash. Usually, it only involves metadata modification and there is no need to move data around.



> ### 在raftstore中实现split Region
>
> 为了支持multi-raft，系统进行了数据分片，并使每个Raft Group仅存储一部分数据。Hash和Range通常用于数据分片。TinyKV使用Range，主要原因是Range可以更好地聚合具有相同前缀的key，这便于scan等操作。此外，Range在split中的性能优于hash。**通常，它只涉及元数据修改，不需要移动数据**(为什么通常只需要修改元数据不需要移动数据)。



``` protobuf
message Region {
 uint64 id = 1;
 // Region key range [start_key, end_key).
 bytes start_key = 2;
 bytes end_key = 3;
 RegionEpoch region_epoch = 4;
 repeated Peer peers = 5
}
```



Let’s take a relook at Region definition, it includes two fields `start_key` and `end_key` to indicate the range of data which the Region is responsible for. So split is the key step to support multi-raft. In the beginning, there is only one Region with range [“”, “”). You can regard the key space as a loop, so [“”, “”) stands for the whole space. With the data written, the split checker will checks the region size every `cfg.SplitRegionCheckTickInterval`, and generates a split key if possible to cut the Region into two parts, you can check the logic in
`kv/raftstore/runner/split_check.go`. The split key will be wrapped as a `MsgSplitRegion` handled by `onPrepareSplitRegion()`.



> 让我们重新查看Region定义，它包括两个字段`start_key`和`end_key`，以指示区域负责的数据范围。因此，分裂是支持multi-raft的关键步骤。在开始时，只有一个区域的范围为`['','')`。您可以将键空间视为一个循环，因此`['','')`代表整个空间。写入数据后，分裂检查器将在每个`SplitRegionCheckTickInterval`检中检查Region大小。并生成一个split key（如果可能），以将Region分割为两部分。你可以在`kv/raftstore/runner/split_check.go`中查看分裂逻辑。分裂key将被包装为由`PreparonSplitRegion()`处理的`MsgSplitRegion`。



To make sure the ids of the newly created Region and Peers are unique, the ids are allocated by the scheduler. It’s also provided, so you don’t have to implement it.
`onPrepareSplitRegion()` actually schedules a task for the pd worker to ask the scheduler for the ids. And make a split admin command after receiving the response from scheduler, see `onAskSplit()` in `kv/raftstore/runner/scheduler_task.go`.



> 为了确保新创建的Region和Peer的ID是唯一的，由调度程序分配ID。ID由调度程序分配。它也是提供的，所以您不必实现它。`onPrepareSplitRegion()`实际上为pd worker安排了一个任务，以向调度器请求ID。并在收到调度程序的响应后发出`split admin`命令，查看`kv/raftstore/runner/scheduler_task.go`中的`onAskSplit()`



So your task is to implement the process of handling split admin command, just like conf change does. The provided framework supports multiple raft, see `kv/raftstore/router.go`. When a Region splits into two Regions, one of the Regions will inherit the metadata before splitting and just modify its Range and RegionEpoch while the other will create relevant meta information.



> 因此，您的任务是实现处理split admin命令的过程，就像处理conf change一样。提供的框架支持multiple Raft，参见`kv/raftstore/router.go`。当一个`Region`拆分为两个`Region`时，其中一个`Region`将在拆分前继承元数据，只需修改其`Range`和`RegionEpoch`，而另一个Region将创建相关的元数据信息。



> Hints:
>
> - The corresponding Peer of this newly-created Region should be created by
`createPeer()` and registered to the router.regions. And the region’s info should be inserted into `regionRanges` in ctx.StoreMeta.
> - For the case region split with network isolation, the snapshot to be applied may have overlap with the existing region’s range. The check logic is in `checkSnapshot()` in `kv/raftstore/peer_msg_handler.go`. Please keep it in mind when implementing and take care of that case.
> - Use `engine_util.ExceedEndKey()` to compare with region’s end key. Because when the end key equals “”, any key will equal or greater than “”. > - There are more errors need to be considered: `ErrRegionNotFound`,
`ErrKeyNotInRegion`, `ErrEpochNotMatch`.



> 提示
>
> - 此新创建Region的对应Peer应由`createPeer()`创建并注册到`router.regions`(kv/raftstore/router.go:43)。Region信息应插入`ctx.storeMeta`中的`regionRanges`中。
> - 对于发生网络隔离的分裂Region，要应用的快照可能与现有区域的范围重叠。检查逻辑位于`kv/raftstore/peer_msg_handler.go`的`checkSnapshot()`中。请在实施和处理该案例时牢记这一点。
> - 使用`engine_util.ExceedEndKey()`与`region`的结束键进行比较。因为当结束键等于“”时，任何键都将等于或大于“”需要考虑的错误还有：`ErrRegionNotFound`、`ErrKeyNotInRegion`、`ErrEpochNotMatch`。
> - Q: 为什么split只涉及元数据修改，不需要移动数据
> - A: 因为在存储层面上，同一个store的所有region的data都是存在一起的，只是这些数据的管理权隶属于不同的region，所以看起来就不需要挪动数据

## Part C

> 注意: pending peers是leader不认为它们是working followers的peers

As we have instructed above, all data in our kv store is split into several regions, and every region contains multiple replicas. A problem emerged: where should we place every replica? and how can we find the best place for a replica? Who sends former AddPeer and RemovePeer commands? The Scheduler takes on this responsibility.

> 如上所述，我们的kv存储中的所有数据都分为几个Region，每个region都包含多个副本。这出现了一个问题：我们应该将每个副本放在哪里？我们如何才能找到副本的最佳位置？谁发送之前提到的`AddPeer`和`RemovePeer`命令？调度器(scheduler)承担这个责任。

To make informed decisions, the Scheduler should have some information about the whole cluster. It should know where every region is. It should know how many keys they have. It should know how big they are…  To get related information, the Scheduler requires that every region should send a heartbeat request to the Scheduler periodically. You can find the heartbeat request structure `RegionHeartbeatRequest` in `/proto/proto/pdpb.proto`. After receiving a heartbeat, the scheduler will update local region information.

> 为了做出明智的决策，调度器应该有一些关于整个集群的信息。它应该知道每个region在哪里。它应该知道他们存了多少key。它应该知道它们有多大……为了获得相关信息，调度程序**要求每个region都定期向调度程序发送心跳请求**。您可以在`/proto/proto/pdpb.proto`中找到心跳请求结构`RegionHeartbeatRequest`。在接收到心跳信号后，调度程序将更新本地存储的region信息。

Meanwhile, the Scheduler checks region information periodically to find whether there is an imbalance in our TinyKV cluster. For example, if any store contains too many regions, regions should be moved to other stores from it. These commands will be picked up as the response for corresponding regions’ heartbeat requests.

> 同时，调度器定期检查region信息，以发现TinyKV集群中是否存在不平衡。例如，如果任何store包含太多region，则应将区域从该store移动到其他store。这些命令将被放到相应region的heartbeat响应中。

In this part, you will need to implement the above two functions for Scheduler. Follow our guide and framework, and it won’t be too difficult.

>在本部分中，您将需要为调度器实现上述两个功能。遵循我们的指南和框架，这不会太困难。

### The Code

The code you need to modify is all about `scheduler/server/cluster.go` and `scheduler/server/schedulers/balance_region.go`. As described above, when the Scheduler received a region heartbeat, it will update its local region information first. Then it will check whether there are pending commands for this region. If there is, it will be sent back as the response.

> 你需要修改的代码全部都在`scheduler/server/cluster.go`和`scheduler/server/schedulers/balance_region.go`。如上所述，当调度器接收到区域心跳信号时，它将首先更新其本地区域信息。然后，它将检查此区域是否有挂起的命令。如果有，它将作为响应发回。

You only need to implement `processRegionHeartbeat` function, in which the Scheduler updates local information; and `Schedule` function for the balance-region scheduler, in which the Scheduler scans stores and determines whether there is an imbalance and which region it should move.

>你只需实现`processRegionHeartbeat`函数，在这个函数中调度器更新本地信息；
>
>你还需要实现`Schedule`函数作为平衡region调度器。其中调度器扫描存储并确定是否存在不平衡以及应移动哪个region。

### Collect region heartbeat

As you can see, the only argument of `processRegionHeartbeat` function is a regionInfo. It contains information about the sender region of this heartbeat. What the Scheduler needs to do is just to update local region records. But should it update these records for every heartbeat?

> 如您所见，`processRegionHeartbeat`函数的唯一参数是`regionInfo`。它包含有关此heartbeat的发送方region的信息。调度器需要做的只是更新本地region记录。但它应该为每一次心跳更新这些记录吗？

Definitely not! There are two reasons. One is that updates could be skipped when no changes have been made for this region. The more important one is that the Scheduler cannot trust every heartbeat. Particularly speaking, if the cluster has partitions in a certain section, the information about some nodes might be wrong.

> 绝对不是！有两个原因。一个是，当没有对该region进行任何更改时，可以跳过更新。更重要的是，调度器不能信任每个心跳。特别是，如果集群在某个部分中有分区，那么关于某些节点的信息可能是错误的。

For example, some Regions re-initiate elections and splits after they are split, but another isolated batch of nodes still sends the obsolete information to Scheduler through heartbeats. So for one Region, either of the two nodes might say that it's the leader, which means the Scheduler cannot trust them both.

> 例如，某些region在split后重新启动选举和split，但另一批孤立的节点仍然通过心跳将过时的信息发送给调度程序。因此，对于一个region，两个节点中的任何一个都可能说它是领导者，这意味着调度器不能同时信任这两个节点。

Which one is more credible? The Scheduler should use `conf_ver` and `version` to determine it, namely `RegionEpcoh`. The Scheduler should first compare the values of the Region version of two nodes. If the values are the same, the Scheduler compares the values of the configuration change version. The node with a larger configuration change version must have newer information.

> 哪一个更可信？调度程序应该使用`conf_ver`和`version`来确定它，即`RegionEpoch`。调度程序应首先比较两个节点的region的version。如果version相同，调度程序将比较`conf_ver`的值。`conf_ver`较大的节点必然具有更新的信息。

Simply speaking, you could organize the check routine in the below way:

1. Check whether there is a region with the same Id in local storage. If there is and at least one of the heartbeats’ `conf_ver` and `version` is less than its, this heartbeat region is stale

2. If there isn’t, scan all regions that overlap with it. The heartbeats’ `conf_ver` and `version` should be greater or equal than all of them, or the region is stale.

> 简单地说，您可以按以下方式组织检查程序：
>
> 1. 检查本地存储中是否存在具有相同Id的region。如果存在并且heartbeat中的`conf_ver`和`version`这两个中至少有一个小于本地存储中的region，则此heartbeat中的region已过时
> 2. 如果没有，请扫描与其重叠的所有region。heartbeat的`conf_ver`和`version`应大于或等于所有这些region，否则heartbeat中的region过时。

Then how the Scheduler determines whether it could skip this update? We can list some simple conditions:

* If the new one’s `version` or `conf_ver` is greater than the original one, it cannot be skipped

* If the leader changed,  it cannot be skipped

* If the new one or original one has pending peer,  it cannot be skipped

* If the ApproximateSize changed, it cannot be skipped

* …

> 那么如何决定Scheduler是否跳过这个更新呢？我们可以列出一些简单的情况
>
> - 如果新的`version`或`conf_ver`比原来的大，它不能被跳过
> - 如果leader变更了，它不能被跳过
> - 如果新的这个或者原来的有挂起的peer，不能被跳过
> - 如果`ApproximateSize`发生了变化，不能被跳过

Don’t worry. You don’t need to find a strict sufficient and necessary condition. Redundant updates won’t affect correctness.

> 别担心。你不需要找到一个严格的充分必要条件。冗余更新不会影响正确性。

If the Scheduler determines to update local storage according to this heartbeat, there are two things it should update: region tree and store status. You could use `RaftCluster.core.PutRegion` to update the region tree and use `RaftCluster.core.UpdateStoreStatus` to update related store’s status (such as leader count, region count, pending peer count… ).

> 如果调度程序决定根据此心跳更新本地存储，那么它应该更新两处：region tree和store status。你可以使用`RaftCluster.core.PutRegion`来更新region tree并使用`RaftCluster.core.UpdateStoreStatus`以更新相关存储的状态（例如领导计数、区域计数、挂起的peer计数…）

### Implement region balance scheduler

There can be many different types of schedulers running in the Scheduler, for example, balance-region scheduler and balance-leader scheduler. This learning material will focus on the balance-region scheduler.

> Scheduler中可以运行许多不同类型的schedulers，例如，balance-region scheduler和balance-leader scheduler。本学习材料将重点介绍balance-region scheduler。

Every scheduler should have implemented the Scheduler interface, which you can find in `/scheduler/server/schedule/scheduler.go`. The Scheduler will use the return value of `GetMinInterval` as the default interval to run the `Schedule` method periodically. If it returns null (with several times retry), the Scheduler will use `GetNextInterval` to increase the interval. By defining `GetNextInterval` you can define how the interval increases. If it returns an operator, the Scheduler will dispatch these operators as the response of the next heartbeat of the related region.

> 每个Scheduler都应该实现Scheduler interface，您可以在`/scheduler/server/schedule/scheduler.go`中找到该接口。Scheduler将使用`GetMinInterval`的返回值作为定期运行`Schedule`方法的默认间隔。如果它返回`null`（多次重试），调度程序将使用`GetNextInerval`来增加间隔。通过定义`GetNextInterval`，可以定义间隔的增加方式。如果它返回一个`operator`，`Scheduler`将分发这些操作符作为相关`region`的下一个心跳信号的响应。

The core part of the Scheduler interface is `Schedule` method. The return value of this method is `Operator`, which contains multiple steps such as `AddPeer` and `RemovePeer`. For example, `MovePeer` may contain `AddPeer`,  `transferLeader` and `RemovePeer` which you have implemented in former part. Take the first RaftGroup in the diagram below as an example. The scheduler tries to move peers from the third store to the fourth. First, it should `AddPeer` for the fourth store. Then it checks whether the third is a leader, and find that no, it isn’t, so there is no need to `transferLeader`. Then it removes the peer in the third store.

> Schduler接口的核心部分是`Schedule`方法。此方法的返回值为`Operator`，其中包含如`AddPeer`和`RemovePeer`等多个步骤。例如，`MovePeer`可能包含在前一部分中实现的`AddPeer`、`transferLeader`和`RemovePeer`。以下图中的第一个RaftGroup为例。调度程序尝试将peers从第三个store移动到第四个store。首先，它应该为第四个store添加Peer。然后检查第三个是否是领导者，发现不是，所以没有必要转移领导者。然后它删除第三个store中的peer。

You can use the `CreateMovePeerOperator` function in `scheduler/server/schedule/operator` package to create a `MovePeer` operator.

> 你可以使用`scheduler/server/schdule/operator`包中的`CreateMovePeerOperator`函数来创建一个`MovePeer`操作符。

![balance](imgs/balance1.png)

![balance](imgs/balance2.png)

In this part, the only function you need to implement is the `Schedule` method in `scheduler/server/schedulers/balance_region.go`. This scheduler avoids too many regions in one store. First, the Scheduler will select all suitable stores. Then sort them according to their region size. Then the Scheduler tries to find regions to move from the store with the biggest region size.

> 在这一部分，你唯一需要实现的`Schedule`方法在`scheduler/server/schedulers/balance_region.go`上。此Scheduler避免在一个store中出现过多的region。首先，scheduler将选择所有合适的store。然后根据region大小对它们进行排序。然后，scheduler从region size最大的store中寻找region来移动

The scheduler will try to find the region most suitable for moving in the store. First, it will try to select a pending region because pending may mean the disk is overloaded. If there isn’t a pending region, it will try to find a follower region. If it still cannot pick out one region, it will try to pick leader regions. Finally, it will select out the region to move, or the Scheduler will try the next store which has a smaller region size until all stores will have been tried.

> Scheduler将尝试在store中找到最适合移动的region。
>
> - 首先，它将尝试选择一个挂起的region，因为挂起可能意味着磁盘过载。
> - 如果没有挂起region，它将尝试查找follower region。
> - 如果它仍然无法选择一个region，它将尝试选择leader区域。
> - 最后，它将选择要移动的区域，或者scheduler将尝试下一个region大小较小的store，直到所有store都已尝试。

After you pick up one region to move, the Scheduler will select a store as the target. Actually, the Scheduler will select the store with the smallest region size. Then the Scheduler will judge whether this movement is valuable, by checking the difference between region sizes of the original store and the target store. If the difference is big enough, the Scheduler should allocate a new peer on the target store and create a move peer operator.

> 选择一个要移动的region后，scheduler将选择一个store作为目标。实际上，scheduler将选择具有最小region size的store。然后，scheduler将通过检查原始存储和目标存储的区域大小之间的差异来判断此移动是否有价值。如果差异足够大，scheduler应该在目标store上分配一个新的peer，并创建一个move peer operator。

As you might have noticed, the routine above is just a rough process. A lot of problems are left:

* Which stores are suitable to move?

In short, a suitable store should be up and the down time cannot be longer than `MaxStoreDownTime` of the cluster, which you can get through `cluster.GetMaxStoreDownTime()`.

> 正如您可能已经注意到的，上面的例行程序只是一个粗略的过程。留下了很多问题：
>
> q: 哪个store适合移动？
>
> a:简言之，一个合适的store应该是启动(up)的，而停机时间不能超过集群的`MaxStoreDownTime`，您可以通过`cluster.GetMaxStoreDownTime()`来获得它。

* How to select regions?

The Scheduler framework provides three methods to get regions. `GetPendingRegionsWithLock`, `GetFollowersWithLock` and `GetLeadersWithLock`. The Scheduler can get related regions from them. And then you can select a random region.

> q: 如何选择region
>
> a: 调度器框架提供了三种获取region的方法。`GetPendingRegionsWithLock`、`GetFollowersWithLock`、`GetLeadersWithLock`。调度器可以从中获取相关region。然后你可以选择一个随机region。

* How to judge whether this operation is valuable?

If the difference between the original and target stores’ region sizes is too small, after we move the region from the original store to the target store, the Scheduler may want to move back again next time. So we have to make sure that the difference has to be bigger than two times the approximate size of the region, which ensures that after moving, the target store’s region size is still smaller than the original store.

> q:如何判断这个操作是否有价值？
>
> a:如果原始存储和目标存储的region大小之间的差异太小，则在我们将区域从原始存储移动到目标存储后，计划程序可能希望下次再次移回。因此，我们必须确保差异必须大于region近似大小的两倍，这确保在移动后，目标store的region大小仍然小于original store。



[2021/09/13 01:21:47.892 -07:00] [FATAL] [log.go:294] [panic] [recover={}] [stack="github.com/pingcap/log.Fatal\n\t/home/tg/go/pkg/mod/github.com/pingcap/log@v0.0.0-20200117041106-d28c14d3b1cd/global.go:59\ngithub.com/pingcap-incubator/tinykv/scheduler/pkg/logutil.LogPanic\n\t/home/tg/go/tinykv/scheduler/pkg/logutil/log.go:294\nruntime.gopanic\n\t/usr/local/go/src/runtime/panic.go:969\nruntime.goPanicIndex\n\t/usr/local/go/src/runtime/panic.go:88\ngithub.com/pingcap-incubator/tinykv/scheduler/server/schedulers.(*balanceRegionScheduler).Schedule\n\t/home/tg/go/tinykv/scheduler/server/schedulers/balance_region.go:126\ngithub.com/pingcap-incubator/tinykv/scheduler/server.(*scheduleController).Schedule\n\t/home/tg/go/tinykv/scheduler/server/coordinator.go:350\ngithub.com/pingcap-incubator/tinykv/scheduler/server.(*coordinator).runScheduler\n\t/home/tg/go/tinykv/scheduler/server/coordinator.go:303"]