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

package raft

import (
	"errors"

	pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"
)

// ErrStepLocalMsg is returned when try to step a local raft message
var ErrStepLocalMsg = errors.New("raft: cannot step raft local message")

// ErrStepPeerNotFound is returned when try to step a response message
// but there is no peer found in raft.Prs for that node.
var ErrStepPeerNotFound = errors.New("raft: cannot step as peer not found")

// SoftState提供的状态是易失性的，不需要持久保存到WAL。
// SoftState provides state that is volatile and does not need to be persisted to the WAL.
type SoftState struct {
	Lead      uint64
	RaftState StateType
}

// Ready封装entries和messages，这些条目和消息已准备被读取，被保存到稳定的存储中，被提交 或被发送给其他对等方。
// Ready中的所有字段均为只读。
// Ready encapsulates the entries and messages that are ready to read,
// be saved to stable storage, committed or sent to other peers.
// All fields in Ready are read-only.
type Ready struct {
	// The current volatile state of a Node.
	// SoftState will be nil if there is no update.
	// It is not required to consume or store SoftState.
	// 节点的当前易失性状态。
	// 如果不更新，SoftState将为nil。
	// 不需要使用或存储SoftState。
	*SoftState

	// The current state of a Node to be saved to stable storage BEFORE
	// Messages are sent.
	// HardState will be equal to empty state if there is no update.
	// 一个节点在Messages被sent之前需要被保存到持久化存储的当前状态
	pb.HardState

	// Entries specifies entries to be saved to stable storage BEFORE
	// Messages are sent.
	// 在Messages被sent之前需要保存到持久化存储的entries
	Entries []pb.Entry

	// Snapshot specifies the snapshot to be saved to stable storage.
	// 要保存到持久化状态中的快照
	Snapshot pb.Snapshot

	// CommittedEntries specifies entries to be committed to a
	// store/state-machine. These have previously been committed to stable
	// store.
	// 指定需要被应用到状态机上的entries
	// 这些entries之前已经提交到了持久化存储里
	CommittedEntries []pb.Entry

	// 消息指定将entries提交到稳定存储后要发送的outbound(出站)消息。
	// 如果它包含MessageType_MsgSnapshot消息，则当收到快照或通过调用ReportSnapshot失败快照时，应用程序务必报告回raft。
	// Messages specifies outbound messages to be sent AFTER Entries are
	// committed to stable storage.
	// If it contains a MessageType_MsgSnapshot message, the application MUST report back to raft
	// when the snapshot has been received or has failed by calling ReportSnapshot.
	Messages []pb.Message
}

// RawNode就是包了一下Raft
// RawNode is a wrapper of Raft.
type RawNode struct {
	Raft *Raft
	// Your Data Here (2A).
	ready *Ready
}

// NewRawNode returns a new RawNode given configuration and a list of raft peers.
// 根据给定配置生成一个新的RawNode，上层用的
func NewRawNode(config *Config) (*RawNode, error) {
	// Your Code Here (2A).
	raft := newRaft(config)
	rawNode := &RawNode{
		Raft:  raft,
		ready: nil,
	}
	return rawNode, nil
}

// Tick推进内部逻辑时钟一下，上层用的，也就是上层想啥时候tick就啥时候tick
// Tick advances the internal logical clock by a single tick.
func (rn *RawNode) Tick() {
	rn.Raft.tick()
}

// Campaign使该RawNode转换到candidate状态。也就是它决定开始竞选
// Campaign causes this RawNode to transition to candidate state.
func (rn *RawNode) Campaign() error {
	return rn.Raft.Step(pb.Message{
		MsgType: pb.MessageType_MsgHup,
	})
}

// 客户调用这个api来试图(proposes)将data添加到raft日志中
// Propose proposes data be appended to the raft log.
func (rn *RawNode) Propose(data []byte) error {
	ent := pb.Entry{Data: data}
	return rn.Raft.Step(pb.Message{
		MsgType: pb.MessageType_MsgPropose,
		From:    rn.Raft.id,
		Entries: []*pb.Entry{&ent}})
}

// 客户调用这个api来修改配置
// ProposeConfChange proposes a config change.
func (rn *RawNode) ProposeConfChange(cc pb.ConfChange) error {
	data, err := cc.Marshal()
	if err != nil {
		return err
	}
	ent := pb.Entry{EntryType: pb.EntryType_EntryConfChange, Data: data}
	return rn.Raft.Step(pb.Message{
		MsgType: pb.MessageType_MsgPropose,
		Entries: []*pb.Entry{&ent},
	})
}

