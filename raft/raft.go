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
	"math/rand"
	"time"

	pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"
)

// None是一个占位符nodeid，用来表示没有leader的情况
// None is a placeholder node ID used when there is no leader.
const None uint64 = 0

// StateType represents the role of a node in a cluster.
// StateType代表在一个集群中节点的角色
type StateType uint64

const (
	StateFollower StateType = iota
	StateCandidate
	StateLeader
)

// 状态的map，可以将状态从 int -> string
var stmap = [...]string{
	"StateFollower",
	"StateCandidate",
	"StateLeader",
}

// 打印状态的String
func (st StateType) String() string {
	return stmap[uint64(st)]
}

// 如果proposal在某些情况下被忽略，将返回ErrProposalDropped，以便可以通知proposer并快速失败。
// ErrProposalDropped is returned when the proposal is ignored by some cases,
// so that the proposer can be notified and fail fast.
var ErrProposalDropped = errors.New("raft proposal dropped")

// Config包含了启动一个raft节点需要的参数
// Config contains the parameters to start a raft.
type Config struct {
	// ID is the identity of the local raft. ID cannot be 0.
	// ID是这个raft的标识符，不能为0
	ID uint64

	// peers contains the IDs of all nodes (including self) in the raft cluster. It
	// should only be set when starting a new raft cluster. Restarting raft from
	// previous configuration will panic if peers is set. peer is private and only
	// used for testing right now.
	// peers包含了这个raft集群中的所有节点的id(包括自己)
	// 它应该只能在开启一个新的raft集群时被设置
	// 从之前的配置重启raft将会产生一个panic
	// peer是私有的并且只用于测试
	Peers []uint64

	// ElectionTick is the number of Node.Tick invocations that must pass between
	// elections. That is, if a follower does not receive any message from the
	// leader of current term before ElectionTick has elapsed, it will become
	// candidate and start an election. ElectionTick must be greater than
	// HeartbeatTick. We suggest ElectionTick = 10 * HeartbeatTick to avoid
	// unnecessary leader switching.
	// ElectionTick是两次选举之间必须传递的Node.Tick调用数。
	// 也就是说，如果在ElectionTick过去之前，关注者没有从当前任职的领导者那里收到任何消息，它将成为候选人并开始选举。
	// ElectionTick必须大于HeartbeatTick。 我们建议 ElectionTick = 10 * HeartbeatTick 以避免不必要的领导者切换。
	// 多久没收到消息开一次选举
	ElectionTick int

	// HeartbeatTick is the number of Node.Tick invocations that must pass between
	// heartbeats. That is, a leader sends heartbeat messages to maintain its
	// leadership every HeartbeatTick ticks.
	// HeartbeatTick是必须在两次心跳之间传递的Node.Tick调用数。
	// 也就是说，领导者发送心跳消息，以在每HeartbeatTick ticks时保持其领导地位。
	// 多久发一次心跳
	HeartbeatTick int

	// Storage is the storage for raft. raft generates entries and states to be
	// stored in storage. raft reads the persisted entries and states out of
	// Storage when it needs. raft reads out the previous state and configuration
	// out of storage when restarting.
	// Storage是为raft服务的存储。raft生成entries和states以存储在存储器中。
	// raft在需要时从存储中读取保存的条目和状态
	// 重新启动时，raft会从存储中读取以前的状态和配置。
	//
	Storage Storage

	// Applied is the last applied index. It should only be set when restarting
	// raft. raft will not return entries to the application smaller or equal to
	// Applied. If Applied is unset when restarting, raft might return previous
	// applied entries. This is a very application dependent configuration.
	// Applied 是最后应用的索引。 仅应在重新启动时设置。
	// raft 将不会返回小于或等于Applied的条目到应用程序
	// 如果重新启动时未设置Applied，则raft可能会返回之前已经applied的entries。
	// 这是一个非常依赖于应用程序的配置。
	Applied uint64
}

// 检查配置是否合法
func (c *Config) validate() error {
	if c.ID == None {
		return errors.New("cannot use none as id")
	}

	if c.HeartbeatTick <= 0 {
		return errors.New("heartbeat tick must be greater than 0")
	}

	if c.ElectionTick <= c.HeartbeatTick {
		return errors.New("election tick must be greater than heartbeat tick")
	}

	if c.Storage == nil {
		return errors.New("storage cannot be nil")
	}

	return nil
}

// Progress represents a follower’s progress in the view of the leader. Leader maintains
// progresses of all followers, and sends entries to the follower based on its progress.
// 进度代表领导者认为跟随者的进度。 领导者维护所有跟随者的进度，并根据其进度将entries发送给跟随者。
// 论文中有这个
type Progress struct {
	Match, Next uint64
}

