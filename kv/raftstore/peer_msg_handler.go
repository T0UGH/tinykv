package raftstore

import (
	"fmt"
	"github.com/Connor1996/badger"
	"github.com/pingcap-incubator/tinykv/kv/raftstore/meta"
	"github.com/pingcap-incubator/tinykv/kv/util/engine_util"
	"github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"
	"github.com/pingcap-incubator/tinykv/raft"
	"time"

	"github.com/Connor1996/badger/y"
	"github.com/pingcap-incubator/tinykv/kv/raftstore/message"
	"github.com/pingcap-incubator/tinykv/kv/raftstore/runner"
	"github.com/pingcap-incubator/tinykv/kv/raftstore/snap"
	"github.com/pingcap-incubator/tinykv/kv/raftstore/util"
	"github.com/pingcap-incubator/tinykv/log"
	"github.com/pingcap-incubator/tinykv/proto/pkg/metapb"
	"github.com/pingcap-incubator/tinykv/proto/pkg/raft_cmdpb"
	rspb "github.com/pingcap-incubator/tinykv/proto/pkg/raft_serverpb"
	"github.com/pingcap-incubator/tinykv/scheduler/pkg/btree"
	"github.com/pingcap/errors"
)

type PeerTick int

const (
	PeerTickRaft               PeerTick = 0
	PeerTickRaftLogGC          PeerTick = 1
	PeerTickSplitRegionCheck   PeerTick = 2
	PeerTickSchedulerHeartbeat PeerTick = 3
)

type peerMsgHandler struct {
	*peer
	ctx *GlobalContext
}

func newPeerMsgHandler(peer *peer, ctx *GlobalContext) *peerMsgHandler {
	return &peerMsgHandler{
		peer: peer,
		ctx:  ctx,
	}
}

func (d *peerMsgHandler) HandleRaftReady() {
	if d.stopped {
		return
	}
	// Your Code Here (2B).
	rd := d.RaftGroup.Ready()
	for _, entry := range rd.CommittedEntries {
		if entry.EntryType == eraftpb.EntryType_EntryConfChange {
			d.applyConfChange(entry)
		} else {
			d.applyEntry(entry)
		}
	}
	// 先保存后发送消息
	d.maybeUpdateRegionBySnap(&rd)
	_, err := d.peer.peerStorage.SaveReadyState(&rd)
	if err != nil {
		log.Error(err)
		return
	}
	d.Send(d.ctx.trans, rd.Messages)
	d.RaftGroup.Advance(rd)
}

func (d *peerMsgHandler) HandleMsg(msg message.Msg) {
	switch msg.Type {
	case message.MsgTypeRaftMessage:
		raftMsg := msg.Data.(*rspb.RaftMessage)
		if err := d.onRaftMsg(raftMsg); err != nil {
			log.Errorf("%s handle raft message error %v", d.Tag, err)
		}
	case message.MsgTypeRaftCmd:
		raftCMD := msg.Data.(*message.MsgRaftCmd)
		d.proposeRaftCommand(raftCMD.Request, raftCMD.Callback)
	case message.MsgTypeTick:
		d.onTick()
	case message.MsgTypeSplitRegion:
		split := msg.Data.(*message.MsgSplitRegion)
		log.Infof("%s on split with %v", d.Tag, split.SplitKey)
		d.onPrepareSplitRegion(split.RegionEpoch, split.SplitKey, split.Callback)
	case message.MsgTypeRegionApproximateSize:
		d.onApproximateRegionSize(msg.Data.(uint64))
	case message.MsgTypeGcSnap:
		gcSnap := msg.Data.(*message.MsgGCSnap)
		d.onGCSnap(gcSnap.Snaps)
	case message.MsgTypeStart:
		d.startTicker()
	}
}

