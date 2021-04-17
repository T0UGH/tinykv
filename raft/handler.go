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
	// 2 先count一遍，处理singleNode的情况
	count := countVotes(h.raft.votes)
	if count >= len(h.raft.peers)/2+1 {
		h.raft.becomeLeader()
	}
	// 3 BroadCast来让其他人给他投票
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
		eqMsg.Reject = h.raft.Vote != m.GetFrom()
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

func NewCandidateMsgRequestVoteResponseHandler(raft *Raft) *CandidateMsgRequestVoteResponseHandler {
	return &CandidateMsgRequestVoteResponseHandler{raft: raft}
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

// Follower处理Propose消息
type FollowerMsgProposeHandler struct {
	raft *Raft
}

func NewFollowerMsgProposeHandler(raft *Raft) *FollowerMsgProposeHandler {
	return &FollowerMsgProposeHandler{raft: raft}
}

// 如果有lead就转发给lead, 否则丢弃
// todo: 丢弃的时候用不用干点啥通知上游
func (h *FollowerMsgProposeHandler) Handle(m pb.Message) error {
	if h.raft.Lead != None {
		m.To = h.raft.Lead
		h.raft.addMsg(m)
	}
	return nil
}

// Leader处理Propose消息
type LeaderMsgProposeHandler struct {
	raft *Raft
}

func NewLeaderMsgProposeHandler(raft *Raft) *LeaderMsgProposeHandler {
	return &LeaderMsgProposeHandler{raft: raft}
}

// 当将`MessageType_MsgPropose`传递给领导者的`Step`方法时，
// 领导者首先调用`appendEntry`方法以将条目追加到其日志中，
// 然后调用`bcastAppend`方法将这些条目发送给其所有对等方。
func (h *LeaderMsgProposeHandler) Handle(m pb.Message) error {
	h.raft.addEntries(m)
	// todo 为什么要在这里bcastAppend, 在这里bcastAppend都需要干什么
	// todo 也就是说每次发生propose事件才会引发AppendEntries
	// todo 这是否合理, 应该还算合理, 因为也只有这个时候leader发生了日志的变动，它需要把日志的变动告诉大家
	// todo 但是有个很牛逼的地方: 这样会不会频率太过小了, 因为有的节点可能不吃这个内容
	h.raft.broadCastAppend()
	return nil
}

type MsgAppendHandler struct {
	raft *Raft
}

// 处理appendEntries消息
func (h *MsgAppendHandler) Handle(m pb.Message) error {

	reply := pb.Message{
		MsgType: pb.MessageType_MsgAppendResponse,
		To:      m.GetFrom(),
		Term:    h.raft.Term,
		Reject:  true,
	}

	// 1 如果term比自己还老，说明这个是个老领导，老领导的AppendEntries就不用管了，直接返回false
	if m.GetTerm() < h.raft.Term {
		h.raft.addMsg(reply)
		return nil
	}

	// 2 prevLog没有怎么处理
	lastIndex := h.raft.RaftLog.LastIndex()
	lastTerm, _ := h.raft.RaftLog.Term(h.raft.RaftLog.LastIndex())
	noPrevLog := m.GetIndex() > lastIndex || m.GetTerm() != None && m.GetTerm() != lastTerm
	if noPrevLog {
		h.raft.addMsg(reply)
		return nil
	}

	// 3
	h.raft.RaftLog.UpdateEntries(m.GetIndex(), m.GetEntries())

	// 4 如果我是leader，并且还收到了一个至少termNumber跟我一样新的AppendEntries，就说明我是老leader，则设置我不是leader了
	// 即便我是follower也在这里调用一下来更新状态
	h.raft.becomeFollower(m.GetTerm(), m.GetFrom())

	// 5 更新commitIndex
	h.raft.RaftLog.UpdateCommit(m.Commit)

	reply.Term = h.raft.Term
	reply.Reject = false
	reply.Index = h.raft.RaftLog.LastIndex()
	h.raft.addMsg(reply)
	return nil
}

func NewMsgAppendHandler(raft *Raft) *MsgAppendHandler {
	return &MsgAppendHandler{raft: raft}
}

type MsgAppendResponseHandler struct {
	raft *Raft
}

func NewMsgAppendResponseHandler(raft *Raft) *MsgAppendResponseHandler {
	return &MsgAppendResponseHandler{raft: raft}
}

func (h *MsgAppendResponseHandler) Handle(m pb.Message) error {
	// 1 如果 reject了 就把Next-1然后再发一遍
	if m.GetReject() == true {
		h.raft.Prs[m.GetFrom()].Next--
		h.raft.sendAppend(m.GetFrom())
	}
	// 2 如果没有reject 就更新matchIndex然后, 重算commitIndex
	// 问: 没有reject如何更新matchIndex? 我需要知道一些信息, 比如当前进行到了哪里，然后用这个信息来更新
	h.raft.Prs[m.GetFrom()].Match = m.GetIndex()
	matches := ExtractProgressForMatches(h.raft.Prs)
	commit := CalcCommit(matches)
	h.raft.RaftLog.UpdateCommit(commit)
	return nil
}

// 不做任何操作的一个Handler
type NoopHandler struct{}

func NewNoopHandler() *NoopHandler {
	return &NoopHandler{}
}

func (h *NoopHandler) Handle(m pb.Message) error { return nil }