// 应用配置变化到本地节点
// ApplyConfChange applies a config change to the local node.
func (rn *RawNode) ApplyConfChange(cc pb.ConfChange) *pb.ConfState {
	if cc.NodeId == None {
		return &pb.ConfState{Nodes: nodes(rn.Raft)}
	}
	switch cc.ChangeType {
	case pb.ConfChangeType_AddNode:
		rn.Raft.addNode(cc.NodeId)
	case pb.ConfChangeType_RemoveNode:
		rn.Raft.removeNode(cc.NodeId)
	default:
		panic("unexpected conf type")
	}
	return &pb.ConfState{Nodes: nodes(rn.Raft)}
}

// 使用给定消息推动状态机 这是用来将其他节点发来的消息转发给raft处理
// Step advances the state machine using the given message.
func (rn *RawNode) Step(m pb.Message) error {
	// ignore unexpected local messages receiving over network
	if IsLocalMsg(m.MsgType) {
		return ErrStepLocalMsg
	}
	// From节点来的消息，通过上层转发到这来了
	if pr := rn.Raft.Prs[m.From]; pr != nil || !IsResponseMsg(m.MsgType) {
		return rn.Raft.Step(m)
	}
	return ErrStepPeerNotFound
}

// Ready返回此RawNode的当前时间点(point-in-time)状态。
// Ready returns the current point-in-time state of this RawNode.
func (rn *RawNode) Ready() Ready {
	// Your Code Here (2A).
	if rn.ready == nil {
		rn.ready = &Ready{}
		// HardState
		rn.ready.HardState = pb.HardState{}
		rn.ready.HardState.Term = rn.Raft.Term
		rn.ready.HardState.Vote = rn.Raft.Vote
		rn.ready.HardState.Commit = rn.Raft.RaftLog.committed
		// Entries
		rn.ready.Entries = rn.Raft.RaftLog.unstableEntries()
		// todo 2C 加Snapshot
		// CommittedEntries
		rn.ready.CommittedEntries = rn.Raft.RaftLog.nextEnts()
		// Messages
		rn.ready.Messages = rn.Raft.msgs
	}
	return *rn.ready
}

// HasReady在RawNode用户需要检查是否有ready在pending时被调用
// HasReady called when RawNode user need to check if any Ready pending.
func (rn *RawNode) HasReady() bool {
	// Your Code Here (2A).
	return rn.ready != nil
}

// Advance通知RawNode: APP已经被提交并且保存状态了 in the last Ready results
// Advance notifies the RawNode that the application has applied and saved progress in the
// last Ready results.
// `RawNode.Advance()`来更新raft的内部状态像`applied`、`stabled`等
func (rn *RawNode) Advance(rd Ready) {
	// Your Code Here (2A).
	rn.Raft.msgs = make([]pb.Message, 0)
	// 也可能需要使用snapshot里面的状态来更新
	// 更新applied
	if len(rd.CommittedEntries) != 0 {
		rn.Raft.RaftLog.applied = rd.CommittedEntries[len(rd.CommittedEntries)-1].Index
	} else if rd.Snapshot.Metadata != nil {
		rn.Raft.RaftLog.applied = rd.Snapshot.Metadata.Index
	}
	// 更新stabled
	if len(rd.Entries) != 0 {
		rn.Raft.RaftLog.stabled = rd.Entries[len(rd.Entries)-1].Index
	} else if rd.Snapshot.Metadata != nil {
		rn.Raft.RaftLog.stabled = rd.Snapshot.Metadata.Index
	}
	rn.ready = nil
}

// 获取进度
// GetProgress return the the Progress of this node and its peers, if this
// node is leader.
func (rn *RawNode) GetProgress() map[uint64]Progress {
	prs := make(map[uint64]Progress)
	if rn.Raft.State == StateLeader {
		for id, p := range rn.Raft.Prs {
			prs[id] = *p
		}
	}
	return prs
}

// TransferLeader尝试将领导权转移给指定的受让人。
// TransferLeader tries to transfer leadership to the given transferee.
func (rn *RawNode) TransferLeader(transferee uint64) {
	_ = rn.Raft.Step(pb.Message{MsgType: pb.MessageType_MsgTransferLeader, From: transferee})
}