func (d *peerMsgHandler) applyConfChange(ent eraftpb.Entry) {

	// 解码
	var cc eraftpb.ConfChange
	if err := cc.Unmarshal(ent.Data); err != nil {
		panic(err)
	}
	msg := &raft_cmdpb.RaftCmdRequest{}
	if err := msg.Unmarshal(cc.Context); err != nil {
		panic(err)
	}

	// 新建Resp
	resp := newCmdResp()

	//从peerStorage中取出内存中的Region
	region := d.Region()

	// 寻找本次需要change的peer在region列表中是否存在
	peerIdx := -1
	for i, v := range region.Peers {
		if v.Id == cc.NodeId {
			peerIdx = i
			break
		}
	}

	// 根据不同的changeType来对region中的peer列表进行变更
	changed := false // 一个flag用来判断region是否被修改了
	switch cc.ChangeType {
	case eraftpb.ConfChangeType_AddNode:
		if peerIdx == -1 {
			changed = true
			region.Peers = append(region.Peers, msg.AdminRequest.ChangePeer.Peer)
			d.insertPeerCache(msg.AdminRequest.ChangePeer.Peer)
		}
	case eraftpb.ConfChangeType_RemoveNode:
		if cc.NodeId == d.PeerId() {
			d.destroyPeer()
			return
		} else {
			// remove idx from peers
			if peerIdx != -1 {
				changed = true
				region.Peers = append(region.Peers[:peerIdx], region.Peers[peerIdx+1:]...)
				d.removePeerCache(cc.NodeId)
			}
		}
	default:
		panic("unexpected conf type")
	}

	// 如果发生了更改就更新peerStorage,DB,ctx, 统统都更新
	if changed {
		region.RegionEpoch.ConfVer += 1
		d.updateRegion(region)
		d.RaftGroup.ApplyConfChange(cc)
	}

	// 构建resp并且调用cb回调它
	resp.AdminResponse = &raft_cmdpb.AdminResponse{
		CmdType: raft_cmdpb.AdminCmdType_ChangePeer,
		ChangePeer: &raft_cmdpb.ChangePeerResponse{
			Region: region,
		},
	}
	d.finishCallback(ent.Index, ent.Term, resp, nil)
}

func (d *peerMsgHandler) preProposeRaftCommand(req *raft_cmdpb.RaftCmdRequest) error {
	// 检查store_id, 确保msg被分发到正确的地方
	// Check store_id, make sure that the msg is dispatched to the right place.
	if err := util.CheckStoreID(req, d.storeID()); err != nil {
		return err
	}

	// 检查这个store是否有正确的peer来处理这个请求, 这个peer必须是leader才行
	// Check whether the store has the right peer to handle the request.
	regionID := d.regionId
	leaderID := d.LeaderId()
	if !d.IsLeader() {
		leader := d.getPeerFromCache(leaderID)
		return &util.ErrNotLeader{RegionId: regionID, Leader: leader}
	}
	// 检查peer_id是否一致
	// peer_id must be the same as peer's.
	if err := util.CheckPeerID(req, d.PeerId()); err != nil {
		return err
	}
	// Check whether the term is stale.
	// 检查这个term是否已经过时
	if err := util.CheckTerm(req, d.Term()); err != nil {
		return err
	}
	// 检查regionEpoch
	err := util.CheckRegionEpoch(req, d.Region(), true)
	if errEpochNotMatching, ok := err.(*util.ErrEpochNotMatch); ok {
		// Attach the region which might be split from the current region. But it doesn't
		// matter if the region is not split from the current region. If the region meta
		// received by the TiKV driver is newer than the meta cached in the driver, the meta is
		// updated.
		siblingRegion := d.findSiblingRegion()
		if siblingRegion != nil {
			errEpochNotMatching.Regions = append(errEpochNotMatching.Regions, siblingRegion)
		}
		return errEpochNotMatching
	}
	return err
}

func (d *peerMsgHandler) proposeRaftCommand(msg *raft_cmdpb.RaftCmdRequest, cb *message.Callback) {
	// 预Propose 可能就是检查检查是否合法之类的
	err := d.preProposeRaftCommand(msg)
	if err != nil {
		cb.Done(ErrResp(err))
		return
	}
	// transferLeader直接调直接返回
	if msg.AdminRequest != nil && msg.AdminRequest.CmdType == raft_cmdpb.AdminCmdType_TransferLeader {
		transferee := msg.AdminRequest.TransferLeader.Peer.Id
		d.RaftGroup.TransferLeader(transferee)
		resp := new(raft_cmdpb.AdminResponse)
		resp.CmdType = raft_cmdpb.AdminCmdType_TransferLeader
		resp.TransferLeader = new(raft_cmdpb.TransferLeaderResponse)
		cb.Done(&raft_cmdpb.RaftCmdResponse{Header: &raft_cmdpb.RaftResponseHeader{
			CurrentTerm: d.Term()}, AdminResponse: resp})
		return
	}
	// Your Code Here (2B).
	d.peer.proposals = append(d.peer.proposals, &proposal{
		term:  d.Term(),
		index: d.nextIndex(),
		cb:    cb,
	})
	// 判断是不是要ChangePeer，如果是，则调用ProposeConfChange
	if msg.AdminRequest != nil && msg.AdminRequest.CmdType == raft_cmdpb.AdminCmdType_ChangePeer {
		msgData, _ := msg.Marshal()
		cc := eraftpb.ConfChange{
			ChangeType: msg.AdminRequest.ChangePeer.ChangeType,
			NodeId:     msg.AdminRequest.ChangePeer.Peer.Id,
			Context:    msgData,
		}
		err = d.RaftGroup.ProposeConfChange(cc)
		if err != nil {
			cb.Done(ErrResp(err))
			return
		}
		return
	}
	data, err := msg.Marshal()
	if err != nil {
		panic(err)
	}
	if err := d.peer.RaftGroup.Propose(data); err != nil {
		cb.Done(ErrResp(err))
		return
	}
}

