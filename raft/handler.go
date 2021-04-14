package raft

import pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"

type Handler interface {
	Handle(m pb.Message) error
}

// 处理 MessageType_MsgBeat 用于 Leader
type LeaderMsgBeatHandler struct {
	raft *Raft
}

func NewLeaderMsgBeatHandler(raft *Raft) *LeaderMsgBeatHandler {
	return &LeaderMsgBeatHandler{raft: raft}
}

func (h *LeaderMsgBeatHandler) Handle(m pb.Message) error {
	h.raft.broadCastMsg(pb.Message{
		Term:    h.raft.Term,
		MsgType: pb.MessageType_MsgHeartbeat})
	return nil
}

type LeaderMsgHeartbeatHandler struct {
	raft *Raft
}

func NewLeaderMsgHeartbeatHandler(raft *Raft) *LeaderMsgHeartbeatHandler {
	return &LeaderMsgHeartbeatHandler{raft: raft}
}

// Leader如何处理收到的心跳?
func (h *LeaderMsgHeartbeatHandler) Handle(m pb.Message) error {
	//1 如果收到一个大于自己的Term号的心跳，就将自己退回到Follower模式
	//todo 后面的lab是否需要精进一下
	if m.GetTerm() > h.raft.Term {
		h.raft.becomeFollower(m.GetTerm(), m.GetFrom())
	}
	//2 回复一个HeartbeatResponse
	h.raft.addMsg(pb.Message{
		MsgType: pb.MessageType_MsgHeartbeatResponse,
		To:      m.GetFrom(),
		Term:    h.raft.Term,
	})
	return nil
}

type FollowerMsgHeartbeatHandler struct {
	raft *Raft
}

func NewFollowerMsgHeartbeatHandler(raft *Raft) *FollowerMsgHeartbeatHandler {
	return &FollowerMsgHeartbeatHandler{raft: raft}
}

// Follower如何处理收到的心跳?
func (h *FollowerMsgHeartbeatHandler) Handle(m pb.Message) error {
	//1 根据心跳中的Term号来更新自己的Term和Lead
	if m.GetTerm() > h.raft.Term {
		h.raft.Lead = m.GetFrom()
	}

	if m.GetTerm() >= h.raft.Term {
		h.raft.resetElectionClock()
	}

	h.raft.Term = max(m.GetTerm(), h.raft.Term)

	//2 回复一个HeartbeatResponse
	h.raft.addMsg(pb.Message{
		MsgType: pb.MessageType_MsgHeartbeatResponse,
		To:      m.GetFrom(),
		Term:    h.raft.Term,
	})
	return nil
}

type CandidateMsgHeartbeatHandler struct {
	raft *Raft
}

func NewCandidateMsgHeartbeatHandler(raft *Raft) *CandidateMsgHeartbeatHandler {
	return &CandidateMsgHeartbeatHandler{raft: raft}
}

// Candidate如何处理收到的心跳?
func (h *CandidateMsgHeartbeatHandler) Handle(m pb.Message) error {
	//1 如果心跳中的Term大于等于则更新自己的Term, 退回到Follower, 更新Leader, 更新CommittedMsg
	// todo 更新CommittedMsg留给Project 2ab
	if m.GetTerm() >= h.raft.Term {
		h.raft.becomeFollower(m.GetTerm(), m.GetFrom())
	}
	//2 回复一个HeartbeatResponse
	h.raft.addMsg(pb.Message{
		MsgType: pb.MessageType_MsgHeartbeatResponse,
		To:      m.GetFrom(),
		Term:    h.raft.Term,
	})
	return nil
}

type MsgHupHandler struct {
	raft *Raft
}

func NewMsgHupHandler(raft *Raft) *MsgHupHandler {
	return &MsgHupHandler{raft: raft}
}

// 如果follower或candidate在选举超时之前未收到任何心跳，它将`MessageType_MsgHup`传递给其Step方法，
// 并成为（或保持）候选人来开始新的选举。
func (h *MsgHupHandler) Handle(m pb.Message) error {
	// 1 变成Candidate
	// todo 此处可能需要更改更多状态字段, 需要注意一下
	h.raft.becomeCandidate()
	// 2 BroadCash来让其他人给他投票
	h.raft.broadCastMsg(pb.Message{
		MsgType: pb.MessageType_MsgRequestVote,
		Term:    h.raft.Term,
	})
	return nil
}

type MsgRequestVoteHandler struct {
	raft *Raft
}

func NewMsgRequestVoteHandler(raft *Raft) *MsgRequestVoteHandler {
	return &MsgRequestVoteHandler{raft: raft}
}

// RequestVote先用一个处理函数吧, 太相似了
func (h *MsgRequestVoteHandler) Handle(m pb.Message) error {

	template := pb.Message{
		MsgType: pb.MessageType_MsgRequestVoteResponse,
		To:      m.GetFrom(),
		Term:    h.raft.Term,
	}

	// case 1: 旧
	if m.GetTerm() < h.raft.Term {
		oldMsg := template
		oldMsg.Reject = true
		h.raft.addMsg(oldMsg)
		return nil
	}

	// case 2: 中
	if m.GetTerm() == h.raft.Term {
		eqMsg := template
		eqMsg.Reject = h.raft.Vote == m.GetFrom()
		h.raft.addMsg(eqMsg)
		return nil
	}

	// case 3: 新
	newMsg := template
	newMsg.Reject = false
	h.raft.Term = m.GetTerm()
	newMsg.Term = m.GetTerm()
	h.raft.Vote = m.GetFrom()
	// todo 这里有个大问题就是我本来就是follower我重新becomeFollower会怎么样
	// todo 还有就是becomeFollower实现之后过来重新整一下这块的逻辑
	h.raft.becomeFollower(m.GetTerm(), None)
	h.raft.addMsg(newMsg)
	return nil
}

// 只有Candidate需要处理RequestVoteResponse
type CandidateMsgRequestVoteResponseHandler struct {
	raft *Raft
}

func NewCandidateMsgRequestVoteResponseHandler(raft *Raft) *MsgRequestVoteHandler {
	return &MsgRequestVoteHandler{raft: raft}
}

func (h *CandidateMsgRequestVoteResponseHandler) Handle(m pb.Message) error {
	// 累积票数
	h.raft.votes[m.GetFrom()] = !m.Reject
	// 当票数累积到一定程度时, becomeLeader
	count := countVotes(h.raft.votes)
	if count >= len(h.raft.peers)/2+1 {
		h.raft.becomeLeader()
	}
	return nil
}

// 不做任何操作的一个Handler
type NoopHandler struct{}

func NewNoopHandler() *NoopHandler {
	return &NoopHandler{}
}

func (h *NoopHandler) Handle(m pb.Message) error { return nil }
