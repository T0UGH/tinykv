package raft

import pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"

type Role interface {
	Tick()
	Handle(m pb.Message) error
}

func InitRoleMap(raft *Raft) map[StateType]Role {
	roleMap := make(map[StateType]Role)
	roleMap[StateLeader] = NewLeaderRole(raft)
	roleMap[StateFollower] = NewFollowerRole(raft)
	roleMap[StateCandidate] = NewCandidateRole(raft)
	return roleMap
}

type LeaderRole struct {
	raft       *Raft
	handlerMap map[pb.MessageType]Handler
}

func NewLeaderRole(raft *Raft) *LeaderRole {
	handlerMap := make(map[pb.MessageType]Handler)
	handlerMap[pb.MessageType_MsgHup] = NewNoopHandler()
	handlerMap[pb.MessageType_MsgRequestVote] = NewMsgRequestVoteHandler(raft)
	handlerMap[pb.MessageType_MsgRequestVoteResponse] = NewNoopHandler()
	handlerMap[pb.MessageType_MsgBeat] = NewLeaderMsgBeatHandler(raft)
	handlerMap[pb.MessageType_MsgHeartbeat] = NewLeaderMsgHeartbeatHandler(raft)
	handlerMap[pb.MessageType_MsgHeartbeatResponse] = NewMsgHeartBeatResponseHandler(raft)
	handlerMap[pb.MessageType_MsgPropose] = NewLeaderMsgProposeHandler(raft)
	handlerMap[pb.MessageType_MsgAppend] = NewMsgAppendHandler(raft)
	handlerMap[pb.MessageType_MsgAppendResponse] = NewMsgAppendResponseHandler(raft)
	handlerMap[pb.MessageType_MsgSnapshot] = NewMsgSnapshotHandler(raft)
	handlerMap[pb.MessageType_MsgTransferLeader] = NewMsgTransferLeaderHandler(raft)
	handlerMap[pb.MessageType_MsgTimeoutNow] = NewNoopHandler()
	return &LeaderRole{
		raft:       raft,
		handlerMap: handlerMap,
	}
}

func (role *LeaderRole) Handle(m pb.Message) error {
	return role.handlerMap[m.GetMsgType()].Handle(m)
}

// 用于处理leader的tick
func (role *LeaderRole) Tick() {
	r := role.raft
	r.heartbeatElapsed++
	if r.heartbeatElapsed >= r.heartbeatTimeout {
		r.Step(pb.Message{From: r.id, To: r.id, MsgType: pb.MessageType_MsgBeat})
	}
}

type CandidateRole struct {
	raft       *Raft
	handlerMap map[pb.MessageType]Handler
}

func NewCandidateRole(raft *Raft) *CandidateRole {
	handlerMap := make(map[pb.MessageType]Handler)
	handlerMap[pb.MessageType_MsgHup] = NewMsgHupAndMsgTimeoutNowHandler(raft)
	handlerMap[pb.MessageType_MsgRequestVote] = NewMsgRequestVoteHandler(raft)
	handlerMap[pb.MessageType_MsgRequestVoteResponse] = NewCandidateMsgRequestVoteResponseHandler(raft)
	handlerMap[pb.MessageType_MsgBeat] = NewNoopHandler()
	handlerMap[pb.MessageType_MsgHeartbeat] = NewCandidateMsgHeartbeatHandler(raft)
	handlerMap[pb.MessageType_MsgHeartbeatResponse] = NewNoopHandler()
	handlerMap[pb.MessageType_MsgPropose] = NewNoopHandler()
	handlerMap[pb.MessageType_MsgAppend] = NewMsgAppendHandler(raft)
	handlerMap[pb.MessageType_MsgAppendResponse] = NewNoopHandler()
	handlerMap[pb.MessageType_MsgSnapshot] = NewMsgSnapshotHandler(raft)
	handlerMap[pb.MessageType_MsgTransferLeader] = NewNotLeaderMsgTransferLeaderHandler(raft)
	handlerMap[pb.MessageType_MsgTimeoutNow] = NewMsgHupAndMsgTimeoutNowHandler(raft)
	return &CandidateRole{
		raft:       raft,
		handlerMap: handlerMap,
	}
}

func (role *CandidateRole) Handle(m pb.Message) error {
	return role.handlerMap[m.GetMsgType()].Handle(m)
}

// 用于处理candidate的tick
func (role *CandidateRole) Tick() {
	r := role.raft
	r.electionElapsed++
	if r.electionElapsed >= r.electionTimeout {
		r.Step(pb.Message{From: r.id, To: r.id, MsgType: pb.MessageType_MsgHup})
	}
}

type FollowerRole struct {
	raft       *Raft
	handlerMap map[pb.MessageType]Handler
}

func NewFollowerRole(raft *Raft) *FollowerRole {
	handlerMap := make(map[pb.MessageType]Handler)
	handlerMap[pb.MessageType_MsgHup] = NewMsgHupAndMsgTimeoutNowHandler(raft)
	handlerMap[pb.MessageType_MsgRequestVote] = NewMsgRequestVoteHandler(raft)
	handlerMap[pb.MessageType_MsgRequestVoteResponse] = NewNoopHandler()
	handlerMap[pb.MessageType_MsgBeat] = NewNoopHandler()
	handlerMap[pb.MessageType_MsgHeartbeat] = NewFollowerMsgHeartbeatHandler(raft)
	handlerMap[pb.MessageType_MsgHeartbeatResponse] = NewNoopHandler()
	handlerMap[pb.MessageType_MsgPropose] = NewFollowerMsgProposeHandler(raft)
	handlerMap[pb.MessageType_MsgAppend] = NewMsgAppendHandler(raft)
	handlerMap[pb.MessageType_MsgAppendResponse] = NewNoopHandler()
	handlerMap[pb.MessageType_MsgSnapshot] = NewMsgSnapshotHandler(raft)
	handlerMap[pb.MessageType_MsgTransferLeader] = NewNotLeaderMsgTransferLeaderHandler(raft)
	handlerMap[pb.MessageType_MsgTimeoutNow] = NewMsgHupAndMsgTimeoutNowHandler(raft)
	return &FollowerRole{
		handlerMap: handlerMap,
		raft:       raft,
	}
}

func (role *FollowerRole) Handle(m pb.Message) error {
	return role.handlerMap[m.GetMsgType()].Handle(m)
}

// 用于处理follower的tick
func (role *FollowerRole) Tick() {
	r := role.raft
	r.electionElapsed++
	if r.electionElapsed >= r.electionTimeout {
		r.Step(pb.Message{From: r.id, To: r.id, MsgType: pb.MessageType_MsgHup})
	}
}