func (d *peerMsgHandler) onTick() {
	if d.stopped {
		return
	}
	d.ticker.tickClock()
	if d.ticker.isOnTick(PeerTickRaft) {
		d.onRaftBaseTick()
	}
	if d.ticker.isOnTick(PeerTickRaftLogGC) {
		d.onRaftGCLogTick()
	}
	if d.ticker.isOnTick(PeerTickSchedulerHeartbeat) {
		d.onSchedulerHeartbeatTick()
	}
	if d.ticker.isOnTick(PeerTickSplitRegionCheck) {
		d.onSplitRegionCheckTick()
	}
	d.ctx.tickDriverSender <- d.regionId
}

func (d *peerMsgHandler) startTicker() {
	d.ticker = newTicker(d.regionId, d.ctx.cfg)
	d.ctx.tickDriverSender <- d.regionId
	d.ticker.schedule(PeerTickRaft)
	d.ticker.schedule(PeerTickRaftLogGC)
	d.ticker.schedule(PeerTickSplitRegionCheck)
	d.ticker.schedule(PeerTickSchedulerHeartbeat)
}

func (d *peerMsgHandler) onRaftBaseTick() {
	d.RaftGroup.Tick()
	d.ticker.schedule(PeerTickRaft)
}

// 规划压缩日志, 它发送一个压缩任务给Raftlog-gc worker
func (d *peerMsgHandler) scheduleCompactLog(truncatedIndex uint64) {
	raftLogGCTask := &runner.RaftLogGCTask{
		RaftEngine: d.ctx.engine.Raft,
		RegionID:   d.regionId,
		StartIdx:   d.LastCompactedIdx,
		EndIdx:     truncatedIndex + 1,
	}
	d.LastCompactedIdx = raftLogGCTask.EndIdx
	// 实际上被这里收到并处理了 kv/raftstore/runner/raftlog_gc.go:80
	// 只删除log不生成Snapshot
	d.ctx.raftLogGCTaskSender <- raftLogGCTask
}

func (d *peerMsgHandler) onRaftMsg(msg *rspb.RaftMessage) error {
	log.Debugf("%s handle raft message %s from %d to %d",
		d.Tag, msg.GetMessage().GetMsgType(), msg.GetFromPeer().GetId(), msg.GetToPeer().GetId())
	if !d.validateRaftMessage(msg) {
		return nil
	}
	if d.stopped {
		return nil
	}
	if msg.GetIsTombstone() {
		// we receive a message tells us to remove self.
		d.handleGCPeerMsg(msg)
		return nil
	}
	if d.checkMessage(msg) {
		return nil
	}
	key, err := d.checkSnapshot(msg)
	if err != nil {
		return err
	}
	if key != nil {
		// If the snapshot file is not used again, then it's OK to
		// delete them here. If the snapshot file will be reused when
		// receiving, then it will fail to pass the check again, so
		// missing snapshot files should not be noticed.
		s, err1 := d.ctx.snapMgr.GetSnapshotForApplying(*key)
		if err1 != nil {
			return err1
		}
		d.ctx.snapMgr.DeleteSnapshot(*key, s, false)
		return nil
	}
	d.insertPeerCache(msg.GetFromPeer())
	err = d.RaftGroup.Step(*msg.GetMessage())
	if err != nil {
		return err
	}
	if d.AnyNewPeerCatchUp(msg.FromPeer.Id) {
		d.HeartbeatScheduler(d.ctx.schedulerTaskSender)
	}
	return nil
}

// return false means the message is invalid, and can be ignored.
func (d *peerMsgHandler) validateRaftMessage(msg *rspb.RaftMessage) bool {
	regionID := msg.GetRegionId()
	from := msg.GetFromPeer()
	to := msg.GetToPeer()
	log.Debugf("[region %d] handle raft message %s from %d to %d", regionID, msg, from.GetId(), to.GetId())
	if to.GetStoreId() != d.storeID() {
		log.Warnf("[region %d] store not match, to store id %d, mine %d, ignore it",
			regionID, to.GetStoreId(), d.storeID())
		return false
	}
	if msg.RegionEpoch == nil {
		log.Errorf("[region %d] missing epoch in raft message, ignore it", regionID)
		return false
	}
	return true
}

