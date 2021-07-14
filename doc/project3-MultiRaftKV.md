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

`TransferLeader` and `ChangePeer` are the commands based on the Raft support of leadership change and membership change. These will be used as the basic operator steps for the balance scheduler. `Split` splits one Region into two Regions, that’s the base for multi raft. You will implement them step by step.

### The Code

All the changes are based on the implementation of the project2, so the code you need to modify is all about `kv/raftstore/peer_msg_handler.go` and `kv/raftstore/peer.go`.

### Propose transfer leader

This step is quite simple. As a raft command, `TransferLeader` will be proposed as a Raft entry. But `TransferLeader` actually is an action with no need to replicate to other peers, so you just need to call the `TransferLeader()` method of `RawNode` instead of `Propose()` for `TransferLeader` command.

### Implement conf change in raftstore

The conf change has two different types, `AddNode` and `RemoveNode`. Just as its name implies, it adds a Peer or removes a Peer from the Region. To implement conf change, you should learn the terminology of `RegionEpoch` first. `RegionEpoch` is a part of the meta-information of `metapb.Region`. When a Region adds or removes Peer or splits, the Region’s epoch has changed. RegionEpoch’s `conf_ver` increases during ConfChange while `version` increases during a split. It will be used to guarantee the latest region information under network isolation that two leaders in one Region.

You need to make raftstore support handling conf change commands. The process would be:

1. Propose conf change admin command by `ProposeConfChange`
2. After the log is committed, change the `RegionLocalState`, including `RegionEpoch` and `Peers` in `Region`
3. Call `ApplyConfChange()` of `raft.RawNode`

> Hints:
>
> - For executing `AddNode`, the newly added Peer will be created by heartbeat from the leader, check `maybeCreatePeer()` of `storeWorker`. At that time, this Peer is uninitialized and any information of its Region is unknown to us, so we use 0 to initialize its `Log Term` and `Index`. The leader then will know this Follower has no data (there exists a Log gap from 0 to 5) and it will directly send a snapshot to this Follower.
> - For executing `RemoveNode`, you should call the `destroyPeer()` explicitly to stop the Raft module. The destroy logic is provided for you.
> - Do not forget to update the region state in `storeMeta` of `GlobalContext`
> - Test code schedules the command of one conf change multiple times until the conf change is applied, so you need to consider how to ignore the duplicate commands of the same conf change.

### Implement split region in raftstore

![raft_group](imgs/keyspace.png)

To support multi-raft, the system performs data sharding and makes each Raft group store just a portion of data. Hash and Range are commonly used for data sharding. TinyKV uses Range and the main reason is that Range can better aggregate keys with the same prefix, which is convenient for operations like scan. Besides, Range outperforms in split than Hash. Usually, it only involves metadata modification and there is no need to move data around.

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

To make sure the ids of the newly created Region and Peers are unique, the ids are allocated by the scheduler. It’s also provided, so you don’t have to implement it.
`onPrepareSplitRegion()` actually schedules a task for the pd worker to ask the scheduler for the ids. And make a split admin command after receiving the response from scheduler, see `onAskSplit()` in `kv/raftstore/runner/scheduler_task.go`.

So your task is to implement the process of handling split admin command, just like conf change does. The provided framework supports multiple raft, see `kv/raftstore/router.go`. When a Region splits into two Regions, one of the Regions will inherit the metadata before splitting and just modify its Range and RegionEpoch while the other will create relevant meta information.

> Hints:
>
> - The corresponding Peer of this newly-created Region should be created by
`createPeer()` and registered to the router.regions. And the region’s info should be inserted into `regionRanges` in ctx.StoreMeta.
> - For the case region split with network isolation, the snapshot to be applied may have overlap with the existing region’s range. The check logic is in `checkSnapshot()` in `kv/raftstore/peer_msg_handler.go`. Please keep it in mind when implementing and take care of that case.
> - Use `engine_util.ExceedEndKey()` to compare with region’s end key. Because when the end key equals “”, any key will equal or greater than “”. > - There are more errors need to be considered: `ErrRegionNotFound`,
`ErrKeyNotInRegion`, `ErrEpochNotMatch`.

## Part C

As we have instructed above, all data in our kv store is split into several regions, and every region contains multiple replicas. A problem emerged: where should we place every replica? and how can we find the best place for a replica? Who sends former AddPeer and RemovePeer commands? The Scheduler takes on this responsibility.

To make informed decisions, the Scheduler should have some information about the whole cluster. It should know where every region is. It should know how many keys they have. It should know how big they are…  To get related information, the Scheduler requires that every region should send a heartbeat request to the Scheduler periodically. You can find the heartbeat request structure `RegionHeartbeatRequest` in `/proto/proto/pdpb.proto`. After receiving a heartbeat, the scheduler will update local region information.

Meanwhile, the Scheduler checks region information periodically to find whether there is an imbalance in our TinyKV cluster. For example, if any store contains too many regions, regions should be moved to other stores from it. These commands will be picked up as the response for corresponding regions’ heartbeat requests.

In this part, you will need to implement the above two functions for Scheduler. Follow our guide and framework, and it won’t be too difficult.

### The Code

The code you need to modify is all about `scheduler/server/cluster.go` and `scheduler/server/schedulers/balance_region.go`. As described above, when the Scheduler received a region heartbeat, it will update its local region information first. Then it will check whether there are pending commands for this region. If there is, it will be sent back as the response.

