// Copyright 2015 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

Package raft sends and receives messages in the Protocol Buffer format defined in the eraftpb package.

> raft包使用在eraftpb包中定义的Protocol Buffer格式发送和接收消息

Raft is a protocol with which a cluster of nodes can maintain a replicated state machine.
The state machine is kept in sync through the use of a replicated log.
For more details on Raft, see "In Search of an Understandable Consensus Algorithm"
(https://ramcloud.stanford.edu/raft.pdf) by Diego Ongaro and John Ousterhout.

> Raft是一种协议，节点群集可以使用该协议维护复制的状态机。通过使用复制的日志，状态机保持同步。
> 有关Raft的更多详细信息，请参见“寻找可理解的共识算法”（https://ramcloud.stanford.edu/raft.pdf），作者是Diego Ongaro和John Ousterhout。

### Usage 用法

The primary object in raft is a Node. You either start a Node from scratch using raft.StartNode or start a Node from some initial state using raft.RestartNode.

> Raft中的基本对象是一个节点。 你要么使用`raft.StartNode`从头开始创建节点或使用`raft.RestartNode`从某个初始状态启动Node。这里说的node很有可能是代码里的RawNode


To start a node from scratch:

> 从头开启一个节点

```  go
storage := raft.NewMemoryStorage()
c := &Config{
    ID:              0x01,
    ElectionTick:    10,
    HeartbeatTick:   1,
    Storage:         storage,
}
n := raft.StartNode(c, []raft.Peer{{ID: 0x02}, {ID: 0x03}})
```


To restart a node from previous state:

> 使用之前的状态重启一个节点

```go
storage := raft.NewMemoryStorage()

// 从持久化中恢复
// recover the in-memory storage from persistent
// snapshot, state and entries.
storage.ApplySnapshot(snapshot)
storage.SetHardState(state)
storage.Append(entries)

c := &Config{
    ID:              0x01,
    ElectionTick:    10,
    HeartbeatTick:   1,
    Storage:         storage,
    MaxInflightMsgs: 256,
  }

// restart raft without peer information.
// peer information is already included in the storage.
n := raft.RestartNode(c)
```



Now that you are holding onto a Node you have a few responsibilities:

> 现在，你已经拥有一个节点，你将承担以下几项责任：

First, you must read from the `Node.Ready()` channel and process the updates it contains. These steps may be performed in parallel, except as noted in step 2.

> 首先，您必须从`Node.Ready()`通道读取数据，并处理其中包含的更新。这些步骤可以并行执行，但步骤2中提到的除外。

1. Write HardState, Entries, and Snapshot to persistent storage if they are not empty. Note that when writing an Entry with Index i, any previously-persisted entries with Index >= i must be discarded.

1. > 如果HardState，Entries和Snapshot不为空，则将它们写入持久性存储。 请注意，在写索引为i的entry时，必须丢弃所有先前存在的索引>=i的条目。


2. Send all Messages to the nodes named in the To field. It is important that no messages be sent until the latest HardState has been persisted to disk, and all Entries written by any previous Ready batch (Messages may be sent while entries from the same batch are being persisted).

2. > 将所有消息发送到“To”字段中命名的节点。重要的是，直到将最新的HardState持久化到磁盘上，以及任何先前的Ready批处理写入的所有条目之前，都不要发送任何消息（可以在持久化来自同一批处理的条目时发送消息）。

Note: Marshalling messages is not thread-safe; it is important that you make sure that no new entries are persisted while marshalling. The easiest way to achieve this is to serialize the messages directly inside your main raft loop.

> 注意：编组消息不是线程安全的； 重要的是，请确保在编组期间不persist任何新条目。
> 实现此的最简单方法是直接在主Raft循环内序列化消息。


3. Apply Snapshot (if any) and CommittedEntries to the state machine. If any committed Entry has Type EntryType_EntryConfChange, call Node.ApplyConfChange() to apply it to the node. The configuration change may be cancelled at this point by setting the NodeId field to zero before calling ApplyConfChange
(but ApplyConfChange must be called one way or the other, and the decision to cancel must be based solely on the state machine and not external information such as the observed health of the node).

3. > 将快照（如果有）和CommittedEntries应用于状态机。
> 如果任何已提交的条目的类型为`EntryType_EntryConfChange`，则调用`Node.ApplyConfChange()`以将其应用于节点。此时可以通过在调用`ApplyConfChange`之前将`NodeId`字段设置为零来取消配置更改
> （但是`ApplyConfChange`必须以一种或另一种方式调用，并且取消决定必须仅基于状态机，而不是外部信息，例如观察到的节点运行状况）。

4. Call Node.Advance() to signal readiness for the next batch of updates. This may be done at any time after step 1, although all updates must be processed in the order they were returned by Ready.

4. > 调用Node.Advance()表示已准备好进行下一批更新。
   > although all updates must be processed in the order they were returned by Ready.，可以在步骤1之后的任何时间完成此操作。

Second, all persisted log entries must be made available via an implementation of the Storage interface. The provided MemoryStorage type can be used for this (if you repopulate its state upon a restart), or you can supply your own disk-backed implementation.

> 其次，必须通过Storage接口的实现来提供持久化日志的可见性。


Third, when you receive a message from another node, pass it to Node.Step:

> 第三，当你从另一个节点收到消息时，消息需要被传递给Node.Step：

	func recvRaftRPC(ctx context.Context, m eraftpb.Message) {
		n.Step(ctx, m)
	}



Finally, you need to call Node.Tick() at regular intervals (probably via a time.Ticker). Raft has two important timeouts: heartbeat and the election timeout. However, internally to the raft package time is represented by an abstract "tick".

>  最后，您需要定期调用Node.Tick()（可能通过time.Ticker。raft有两个重要的超时：心跳和
> 选举超时。 但是，在raft包内部，时间是用抽象的“tick”表示。

The total state machine handling loop will look something like this:

> 总的状态机处理循环可能长这样：下面的变量`s`代表状态机

```go
  for {
    select {
    case <-s.Ticker:
      n.Tick()
    case rd := <-s.Node.Ready():
      // 这几步是同步的, 因此不会在saveToStorage的同时sendMsg
      // 保存到硬盘
      saveToStorage(rd.State, rd.Entries, rd.Snapshot)
      // 发送消息
      send(rd.Messages)
      //创建snapshot
      if !raft.IsEmptySnap(rd.Snapshot) {
        processSnapshot(rd.Snapshot)
      }
      // 应用entries到app里的状态机
      for _, entry := range rd.CommittedEntries {
        process(entry)
        if entry.Type == eraftpb.EntryType_EntryConfChange {
          var cc eraftpb.ConfChange
          cc.Unmarshal(entry.Data)
          s.Node.ApplyConfChange(cc)
        }
      }
      // 继续
      s.Node.Advance()
    case <-s.done:
      return
    }
  }
```

To propose changes to the state machine from your node take your application
data, serialize it into a byte slice and call:

> 要从您的节点提出对状态机的更改建议，请使用您的应用程序数据，将其序列化为slice并调用

```go
n.Propose(data)
```

If the proposal is committed, data will appear in committed entries with type
eraftpb.EntryType_EntryNormal. There is no guarantee that a proposed command will be
committed; you may have to re-propose after a timeout.

> 如果proposal是已提交，则数据将显示在类型为已提交的条目中eraftpb.EntryType_EntryNormal。
> 不能保证一个被proposed的命令就一定会被提交; 您可能必须在超时后re-propose。

To add or remove a node in a cluster, build ConfChange struct 'cc' and call:

> 要添加或删除集群中的节点，请构建ConfChange结构'cc'并调用： 

	n.ProposeConfChange(cc)

After config change is committed, some committed entry with type eraftpb.EntryType_EntryConfChange will be returned. You must apply it to node through:

> 在配置变更被提交后, 有些类型为`eraftpb.EntryType_EntryConfChange`的已提交的entry将会被返回。你必须通过以下的方法将它应用到节点上

	var cc eraftpb.ConfChange
	cc.Unmarshal(data)
	n.ApplyConfChange(cc)

Note: An ID represents a unique node in a cluster for all time. A given ID MUST be used only once even if the old node has been removed. This means that for example IP addresses make poor node IDs since they may be reused. Node IDs must be non-zero.

> 注意：ID始终代表集群中的唯一节点。 即使删除了旧节点，给定的ID也必须仅使用一次。这意味着，用ip地址做id很差劲，因为它们可能会被重用。 节点ID必须为非零。

### Implementation notes

This implementation is up to date with the final Raft thesis (https://ramcloud.stanford.edu/~ongaro/thesis.pdf), although our implementation of the membership change protocol differs somewhat from that described in chapter 4. The key invariant that membership changes happen one node at a time is preserved, but in our implementation the
membership change takes effect when its entry is applied, not when it is added to the log (so the entry is committed under the old membership instead of the new). This is equivalent in terms of safety, since the old and new configurations are guaranteed to overlap.

> todo 没看懂 
>
> 该实现与最新的Raft论文同步（https://ramcloud.stanford.edu/~ongaro/thesis.pdf），尽管我们成员资格更改协议的实现与第4章中描述的有所不同。关键的不变点是，成员资格更改一次在一个节点上发生，但在我们的实现中，成员资格更改在应用它的条目时才生效，而不是在添加到日志时生效（因此该条目是在旧的成员资格而不是新的成员资格下提交的）。 就安全性而言，这是等效的，因为保证了新旧配置的重叠。

To ensure that we do not attempt to commit two membership changes at once by matching log positions (which would be unsafe since they should have different quorum requirements), we simply disallow any proposed membership change while any uncommitted change appears in the leader's log.

This approach introduces a problem when you try to remove a member from a two-member cluster: If one of the members dies before the other one receives the commit of the confchange entry, then the member cannot be removed any more since the cluster cannot make progress. For this reason it is highly recommended to use three or more nodes in every cluster.

### MessageType 消息类型

Package raft sends and receives message in Protocol Buffer format (defined in eraftpb package). Each state (follower, candidate, leader) implements its own 'step' method ('stepFollower', 'stepCandidate', 'stepLeader') when advancing with the given eraftpb.Message. Each step is determined by its eraftpb.MessageType. Note that every step is checked by one common method 'Step' that safety-checks the terms of node and incoming message to prevent stale log entries:

'MessageType_MsgHup' is used for election. If a node is a follower or candidate, the 'tick' function in 'raft' struct is set as 'tickElection'. If a follower or candidate has not received any heartbeat before the election timeout, it passes 'MessageType_MsgHup' to its Step method and becomes (or remains) a candidate to start a new election.

>  `MessageType_MsgHup`用于选举。 如果节点是`follower`或`candidate`，则将`raft`结构中的`tick`函数设置为`tickElection`。如果follower或candidate在选举超时之前未收到任何心跳，它将`MessageType_MsgHup`传递给其`Step`方法，并成为（或保持）候选人来开始新的选举。

'MessageType_MsgBeat' is an internal type that signals the leader to send a heartbeat of the 'MessageType_MsgHeartbeat' type. If a node is a leader, the 'tick' function in the 'raft' struct is set as 'tickHeartbeat', and triggers the leader to send periodic 'MessageType_MsgHeartbeat' messages to its followers.

> `MessageType_MsgBeat`是一种内部消息，用于通知发送`MessageType_MsgHeartbeat`类型的心跳。
> 如果节点是领导者，则将"raft"结构中的`tick`方法设置为`tickHeartbeat`，并触发领导者向其关注者定期发送`MessageType_MsgHeartbeat`消息。

`MessageType_MsgPropose` proposes to append data to its log entries. This is a special type to redirect proposals to the leader. Therefore, `send` method overwrites `eraftpb.Message`'s term with its `HardState`'s term to avoid attaching its local term to `MessageType_MsgPropose`. When `MessageType_MsgPropose` is passed to the leader's `Step` method, the leader first calls the `appendEntry` method to append entries to its log, and then calls `bcastAppend` method to send those entries to its peers. When passed to candidate, `MessageType_MsgPropose` is dropped. When passed to follower, `MessageType_MsgPropose` is stored in follower's mailbox(msgs) by the send method. It is stored with sender's ID and later forwarded to the leader by rafthttp package.

> `MessageType_MsgPropose`建议将数据附加到其日志条目。 这是一种特殊类型，用于**将提案重定向到领导者**。 因此，`send`方法将用它`HardState`中的`term`覆盖`eraftpb.Message`的`term`，以避免附加其
> `MessageType_MsgPropose`的本地术语。 当将`MessageType_MsgPropose`传递给领导者的`Step`方法时，领导者首先调用`appendEntry`方法以将条目追加到其日志中，然后调用`bcastAppend`方法将这些条目发送给其所有对等方。 当传递给candidate时, `MessageType_MsgPropose`会被丢弃。当发送给`follower`时，这个消息被存储在follower的邮箱中，并且一会儿转发给`leader`

'MessageType_MsgAppend' contains log entries to replicate. A leader calls bcastAppend, which calls sendAppend, which sends soon-to-be-replicated logs in 'MessageType_MsgAppend' type. When 'MessageType_MsgAppend' is passed to candidate's Step method, candidate reverts back to follower, because it indicates that there is a valid leader sending 'MessageType_MsgAppend' messages. Candidate and follower respond to this message in 'MessageType_MsgAppendResponse' type.

> `MessageType_MsgAppend`包含要复制的日志项。leader调用`bcastAppend`，它再调用`sendAppend`发送一个`MessageType_MsgAppend`类型。当`MessageType_MsgAppend`被传递给candidate的Step方法时，candidate把自己变回follower，因为它表明有一个有效的leader发送`MessageType_MsgAppend`消息

'MessageType_MsgAppendResponse' is response to log replication request('MessageType_MsgAppend'). When
'MessageType_MsgAppend' is passed to candidate or follower's Step method, it responds by
calling 'handleAppendEntries' method, which sends 'MessageType_MsgAppendResponse' to raft
mailbox.

> `messagetype_msgappsponse`是对日志复制请求(`MessageType_MsgAppend`)的响应。当`MessageType_MsgAppend`被传递给候选或跟随者的步骤方法时，它通过调用`handleAppendEntries`方法响应，该方法将`messagetype_msgappendsponse`发送到raft mailbox。

`MessageType_MsgRequestVote`requests votes for election. When a node is a follower or candidate and `MessageType_MsgHup` is passed to its Step method, then the node calls 'campaign' method to campaign itself to become a leader. Once `campaign` method is called, the node becomes candidate and sends `MessageType_MsgRequestVote` to peers in cluster to request votes. When passed to the leader or candidate's Step method and the message's Term is lower than leader's or candidate's, 'MessageType_MsgRequestVote' will be rejected ('MessageType_MsgRequestVoteResponse' is returned with Reject true).

> `MessageType_MsgRequestVote`请求为选举投票。当一个节点是一个follower或candidate并且`MessageType_MsgHup`被传递给它的Step方法，然后节点调用`campaign`方法来campaign自己成为leader。一旦`campaign`方法被调用，该节点就成为candidate，并将`MessageType_MsgRequestVote`发送给集群中的所有节点以请求投票。当传递给leader或candidate的Step方法且消息的term低于leader或candidate时，`MessageType_MsgRequestVote`将被拒绝(`MessageType_MsgRequestVoteResponse`将被返回，拒绝为true)。

If leader or candidate receives `MessageType_MsgRequestVote` with higher term, it will revert back to follower. When `MessageType_MsgRequestVote` is passed to follower, it votes for the sender only when sender's last term is greater than `MessageType_MsgRequestVote`'s term or sender's last term is equal to `MessageType_MsgRequestVote`'s term but sender's last committed index is greater than or equal to follower's.

> 如果leader或candidate收到更高的`MessageType_MsgRequestVote`，它将自己改回follower。当`MessageType_MsgRequestVote`传递给`follower`,它会看情况投票，具体情况按照论文中的比谁新

`MessageType_MsgRequestVoteResponse` contains responses from voting request. When `MessageType_MsgRequestVoteResponse` is passed to candidate, the candidate calculates how many votes it has won. If it's more than majority (quorum), it becomes leader and calls `bcastAppend`. If candidate receives majority of votes of denials, it reverts back to follower.

> `MessageType_MsgRequestVoteResponse`包含来自投票请求的响应。当`MessageType_MsgRequestVoteResponse`被传递给candidate时，candidate会计算自己赢得了多少张选票。如果它大于majority  (quorum)，它就变成leader并调用`bcastAppend`。如果候选人获得了大多数的否决票，它就会回到追随者状态。

'MessageType_MsgSnapshot' requests to install a snapshot message. When a node has just become a leader or the leader receives 'MessageType_MsgPropose' message, it calls 'bcastAppend' method, which then calls 'sendAppend' method to each follower. In 'sendAppend', if a leader fails to get term or entries, the leader requests snapshot by sending 'MessageType_MsgSnapshot' type message.

> 先略

'MessageType_MsgHeartbeat' sends heartbeat from leader. When 'MessageType_MsgHeartbeat' is passed to candidate and message's term is higher than candidate's, the candidate reverts back to follower and updates its committed index from the one in this heartbeat. And it sends the message to its mailbox. When 'MessageType_MsgHeartbeat' is passed to follower's Step method and message's term is higher than follower's, the follower updates its leaderID with the ID from the message.

> `MessageType_MsgHeartbeat`从`leader`发送心跳。当`MessageType_MsgHeartbeat`被传递给candidate，并且消息的`Term`高于candidate的`Term`时，candidate退回到follower，并从这个心跳中的索引更新其提交的索引。它会把信息发送到它的邮箱。
>
> 当`MessageType_MsgHeartbeat`被传递给`follower`的`Step`方法并且消息的term高于`follower`的term时，follower用消息中的ID更新其`leaderID`。

'MessageType_MsgHeartbeatResponse' is a response to 'MessageType_MsgHeartbeat'. When 'MessageType_MsgHeartbeatResponse' is passed to the leader's Step method, the leader knows which follower responded.

> `MessageType_MsgHeartbeatResponse`是对`MessageType_MsgHeartbeat`响应。当`MessageType_MsgHeartbeatResponse`被传递给`leader`的`Step`方法时，leader知道是哪个follower响应了
>
> ps :  感觉这个方法没啥用啊