/// Checks if the message is sent to the correct peer.
///
/// Returns true means that the message can be dropped silently.
func (d *peerMsgHandler) checkMessage(msg *rspb.RaftMessage) bool {
	fromEpoch := msg.GetRegionEpoch()
	isVoteMsg := util.IsVoteMessage(msg.Message)
	fromStoreID := msg.FromPeer.GetStoreId()

	// Let's consider following cases with three nodes [1, 2, 3] and 1 is leader:
	// a. 1 removes 2, 2 may still send MsgAppendResponse to 1.
	//  We should ignore this stale message and let 2 remove itself after
	//  applying the ConfChange log.
	// b. 2 is isolated, 1 removes 2. When 2 rejoins the cluster, 2 will
	//  send stale MsgRequestVote to 1 and 3, at this time, we should tell 2 to gc itself.
	// c. 2 is isolated but can communicate with 3. 1 removes 3.
	//  2 will send stale MsgRequestVote to 3, 3 should ignore this message.
	// d. 2 is isolated but can communicate with 3. 1 removes 2, then adds 4, remove 3.
	//  2 will send stale MsgRequestVote to 3, 3 should tell 2 to gc itself.
	// e. 2 is isolated. 1 adds 4, 5, 6, removes 3, 1. Now assume 4 is leader.
	//  After 2 rejoins the cluster, 2 may send stale MsgRequestVote to 1 and 3,
	//  1 and 3 will ignore this message. Later 4 will send messages to 2 and 2 will
	//  rejoin the raft group again.
	// f. 2 is isolated. 1 adds 4, 5, 6, removes 3, 1. Now assume 4 is leader, and 4 removes 2.
	//  unlike case e, 2 will be stale forever.
	// TODO: for case f, if 2 is stale for a long time, 2 will communicate with scheduler and scheduler will
	// tell 2 is stale, so 2 can remove itself.
	region := d.Region()
	if util.IsEpochStale(fromEpoch, region.RegionEpoch) && util.FindPeer(region, fromStoreID) == nil {
		// The message is stale and not in current region.
		handleStaleMsg(d.ctx.trans, msg, region.RegionEpoch, isVoteMsg)
		return true
	}
	target := msg.GetToPeer()
	if target.Id < d.PeerId() {
		log.Infof("%s target peer ID %d is less than %d, msg maybe stale", d.Tag, target.Id, d.PeerId())
		return true
	} else if target.Id > d.PeerId() {
		if d.MaybeDestroy() {
			log.Infof("%s is stale as received a larger peer %s, destroying", d.Tag, target)
			d.destroyPeer()
			d.ctx.router.sendStore(message.NewMsg(message.MsgTypeStoreRaftMessage, msg))
		}
		return true
	}
	return false
}

func handleStaleMsg(trans Transport, msg *rspb.RaftMessage, curEpoch *metapb.RegionEpoch,
	needGC bool) {
	regionID := msg.RegionId
	fromPeer := msg.FromPeer
	toPeer := msg.ToPeer
	msgType := msg.Message.GetMsgType()

	if !needGC {
		log.Infof("[region %d] raft message %s is stale, current %v ignore it",
			regionID, msgType, curEpoch)
		return
	}
	gcMsg := &rspb.RaftMessage{
		RegionId:    regionID,
		FromPeer:    toPeer,
		ToPeer:      fromPeer,
		RegionEpoch: curEpoch,
		IsTombstone: true,
	}
	if err := trans.Send(gcMsg); err != nil {
		log.Errorf("[region %d] send message failed %v", regionID, err)
	}
}

func (d *peerMsgHandler) handleGCPeerMsg(msg *rspb.RaftMessage) {
	fromEpoch := msg.RegionEpoch
	if !util.IsEpochStale(d.Region().RegionEpoch, fromEpoch) {
		return
	}
	if !util.PeerEqual(d.Meta, msg.ToPeer) {
		log.Infof("%s receive stale gc msg, ignore", d.Tag)
		return
	}
	log.Infof("%s peer %s receives gc message, trying to remove", d.Tag, msg.ToPeer)
	if d.MaybeDestroy() {
		d.destroyPeer()
	}
}