// Raft节点的结构体
type Raft struct {
	// 节点id
	id uint64
	// 当前任期
	Term uint64
	// 投票给了谁
	Vote uint64

	// Raft日志
	// the log
	RaftLog *RaftLog

	//// todo: 为啥不需要它
	//peers []uint64

	// 记录了所有peers的进度
	// log replication progress of each peers
	Prs map[uint64]*Progress

	// 这个节点的角色
	// this peer's role
	State StateType

	// 角色表
	roleMap map[StateType]Role

	// votes records
	votes map[uint64]bool

	// 需要发送的msgs，也就是有消息需要发送出去就用这个发，可能是发给上层应用也可能是发送给其他raft节点，这个我要看看
	// msgs need to send
	msgs []pb.Message

	// leader的id
	// the leader id
	Lead uint64

	// 心跳间隔，应该发送
	// heartbeat interval, should send
	heartbeatTimeout int
	// 选举间隔baseline
	// baseline of election interval
	electionTimeoutBaseline int

	// 当前选举间隔
	electionTimeout int

	// number of ticks since it reached last heartbeatTimeoutBaseline.
	// only leader keeps heartbeatElapsed.
	// 自上次heartbeatTimeout以来的滴答数
	// 只有领导者保持这个变量
	heartbeatElapsed int

	// Ticks since it reached last electionTimeout when it is leader or candidate.
	// Number of ticks since it reached last electionTimeout or received a
	// valid message from current leader when it is a follower.
	// 当它作为领导者或候选人，记录上次选举超时的ticks数。
	// 自从其达到上次选举超时或从当前领导者成为跟随者以来收到的有效消息以来的滴答数。
	electionElapsed int

	// leadTransferee is id of the leader transfer target when its value is not zero.
	// Follow the procedure defined in section 3.10 of Raft phd thesis.
	// (https://web.stanford.edu/~ouster/cgi-bin/papers/OngaroPhD.pdf)
	// (Used in 3A leader transfer)
	leadTransferee uint64

	// Only one conf change may be pending (in the log, but not yet
	// applied) at a time. This is enforced via PendingConfIndex, which
	// is set to a value >= the log index of the latest pending
	// configuration change (if any). Config changes are only allowed to
	// be proposed if the leader's applied index is greater than this
	// value.
	// (Used in 3A conf change)
	PendingConfIndex uint64
}

// 用给定的配置初始化一个raft节点
// newRaft return a raft peer with the given config
func newRaft(c *Config) *Raft {
	// 先验证配置是否合法
	if err := c.validate(); err != nil {
		panic(err.Error())
	}
	// Your Code Here (2A).
	raft := &Raft{}
	raft.id = c.ID
	raft.Prs = make(map[uint64]*Progress)
	for _, id := range c.Peers {
		raft.Prs[id] = &Progress{}
	}
	// todo: 什么时候考虑从disk中恢复数据
	raft.State = StateFollower
	raft.roleMap = InitRoleMap(raft)
	raft.heartbeatTimeout = c.HeartbeatTick
	raft.electionTimeoutBaseline = c.ElectionTick
	raft.electionElapsed = 0
	raft.heartbeatElapsed = 0
	raft.RaftLog = newLog(c.Storage)
	hardState, _, _ := raft.RaftLog.storage.InitialState()
	raft.Term = hardState.Term
	raft.Vote = hardState.Vote
	return raft
}

// sendAppend sends an append RPC with new entries (if any) and the
// current commit index to the given peer. Returns true if a message was sent.
// sendAppend发送一个append RPC,带有新的entries和当前的commit index给对等体
// 如果消息发送成功就return true
func (r *Raft) sendAppend(to uint64) bool {
	// Your Code Here (2A).
	msg := pb.Message{
		MsgType: pb.MessageType_MsgAppend,
		To:      to,
		Term:    r.Term,
		From:    r.id,
	}
	entries, err := r.RaftLog.Entries(r.Prs[to].Next, r.RaftLog.LastIndex()+1)
	// 如果过界, 就改成发snapshot
	if err == ErrCompacted {
		r.sendSnapshot(to)
		return true
	}
	msg.Entries = ConvertEntryPointerSlice(entries)
	msg.Index = r.Prs[to].Next - 1
	msg.LogTerm, _ = r.RaftLog.Term(msg.Index)
	msg.Commit = r.RaftLog.committed
	r.addMsg(msg)
	return true
}

// sendHeartbeat sends a heartbeat RPC to the given peer.
// 给对等体发送heartbeat RPC
// todo 这个方法我感觉没用先注释了
//func (r *Raft) sendHeartbeat(to uint64) {
//	// Your Code Here (2A).
//}

// tick advances the internal logical clock by a single tick.
// 滴答使内部逻辑时钟前进一个滴答。
func (r *Raft) tick() {
	// Your Code Here (2A).
	r.roleMap[r.State].Tick()
}

// becomeFollower transform this peer's state to Follower
// becomeFollower 将节点的状态改成Follower
func (r *Raft) becomeFollower(term uint64, lead uint64) {
	// Your Code Here (2A).
	// todo: 得写这个啊
	r.State = StateFollower
	r.Term = term
	r.Lead = lead
	r.resetElectionClock()
	// 清空进度
	for i := range r.Prs {
		r.Prs[i] = &Progress{}
	}
}