You only need to implement `processRegionHeartbeat` function, in which the Scheduler updates local information; and `Schedule` function for the balance-region scheduler, in which the Scheduler scans stores and determines whether there is an imbalance and which region it should move.

### Collect region heartbeat

As you can see, the only argument of `processRegionHeartbeat` function is a regionInfo. It contains information about the sender region of this heartbeat. What the Scheduler needs to do is just to update local region records. But should it update these records for every heartbeat?

Definitely not! There are two reasons. One is that updates could be skipped when no changes have been made for this region. The more important one is that the Scheduler cannot trust every heartbeat. Particularly speaking, if the cluster has partitions in a certain section, the information about some nodes might be wrong.

For example, some Regions re-initiate elections and splits after they are split, but another isolated batch of nodes still sends the obsolete information to Scheduler through heartbeats. So for one Region, either of the two nodes might say that it's the leader, which means the Scheduler cannot trust them both.

Which one is more credible? The Scheduler should use `conf_ver` and `version` to determine it, namely `RegionEpcoh`. The Scheduler should first compare the values of the Region version of two nodes. If the values are the same, the Scheduler compares the values of the configuration change version. The node with a larger configuration change version must have newer information.

Simply speaking, you could organize the check routine in the below way:

1. Check whether there is a region with the same Id in local storage. If there is and at least one of the heartbeats’ `conf_ver` and `version` is less than its, this heartbeat region is stale

2. If there isn’t, scan all regions that overlap with it. The heartbeats’ `conf_ver` and `version` should be greater or equal than all of them, or the region is stale.

Then how the Scheduler determines whether it could skip this update? We can list some simple conditions:

* If the new one’s `version` or `conf_ver` is greater than the original one, it cannot be skipped

* If the leader changed,  it cannot be skipped

* If the new one or original one has pending peer,  it cannot be skipped

* If the ApproximateSize changed, it cannot be skipped

* …

Don’t worry. You don’t need to find a strict sufficient and necessary condition. Redundant updates won’t affect correctness.

If the Scheduler determines to update local storage according to this heartbeat, there are two things it should update: region tree and store status. You could use `RaftCluster.core.PutRegion` to update the region tree and use `RaftCluster.core.UpdateStoreStatus` to update related store’s status (such as leader count, region count, pending peer count… ).

### Implement region balance scheduler

There can be many different types of schedulers running in the Scheduler, for example, balance-region scheduler and balance-leader scheduler. This learning material will focus on the balance-region scheduler.

Every scheduler should have implemented the Scheduler interface, which you can find in `/scheduler/server/schedule/scheduler.go`. The Scheduler will use the return value of `GetMinInterval` as the default interval to run the `Schedule` method periodically. If it returns null (with several times retry), the Scheduler will use `GetNextInterval` to increase the interval. By defining `GetNextInterval` you can define how the interval increases. If it returns an operator, the Scheduler will dispatch these operators as the response of the next heartbeat of the related region.

The core part of the Scheduler interface is `Schedule` method. The return value of this method is `Operator`, which contains multiple steps such as `AddPeer` and `RemovePeer`. For example, `MovePeer` may contain `AddPeer`,  `transferLeader` and `RemovePeer` which you have implemented in former part. Take the first RaftGroup in the diagram below as an example. The scheduler tries to move peers from the third store to the fourth. First, it should `AddPeer` for the fourth store. Then it checks whether the third is a leader, and find that no, it isn’t, so there is no need to `transferLeader`. Then it removes the peer in the third store.

You can use the `CreateMovePeerOperator` function in `scheduler/server/schedule/operator` package to create a `MovePeer` operator.

![balance](imgs/balance1.png)

![balance](imgs/balance2.png)

In this part, the only function you need to implement is the `Schedule` method in `scheduler/server/schedulers/balance_region.go`. This scheduler avoids too many regions in one store. First, the Scheduler will select all suitable stores. Then sort them according to their region size. Then the Scheduler tries to find regions to move from the store with the biggest region size.

The scheduler will try to find the region most suitable for moving in the store. First, it will try to select a pending region because pending may mean the disk is overloaded. If there isn’t a pending region, it will try to find a follower region. If it still cannot pick out one region, it will try to pick leader regions. Finally, it will select out the region to move, or the Scheduler will try the next store which has a smaller region size until all stores will have been tried.

After you pick up one region to move, the Scheduler will select a store as the target. Actually, the Scheduler will select the store with the smallest region size. Then the Scheduler will judge whether this movement is valuable, by checking the difference between region sizes of the original store and the target store. If the difference is big enough, the Scheduler should allocate a new peer on the target store and create a move peer operator.

As you might have noticed, the routine above is just a rough process. A lot of problems are left:

* Which stores are suitable to move?

In short, a suitable store should be up and the down time cannot be longer than `MaxStoreDownTime` of the cluster, which you can get through `cluster.GetMaxStoreDownTime()`.

* How to select regions?

The Scheduler framework provides three methods to get regions. `GetPendingRegionsWithLock`, `GetFollowersWithLock` and `GetLeadersWithLock`. The Scheduler can get related regions from them. And then you can select a random region.

* How to judge whether this operation is valuable?

If the difference between the original and target stores’ region sizes is too small, after we move the region from the original store to the target store, the Scheduler may want to move back again next time. So we have to make sure that the difference has to be bigger than two times the approximate size of the region, which ensures that after moving, the target store’s region size is still smaller than the original store.