// Returns `None` if the `msg` doesn't contain a snapshot or it contains a snapshot which
// doesn't conflict with any other snapshots or regions. Otherwise a `snap.SnapKey` is returned.
func (d *peerMsgHandler) checkSnapshot(msg *rspb.RaftMessage) (*snap.SnapKey, error) {
	if msg.Message.Snapshot == nil {
		return nil, nil
	}
	regionID := msg.RegionId
	snapshot := msg.Message.Snapshot
	key := snap.SnapKeyFromRegionSnap(regionID, snapshot)
	snapData := new(rspb.RaftSnapshotData)
	err := snapData.Unmarshal(snapshot.Data)
	if err != nil {
		return nil, err
	}
	snapRegion := snapData.Region
	peerID := msg.ToPeer.Id
	var contains bool
	for _, peer := range snapRegion.Peers {
		if peer.Id == peerID {
			contains = true
			break
		}
	}
	if !contains {
		log.Infof("%s %s doesn't contains peer %d, skip", d.Tag, snapRegion, peerID)
		return &key, nil
	}
	meta := d.ctx.storeMeta
	meta.Lock()
	defer meta.Unlock()
	if !util.RegionEqual(meta.regions[d.regionId], d.Region()) {
		if !d.isInitialized() {
			log.Infof("%s stale delegate detected, skip", d.Tag)
			return &key, nil
		} else {
			panic(fmt.Sprintf("%s meta corrupted %s != %s", d.Tag, meta.regions[d.regionId], d.Region()))
		}
	}

	existRegions := meta.getOverlapRegions(snapRegion)
	for _, existRegion := range existRegions {
		if existRegion.GetId() == snapRegion.GetId() {
			continue
		}
		log.Infof("%s region overlapped %s %s", d.Tag, existRegion, snapRegion)
		return &key, nil
	}

	// check if snapshot file exists.
	_, err = d.ctx.snapMgr.GetSnapshotForApplying(key)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (d *peerMsgHandler) destroyPeer() {
	log.Infof("%s starts destroy", d.Tag)
	regionID := d.regionId
	// We can't destroy a peer which is applying snapshot.
	meta := d.ctx.storeMeta
	meta.Lock()
	defer meta.Unlock()
	isInitialized := d.isInitialized()
	if err := d.Destroy(d.ctx.engine, false); err != nil {
		// If not panic here, the peer will be recreated in the next restart,
		// then it will be gc again. But if some overlap region is created
		// before restarting, the gc action will delete the overlap region's
		// data too.
		panic(fmt.Sprintf("%s destroy peer %v", d.Tag, err))
	}
	d.ctx.router.close(regionID)
	d.stopped = true
	if isInitialized && meta.regionRanges.Delete(&regionItem{region: d.Region()}) == nil {
		panic(d.Tag + " meta corruption detected")
	}
	if _, ok := meta.regions[regionID]; !ok {
		panic(d.Tag + " meta corruption detected")
	}
	delete(meta.regions, regionID)
}

func (d *peerMsgHandler) findSiblingRegion() (result *metapb.Region) {
	meta := d.ctx.storeMeta
	meta.RLock()
	defer meta.RUnlock()
	item := &regionItem{region: d.Region()}
	meta.regionRanges.AscendGreaterOrEqual(item, func(i btree.Item) bool {
		result = i.(*regionItem).region
		return true
	})
	return
}

func (d *peerMsgHandler) onRaftGCLogTick() {
	d.ticker.schedule(PeerTickRaftLogGC)
	// 只有leader才会触发onRaftGCLogTick()
	if !d.IsLeader() {
		return
	}

	appliedIdx := d.peerStorage.AppliedIndex()
	firstIdx, _ := d.peerStorage.FirstIndex()
	var compactIdx uint64
	// 如果超过了GcCountLimit就会触发GC
	if appliedIdx > firstIdx && appliedIdx-firstIdx >= d.ctx.cfg.RaftLogGcCountLimit {
		compactIdx = appliedIdx
	} else {
		return
	}

	y.Assert(compactIdx > 0)
	compactIdx -= 1
	if compactIdx < firstIdx {
		// In case compact_idx == first_idx before subtraction.
		return
	}

	term, err := d.RaftGroup.Raft.RaftLog.Term(compactIdx)
	if err != nil {
		log.Fatalf("appliedIdx: %d, firstIdx: %d, compactIdx: %d", appliedIdx, firstIdx, compactIdx)
		panic(err)
	}

	// Create a compact log request and notify directly.
	regionID := d.regionId
	request := newCompactLogRequest(regionID, d.Meta, compactIdx, term)
	d.proposeRaftCommand(request, nil)
}

func (d *peerMsgHandler) onSplitRegionCheckTick() {
	d.ticker.schedule(PeerTickSplitRegionCheck)
	// To avoid frequent scan, we only add new scan tasks if all previous tasks
	// have finished.
	if len(d.ctx.splitCheckTaskSender) > 0 {
		return
	}

	if !d.IsLeader() {
		return
	}
	if d.ApproximateSize != nil && d.SizeDiffHint < d.ctx.cfg.RegionSplitSize/8 {
		return
	}
	d.ctx.splitCheckTaskSender <- &runner.SplitCheckTask{
		Region: d.Region(),
	}
	d.SizeDiffHint = 0
}

func (d *peerMsgHandler) onPrepareSplitRegion(regionEpoch *metapb.RegionEpoch, splitKey []byte, cb *message.Callback) {
	if err := d.validateSplitRegion(regionEpoch, splitKey); err != nil {
		cb.Done(ErrResp(err))
		return
	}
	region := d.Region()
	d.ctx.schedulerTaskSender <- &runner.SchedulerAskSplitTask{
		Region:   region,
		SplitKey: splitKey,
		Peer:     d.Meta,
		Callback: cb,
	}
}

func (d *peerMsgHandler) validateSplitRegion(epoch *metapb.RegionEpoch, splitKey []byte) error {
	if len(splitKey) == 0 {
		err := errors.Errorf("%s split key should not be empty", d.Tag)
		log.Error(err)
		return err
	}

	if !d.IsLeader() {
		// `region` on this store is no longer leader, skipped.
		log.Infof("%s not leader, skip", d.Tag)
		return &util.ErrNotLeader{
			RegionId: d.regionId,
			Leader:   d.getPeerFromCache(d.LeaderId()),
		}
	}

	region := d.Region()
	latestEpoch := region.GetRegionEpoch()

	// This is a little difference for `check_region_epoch` in region split case.
	// Here we just need to check `version` because `conf_ver` will be update
	// to the latest value of the peer, and then send to Scheduler.
	if latestEpoch.Version != epoch.Version {
		log.Infof("%s epoch changed, retry later, prev_epoch: %s, epoch %s",
			d.Tag, latestEpoch, epoch)
		return &util.ErrEpochNotMatch{
			Message: fmt.Sprintf("%s epoch changed %s != %s, retry later", d.Tag, latestEpoch, epoch),
			Regions: []*metapb.Region{region},
		}
	}
	return nil
}

func (d *peerMsgHandler) onApproximateRegionSize(size uint64) {
	d.ApproximateSize = &size
}

func (d *peerMsgHandler) onSchedulerHeartbeatTick() {
	d.ticker.schedule(PeerTickSchedulerHeartbeat)

	if !d.IsLeader() {
		return
	}
	d.HeartbeatScheduler(d.ctx.schedulerTaskSender)
}

func (d *peerMsgHandler) onGCSnap(snaps []snap.SnapKeyWithSending) {
	compactedIdx := d.peerStorage.truncatedIndex()
	compactedTerm := d.peerStorage.truncatedTerm()
	for _, snapKeyWithSending := range snaps {
		key := snapKeyWithSending.SnapKey
		if snapKeyWithSending.IsSending {
			snap, err := d.ctx.snapMgr.GetSnapshotForSending(key)
			if err != nil {
				log.Errorf("%s failed to load snapshot for %s %v", d.Tag, key, err)
				continue
			}
			if key.Term < compactedTerm || key.Index < compactedIdx {
				log.Infof("%s snap file %s has been compacted, delete", d.Tag, key)
				d.ctx.snapMgr.DeleteSnapshot(key, snap, false)
			} else if fi, err1 := snap.Meta(); err1 == nil {
				modTime := fi.ModTime()
				if time.Since(modTime) > 4*time.Hour {
					log.Infof("%s snap file %s has been expired, delete", d.Tag, key)
					d.ctx.snapMgr.DeleteSnapshot(key, snap, false)
				}
			}
		} else if key.Term <= compactedTerm &&
			(key.Index < compactedIdx || key.Index == compactedIdx) {
			log.Infof("%s snap file %s has been applied, delete", d.Tag, key)
			a, err := d.ctx.snapMgr.GetSnapshotForApplying(key)
			if err != nil {
				log.Errorf("%s failed to load snapshot for %s %v", d.Tag, key, err)
				continue
			}
			d.ctx.snapMgr.DeleteSnapshot(key, a, false)
		}
	}
}

func newAdminRequest(regionID uint64, peer *metapb.Peer) *raft_cmdpb.RaftCmdRequest {
	return &raft_cmdpb.RaftCmdRequest{
		Header: &raft_cmdpb.RaftRequestHeader{
			RegionId: regionID,
			Peer:     peer,
		},
	}
}

func newCompactLogRequest(regionID uint64, peer *metapb.Peer, compactIndex, compactTerm uint64) *raft_cmdpb.RaftCmdRequest {
	req := newAdminRequest(regionID, peer)
	req.AdminRequest = &raft_cmdpb.AdminRequest{
		CmdType: raft_cmdpb.AdminCmdType_CompactLog,
		CompactLog: &raft_cmdpb.CompactLogRequest{
			CompactIndex: compactIndex,
			CompactTerm:  compactTerm,
		},
	}
	return req
}

func (d *peerMsgHandler) applyAdminRequest(req *raft_cmdpb.AdminRequest) (*raft_cmdpb.AdminResponse, error) {
	resp := new(raft_cmdpb.AdminResponse)
	resp.CmdType = req.CmdType
	switch req.CmdType {
	case raft_cmdpb.AdminCmdType_CompactLog:
		// 更新State
		d.peerStorage.applyState.TruncatedState.Index = req.CompactLog.CompactIndex
		d.peerStorage.applyState.TruncatedState.Term = req.CompactLog.CompactTerm
		// 压缩内存中的log
		d.RaftGroup.Raft.RaftLog.Compact(req.CompactLog.CompactIndex, req.CompactLog.CompactTerm)
		// 调用ScheduleCompactLog
		d.scheduleCompactLog(req.CompactLog.CompactIndex)
		// 返回
		resp.CompactLog = new(raft_cmdpb.CompactLogResponse)
	case raft_cmdpb.AdminCmdType_Split:
		splitReq := req.Split
		if err := util.CheckKeyInRegion(splitReq.SplitKey, d.Region()); err != nil {
			return nil, err
		}
		oldRegion := d.Region()
		// 1 根据req中解析出来的数据来构建一个region
		newPeers := make([]*metapb.Peer, 0, len(oldRegion.Peers))
		for i, peer := range oldRegion.Peers {
			newPeers = append(newPeers, &metapb.Peer{
				Id:      splitReq.NewPeerIds[i],
				StoreId: peer.StoreId,
			})
		}
		newRegion := &metapb.Region{
			Id:          splitReq.NewRegionId,
			StartKey:    splitReq.SplitKey,
			EndKey:      oldRegion.EndKey,
			RegionEpoch: new(metapb.RegionEpoch),
			Peers:       newPeers,
		}
		storeMeta := d.ctx.storeMeta
		storeMeta.Lock()
		storeMeta.regionRanges.Delete(&regionItem{region: oldRegion})
		// 0 老region信息更新
		oldRegion.RegionEpoch.Version += 1
		oldRegion.EndKey = splitReq.SplitKey
		// 将region更新到ctx中
		storeMeta.regionRanges.ReplaceOrInsert(&regionItem{region: oldRegion})
		storeMeta.regionRanges.ReplaceOrInsert(&regionItem{region: newRegion})
		storeMeta.regions[oldRegion.Id] = oldRegion
		storeMeta.regions[newRegion.Id] = newRegion
		storeMeta.Unlock()
		// 设置内存中的region
		d.SetRegion(oldRegion)
		// 设置db中的region
		kvWB := new(engine_util.WriteBatch)
		meta.WriteRegionState(kvWB, oldRegion, rspb.PeerState_Normal)
		meta.WriteRegionState(kvWB, newRegion, rspb.PeerState_Normal)
		if err := kvWB.WriteToDB(d.ctx.engine.Kv); err != nil {
			return nil, err
		}
		// 用createPeer来创建peer
		peer, err := createPeer(d.storeID(), d.ctx.cfg, d.ctx.regionTaskSender, d.ctx.engine, newRegion)
		if err != nil {
			return nil, err
		}
		if d.IsLeader() {
			d.HeartbeatScheduler(d.ctx.schedulerTaskSender)
		}
		// 将peer注册到router上
		d.ctx.router.register(peer)
		// 启动这个peer的ticker
		d.ctx.router.send(newRegion.Id, message.Msg{Type: message.MsgTypeStart})
		// 构建返回信息
		resp.Split = &raft_cmdpb.SplitResponse{Regions: []*metapb.Region{newRegion}}
	}
	return resp, nil
}

func (d *peerMsgHandler) applyEntry(ent eraftpb.Entry) {
	var cmdReq raft_cmdpb.RaftCmdRequest
	err := cmdReq.Unmarshal(ent.Data)
	if err != nil {
		panic(err)
	}
	resp := newCmdResp()
	BindRespTerm(resp, ent.Term)
	var txn *badger.Txn
	if cmdReq.AdminRequest != nil {
		adminResp, err := d.applyAdminRequest(cmdReq.AdminRequest)
		if err != nil {
			BindRespError(resp, err)
			d.finishCallback(ent.Index, ent.Term, resp, nil)
			return
		}
		resp.AdminResponse = adminResp
	}
	db := d.peerStorage.Engines.Kv
labelFor:
	for _, req := range cmdReq.Requests {
		// todo 判断key的范围是否在RegionRange中
		switch req.CmdType {
		case raft_cmdpb.CmdType_Get:
			{
				getReq := req.Get
				key := getReq.Key
				if err := util.CheckKeyInRegion(key, d.Region()); err != nil {
					BindRespError(resp, err)
					break labelFor
				}
				val, err := engine_util.GetCF(db, getReq.Cf, key)
				if err != nil {
					BindRespError(resp, err)
					break labelFor
				}
				r := &raft_cmdpb.Response{
					CmdType: req.CmdType,
					Get:     &raft_cmdpb.GetResponse{Value: val},
				}
				resp.Responses = append(resp.Responses, r)
			}
		case raft_cmdpb.CmdType_Delete:
			{
				delReq := req.Delete
				key := delReq.Key
				if err := util.CheckKeyInRegion(key, d.Region()); err != nil {
					BindRespError(resp, err)
					break labelFor
				}
				err := engine_util.DeleteCF(db, delReq.Cf, key)
				if err != nil {
					BindRespError(resp, err)
					break labelFor
				}
				r := &raft_cmdpb.Response{
					CmdType: req.CmdType,
					Delete:  &raft_cmdpb.DeleteResponse{},
				}
				resp.Responses = append(resp.Responses, r)
			}
		case raft_cmdpb.CmdType_Put:
			{
				putReq := req.Put
				key := putReq.Key
				if err := util.CheckKeyInRegion(key, d.Region()); err != nil {
					BindRespError(resp, err)
					break labelFor
				}
				err := engine_util.PutCF(db, putReq.Cf, key, putReq.Value)
				if err != nil {
					BindRespError(resp, err)
					break labelFor
				}
				r := &raft_cmdpb.Response{
					CmdType: req.CmdType,
					Put:     &raft_cmdpb.PutResponse{},
				}
				resp.Responses = append(resp.Responses, r)
			}
		case raft_cmdpb.CmdType_Snap:
			{
				txn = d.peerStorage.Engines.Kv.NewTransaction(false)
				r := &raft_cmdpb.Response{
					CmdType: req.CmdType,
					Snap:    &raft_cmdpb.SnapResponse{Region: d.peerStorage.region},
				}
				resp.Responses = append(resp.Responses, r)
			}
		}
	}
	d.finishCallback(ent.Index, ent.Term, resp, txn)
}

func (d *peerMsgHandler) finishCallback(index, term uint64, resp *raft_cmdpb.RaftCmdResponse, txn *badger.Txn) {
	cb := d.findCallback(index, term)
	if cb != nil {
		if txn != nil {
			cb.Txn = txn
		}
		cb.Done(resp)
	}
}

func (d *peerMsgHandler) updateRegion(region *metapb.Region) {
	// 设置db中的region
	kvWB := new(engine_util.WriteBatch)
	meta.WriteRegionState(kvWB, region, rspb.PeerState_Normal)
	if err := kvWB.WriteToDB(d.ctx.engine.Kv); err != nil {
		return
	}
	// 设置peer storage内存中的region
	d.SetRegion(region)
	// 设置ctx中的region
	storeMeta := d.ctx.storeMeta
	storeMeta.Lock()
	storeMeta.regions[d.regionId] = region
	storeMeta.regionRanges.ReplaceOrInsert(&regionItem{region: region})
	storeMeta.Unlock()
}

// when receiving a snapshot, maybe use snapshot info update region
func (d *peerMsgHandler) maybeUpdateRegionBySnap(rd *raft.Ready) {
	if rd.Snapshot.Metadata == nil {
		return
	}
	snapData := new(rspb.RaftSnapshotData)
	if err := snapData.Unmarshal(rd.Snapshot.Data); err != nil {
		log.Error(err)
		return
	}
	snapRegion := snapData.GetRegion()
	oldRegion := d.Region()
	if oldRegion.RegionEpoch.Version < snapRegion.RegionEpoch.Version ||
		oldRegion.RegionEpoch.ConfVer < snapRegion.RegionEpoch.ConfVer {
		d.updateRegion(snapRegion)
	}

}

func (d *peerMsgHandler) nextIndex() uint64 {
	return d.peer.RaftGroup.Raft.RaftLog.LastIndex() + 1
}