// becomeCandidate transform this peer's state to candidate
// becomeCandidate 将节点的状态改为Candidate
func (r *Raft) becomeCandidate() {
	// Your Code Here (2A).
	// todo: 可能没完
	// 1 修改Term号
	r.Term += 1
	// 2 修改当前的Role和State
	r.State = StateCandidate
	// 3 清空votes数组来计票并且先给自己来一票
	r.votes = make(map[uint64]bool)
	r.votes[r.id] = true
	r.Vote = r.id
	// 4 重设Election时钟 todo 问题: 所有becomeCandidate调用都需要重设Election时钟吗? 需要
	// todo 此外 收到心跳的时候也要resetElectionCLock 此外外 becomeFollower时也需要重置Election时钟
	r.resetElectionClock()
	// 清空进度
	for i := range r.Prs {
		r.Prs[i] = &Progress{}
	}
}

// becomeLeader将节点的状态改为Leader
// becomeLeader transform this peer's state to leader
func (r *Raft) becomeLeader() {
	// Your Code Here (2A).
	// 1 先切换个状态
	r.State = StateLeader
	// 2 重置HeartBeat时钟
	r.resetHeartBeatClock()
	// 3 自己就是Lead
	r.Lead = r.id
	// 4 初始化Prs
	for id := range r.Prs {
		if id == r.id {
			r.Prs[id] = &Progress{
				Match: r.RaftLog.LastIndex(),
				Next:  r.RaftLog.LastIndex() + 1,
			}
		} else {
			r.Prs[id] = &Progress{
				Match: 0,
				Next:  r.RaftLog.LastIndex() + 1,
			}
		}
	}
	// NOTE: Leader should propose a noop entry on its term
	// 5 发一条no op entry在它的任期中
	r.Step(pb.Message{From: r.id, To: r.id, MsgType: pb.MessageType_MsgPropose, Entries: []*pb.Entry{{}}})
}

// 给上层应用和test提供的处理消息的入口, 需要根据消息类型的不同来调用不同的函数来处理
// Step the entrance of handle message, see `MessageType`
// on `eraftpb.proto` for what msgs should be handled
func (r *Raft) Step(m pb.Message) error {
	return r.roleMap[r.State].Handle(m)
}

// 处理AppendEntries RPC 请求
// handleAppendEntries handle AppendEntries RPC request
func (r *Raft) handleAppendEntries(m pb.Message) {
	// Your Code Here (2A).
	r.roleMap[r.State].Handle(m)
}

// 处理Heartbeat RPC 请求
// handleHeartbeat handle Heartbeat RPC request
// todo no usage 所以先注释了哈
//func (r *Raft) handleHeartbeat(m pb.Message) {
//	// Your Code Here (2A).
//}

// handleSnapshot handle Snapshot RPC request
func (r *Raft) handleSnapshot(m pb.Message) {
	// Your Code Here (2C).
	m.MsgType = pb.MessageType_MsgSnapshot
	r.roleMap[r.State].Handle(m)
}

// addNode add a new node to raft group
func (r *Raft) addNode(id uint64) {
	// Your Code Here (3A).
}

// removeNode remove a node from raft group
func (r *Raft) removeNode(id uint64) {
	// Your Code Here (3A).
}

func (r *Raft) addMsg(m pb.Message) {
	m.From = r.id
	r.msgs = append(r.msgs, m)
}

// 将传过来的Message的to改成所有msg并发送其他不变
func (r *Raft) broadCastMsg(m pb.Message) {
	for id := range r.Prs {
		// 给除了自己之外的人发
		if id == r.id {
			continue
		}
		currMsg := m
		currMsg.To = id
		r.addMsg(currMsg)
	}
}

// leader广播AppendEntries, 发给所有它该同步的peers
func (r *Raft) broadCastAppend() {
	for id := range r.Prs {
		// 给除了自己之外的人发
		if id == r.id {
			continue
		}
		r.sendAppend(id)
	}
}

func (r *Raft) resetHeartBeatClock() {
	r.heartbeatElapsed = 0
}

func (r *Raft) resetElectionClock() {
	r.electionElapsed = 0
	rand.Seed(time.Now().UnixNano())
	r.electionTimeout = r.electionTimeoutBaseline/2 + rand.Intn(r.electionTimeoutBaseline)
}

func (r *Raft) updateAndBroadCastCommitProgress() {
	matches := ExtractProgressForMatches(r.Prs)
	commit := CalcCommit(matches)
	updateSuccess := r.RaftLog.UpdateCommit(commit)
	// 更新成功了就再发一个appendEntries
	// 这样很不好, 因为浪费通信 为了通过 TestLogReplication2AB
	if updateSuccess {
		r.broadCastAppend()
	}
}

func (r *Raft) sendSnapshot(to uint64) {
	snapshot, err := r.RaftLog.storage.Snapshot()
	if err != nil {
		return
	}
	r.addMsg(pb.Message{
		MsgType:  pb.MessageType_MsgSnapshot,
		To:       to,
		Term:     r.Term,
		From:     r.id,
		Snapshot: &snapshot,
	})
}
