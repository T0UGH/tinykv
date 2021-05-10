package raftstore

import (
	"sync"

	"github.com/pingcap-incubator/tinykv/kv/raftstore/message"
)

// raftWorker is responsible for run raft commands and apply raft logs.
// raftWorker用于运行raft命令并且应用raft日志
type raftWorker struct {
	// 路由, 用来找peer
	pr *router

	// receiver of messages should sent to raft, including:
	// * raft command from `raftStorage`
	// * raft inner messages from other peers sent by network
	// raftWorker用这个ch来接收消息，包括
	// * 从`raftStorage`发送的raft command
	// * 从其他peers通过网络发送来的内部消息
	raftCh chan message.Msg
	ctx    *GlobalContext

	closeCh <-chan struct{}
}

func newRaftWorker(ctx *GlobalContext, pm *router) *raftWorker {
	return &raftWorker{
		raftCh: pm.peerSender,
		ctx:    ctx,
		pr:     pm,
	}
}

// run runs raft commands.
// On each loop, raft commands are batched by channel buffer.
// After commands are handled, we collect apply messages by peers, make a applyBatch, send it to apply channel.
// run 运行 raft 命令
// 在一次循环中, raft命令被channel buffer给batched
// 当命令被处理, 我们从peers中收集apply msg, 创建要给applyBatch, 并且把它发送给apply信道
func (rw *raftWorker) run(closeCh <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	var msgs []message.Msg
	for {
		// 清空msgs
		msgs = msgs[:0]
		select {
		// 当closeCh发来了消息就退出这个run
		case <-closeCh:
			return
		// 	当raftCh发来了消息就把消息加到msg里面
		case msg := <-rw.raftCh:
			msgs = append(msgs, msg)
		}
		// 看看rw.raftCh里面积压了多少消息
		pending := len(rw.raftCh)
		// 把积压的消息全都装到msg里面
		for i := 0; i < pending; i++ {
			msgs = append(msgs, <-rw.raftCh)
		}
		// 造一个空map
		peerStateMap := make(map[uint64]*peerState)
		// 遍历msgs
		for _, msg := range msgs {
			// 根据msg中的RegionID来获取peerState
			peerState := rw.getPeerState(peerStateMap, msg.RegionID)
			// 没有获取到就接着处理下一条消息
			if peerState == nil {
				continue
			}
			// 新建一个PeerMsgHandler并且调用它的HandleMsg来处理消息
			newPeerMsgHandler(peerState.peer, rw.ctx).HandleMsg(msg)
		}
		// 遍历peerStateMap, 也就是刚才处理过消息的peerState, 处理RaftReady
		for _, peerState := range peerStateMap {
			// 新建一个PeerMsgHandler并且调用它的HandleRaftReady来处理RaftReady
			newPeerMsgHandler(peerState.peer, rw.ctx).HandleRaftReady()
		}
	}
}

// 获取PeerState
func (rw *raftWorker) getPeerState(peersMap map[uint64]*peerState, regionID uint64) *peerState {
	// 先看看map里面有没有, 有的话就用map里面的
	peer, ok := peersMap[regionID]
	// map里面没有就再取
	if !ok {
		// 从router里面根据regionID来获取peer
		peer = rw.pr.get(regionID)
		// 如果不存在直接返回nil
		if peer == nil {
			return nil
		}
		// 存在的话把它放到map里面并且返回它
		peersMap[regionID] = peer
	}
	return peer
}
