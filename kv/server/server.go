package server

import (
	"context"
	"github.com/Connor1996/badger"
	"github.com/pingcap-incubator/tinykv/kv/transaction/mvcc"
	"github.com/pingcap-incubator/tinykv/kv/util/engine_util"

	"github.com/pingcap-incubator/tinykv/kv/coprocessor"
	"github.com/pingcap-incubator/tinykv/kv/storage"
	"github.com/pingcap-incubator/tinykv/kv/storage/raft_storage"
	"github.com/pingcap-incubator/tinykv/kv/transaction/latches"
	coppb "github.com/pingcap-incubator/tinykv/proto/pkg/coprocessor"
	"github.com/pingcap-incubator/tinykv/proto/pkg/kvrpcpb"
	"github.com/pingcap-incubator/tinykv/proto/pkg/tinykvpb"
	"github.com/pingcap/tidb/kv"
)

var _ tinykvpb.TinyKvServer = new(Server)

// Server里面主要就是有一个Storage
// Server是一个TinnyKV server, 它面向外部, 从像TinySQL这样的客户端发送和接收消息
// Server is a TinyKV server, it 'faces outwards', sending and receiving messages from clients such as TinySQL.
type Server struct {
	storage storage.Storage

	// (Used in 4A/4B)
	Latches *latches.Latches

	// coprocessor API handler, out of course scope
	copHandler *coprocessor.CopHandler
}

func NewServer(storage storage.Storage) *Server {
	return &Server{
		storage: storage,
		Latches: latches.NewLatches(),
	}
}

// The below functions are Server's gRPC API (implements TinyKvServer).

// Raw API.
func (server *Server) RawGet(_ context.Context, req *kvrpcpb.RawGetRequest) (*kvrpcpb.RawGetResponse, error) {
	// Your Code Here (1).
	reader, err := server.storage.Reader(req.Context)
	defer reader.Close()
	if err != nil {
		return &kvrpcpb.RawGetResponse{Error: err.Error()}, err
	}

	val, err := reader.GetCF(req.Cf, req.Key)

	// 处理没有找到
	if err == badger.ErrKeyNotFound {
		return &kvrpcpb.RawGetResponse{
			NotFound: true,
		}, nil
	}

	resp := &kvrpcpb.RawGetResponse{
		Value: val,
	}
	if err != nil {
		resp.Error = err.Error()
	}
	return resp, err
}

func (server *Server) RawPut(_ context.Context, req *kvrpcpb.RawPutRequest) (*kvrpcpb.RawPutResponse, error) {
	// Your Code Here (1).
	batch := make([]storage.Modify, 0)
	data := storage.Put{
		Key:   req.Key,
		Value: req.Value,
		Cf:    req.Cf,
	}
	batch = append(batch, storage.Modify{Data: data})
	err := server.storage.Write(req.Context, batch)

	if err != nil {
		return &kvrpcpb.RawPutResponse{
			Error: err.Error(),
		}, err
	}

	return &kvrpcpb.RawPutResponse{}, nil
}

func (server *Server) RawDelete(_ context.Context, req *kvrpcpb.RawDeleteRequest) (*kvrpcpb.RawDeleteResponse, error) {
	// Your Code Here (1).
	batch := make([]storage.Modify, 0)
	data := storage.Delete{
		Key: req.Key,
		Cf:  req.Cf,
	}
	batch = append(batch, storage.Modify{Data: data})
	err := server.storage.Write(req.Context, batch)

	if err != nil {
		return &kvrpcpb.RawDeleteResponse{
			Error: err.Error(),
		}, err
	}

	return &kvrpcpb.RawDeleteResponse{}, nil
}

func (server *Server) RawScan(_ context.Context, req *kvrpcpb.RawScanRequest) (*kvrpcpb.RawScanResponse, error) {
	// Your Code Here (1).
	reader, err := server.storage.Reader(req.Context)
	defer reader.Close()
	if err != nil {
		return &kvrpcpb.RawScanResponse{
			Error: err.Error(),
		}, err
	}

	iter := reader.IterCF(req.Cf)
	iter.Seek(req.StartKey)
	if !iter.Valid() {
		return &kvrpcpb.RawScanResponse{}, nil
	}

	var kvPairs = make([]*kvrpcpb.KvPair, 0)

	for i := 0; i < int(req.Limit); i++ {
		item := iter.Item()
		key := item.Key()
		value, _ := item.Value()
		//todo: keyError 很多种
		kvPairs = append(kvPairs, &kvrpcpb.KvPair{
			Key:   key,
			Value: value,
		})
		iter.Next()
		if !iter.Valid() {
			break
		}
	}
	iter.Close()
	return &kvrpcpb.RawScanResponse{Kvs: kvPairs}, nil
}

// Raft commands (tinykv <-> tinykv)
// Only used for RaftStorage, so trivially forward it.
func (server *Server) Raft(stream tinykvpb.TinyKv_RaftServer) error {
	return server.storage.(*raft_storage.RaftStorage).Raft(stream)
}

// Snapshot stream (tinykv <-> tinykv)
// Only used for RaftStorage, so trivially forward it.
func (server *Server) Snapshot(stream tinykvpb.TinyKv_SnapshotServer) error {
	return server.storage.(*raft_storage.RaftStorage).Snapshot(stream)
}

// Transactional API.
func (server *Server) KvGet(_ context.Context, req *kvrpcpb.GetRequest) (*kvrpcpb.GetResponse, error) {
	// Your Code Here (4B).
	// todo get之前先要获取lock
	resp := new(kvrpcpb.GetResponse)
	reader, err := server.storage.Reader(req.Context)
	defer reader.Close()
	if err != nil {
		return nil, err
	}
	txn := mvcc.NewMvccTxn(reader, req.GetVersion())
	lock, err := txn.GetLock(req.Key)
	if err != nil {
		return nil, err
	}
	// 如果是之后的版本锁了也无所谓，因为我只要startTs之前的版本就行了，之后的更改不会影响我取到之前的版本(mvcc)
	if lock != nil {
		if lock.IsLockedFor(req.Key, req.Version, resp) {
			return resp, nil
		}
	}
	// 直接调用GetValue，GetValue会先在写记录里找，然后找写记录对应的start_ts对应的value
	val, err := txn.GetValue(req.Key)
	if err != nil {
		return nil, err
	}
	if val == nil {
		resp.NotFound = true
	}
	resp.Value = val
	return resp, nil
}

func (server *Server) KvPrewrite(_ context.Context, req *kvrpcpb.PrewriteRequest) (*kvrpcpb.PrewriteResponse, error) {
	// Your Code Here (4B).
	allKeys := [][]byte{}
	for _, m := range req.Mutations {
		allKeys = append(allKeys, m.Key)
	}
	server.Latches.WaitForLatches(allKeys)
	defer server.Latches.ReleaseLatches(allKeys)

	resp := new(kvrpcpb.PrewriteResponse)
	resp.Errors = make([]*kvrpcpb.KeyError, 0)
	reader, err := server.storage.Reader(req.Context)
	defer reader.Close()
	if err != nil {
		return nil, err
	}
	txn := mvcc.NewMvccTxn(reader, req.GetStartVersion())
	//1 先检查所有key有没有被lock, 有没有写入冲突
	if req.Mutations == nil || len(req.Mutations) == 0 {
		return resp, nil
	}
	for _, mut := range req.Mutations {
		// 检查lock
		lock, err := txn.GetLock(mut.Key)
		if err != nil {
			return nil, err
		}
		if lock != nil {
			resp.Errors = append(resp.Errors, &kvrpcpb.KeyError{Locked: lock.Info(mut.Key)})
		}
		// 检查写入冲突
		_, commitTs, err := txn.MostRecentWrite(mut.Key)
		if err != nil {
			return nil, err
		}
		if commitTs >= req.StartVersion {
			resp.Errors = append(resp.Errors, &kvrpcpb.KeyError{
				Conflict: &kvrpcpb.WriteConflict{
					StartTs:    req.StartVersion,
					ConflictTs: commitTs,
					Key:        mut.Key,
					Primary:    req.PrimaryLock,
				},
			})
		}
	}
	if len(resp.Errors) > 0 {
		return resp, nil
	}
	//2 放入锁和Value
	for _, mut := range req.Mutations {
		// todo 这个地方有暗坑, 因为Kind不止两种
		lk := mvcc.WriteKindPut
		if mut.Op == kvrpcpb.Op_Del {
			lk = mvcc.WriteKindDelete
		}

		lock := &mvcc.Lock{
			Primary: req.PrimaryLock,
			Ts:      req.StartVersion,
			Ttl:     req.LockTtl,
			Kind:    lk,
		}
		txn.PutLock(mut.Key, lock)
		if mut.Op == kvrpcpb.Op_Del {
			txn.DeleteValue(mut.Key)
		} else {
			txn.PutValue(mut.Key, mut.Value)
		}
	}
	err = server.storage.Write(req.Context, txn.Writes())
	return resp, err
}

func (server *Server) KvCommit(_ context.Context, req *kvrpcpb.CommitRequest) (*kvrpcpb.CommitResponse, error) {
	// Your Code Here (4B).
	server.Latches.WaitForLatches(req.Keys)
	defer server.Latches.ReleaseLatches(req.Keys)
	resp := new(kvrpcpb.CommitResponse)
	reader, err := server.storage.Reader(req.Context)
	defer reader.Close()
	if err != nil {
		return nil, err
	}
	txn := mvcc.NewMvccTxn(reader, req.GetStartVersion())
	// 判空
	if req.Keys == nil || len(req.Keys) == 0 {
		return resp, nil
	}
	// 去重
	keys := removeDuplicateElement(req.Keys)
	// 这里不对，应该先提交primary key，然后如果secondary key有失败也不算。
	for _, key := range keys {
		lock, err := txn.GetLock(key)
		if err != nil {
			return nil, err
		}
		// 找不到锁直接返回, 这样可以防止重复的commit请求
		if lock == nil {
			return resp, nil
		}
		// 不是我的锁, 返回一个error
		if lock.Ts != req.GetStartVersion() {
			resp.Error = &kvrpcpb.KeyError{
				Retryable: "Retryable",
			}
			return resp, nil
		}
		// 否则删除锁并加一条写提交记录
		write := &mvcc.Write{
			StartTS: req.GetStartVersion(),
			Kind:    lock.Kind,
		}
		txn.PutWrite(key, req.CommitVersion, write)
		txn.DeleteLock(key)
	}
	// 把整个batch写入
	err = server.storage.Write(req.Context, txn.Writes())
	return resp, err
}

func (server *Server) KvScan(_ context.Context, req *kvrpcpb.ScanRequest) (*kvrpcpb.ScanResponse, error) {
	// Your Code Here (4C).
	// req.Version -> 开始时间戳
	// req.StartKey ->从这个key开始scan
	// req.Limit -> 一直scan这么多key
	reader, err := server.storage.Reader(req.Context)
	defer reader.Close()

	resp := &kvrpcpb.ScanResponse{}

	if err != nil {
		return nil, err
	}

	txn := mvcc.NewMvccTxn(reader, req.Version)
	scanner := mvcc.NewScanner(req.StartKey, txn)
	defer scanner.Close()
	for i := uint32(0); i < req.Limit; i++ {
		k, v, err := scanner.Next()
		if k == nil && v == nil && err == nil {
			break
		}
		resp.Pairs = append(resp.Pairs, &kvrpcpb.KvPair{
			Key:   k,
			Value: v,
		})
	}
	return resp, nil
}

func (server *Server) KvCheckTxnStatus(_ context.Context, req *kvrpcpb.CheckTxnStatusRequest) (*kvrpcpb.CheckTxnStatusResponse, error) {
	// Your Code Here (4C).
	server.Latches.WaitForLatches([][]byte{req.PrimaryKey})
	defer server.Latches.ReleaseLatches([][]byte{req.PrimaryKey})

	r, err := server.storage.Reader(req.Context)
	defer r.Close()

	rsp := &kvrpcpb.CheckTxnStatusResponse{}

	if err != nil {
		return nil, err
	}

	txn := mvcc.NewMvccTxn(r, req.LockTs)
	// 先获取primary key对应的锁
	l, err := txn.GetLock(req.PrimaryKey)
	if err != nil {
		return nil, err
	}
	// 如果主锁不存在(已经被回滚了，已经被提交了，压根没有)
	if l == nil {
		// 查找写记录
		w, ts, err := txn.CurrentWrite(req.PrimaryKey)
		if err != nil {
			return nil, err
		}
		// 如果写记录存在，说明已经提交了或者回滚了
		if w != nil {
			// The lock has been released by a commit
			// 如果是提交了
			if w.Kind != mvcc.WriteKindRollback {
				// 设置一下提交时间
				rsp.CommitVersion = ts
			}
			// 如果是回滚了什么也不用管
			return rsp, err
			// 如果写记录不存在, 那就是压根没有
		} else {
			rsp.Action = kvrpcpb.Action_LockNotExistRollback
			// 尝试删除一下value，一般是value也没有
			txn.DeleteValue(req.PrimaryKey)
			// 多一个回滚记录
			txn.PutWrite(req.PrimaryKey, req.LockTs, &mvcc.Write{
				StartTS: txn.StartTS,
				Kind:    mvcc.WriteKindRollback,
			})
		}
		// 如果锁存在并且超时了
	} else if mvcc.PhysicalTime(l.Ts)+l.Ttl <= mvcc.PhysicalTime(req.CurrentTs) {
		// The lock is expired
		rsp.Action = kvrpcpb.Action_TTLExpireRollback

		txn.DeleteLock(req.PrimaryKey)
		txn.DeleteValue(req.PrimaryKey)
		txn.PutWrite(req.PrimaryKey, req.LockTs, &mvcc.Write{
			StartTS: txn.StartTS,
			Kind:    mvcc.WriteKindRollback,
		})
	}
	// 锁存在但没超时，什么也不用设置
	err = server.storage.Write(req.Context, txn.Writes())

	return rsp, err
}

func (server *Server) KvBatchRollback(_ context.Context, req *kvrpcpb.BatchRollbackRequest) (*kvrpcpb.BatchRollbackResponse, error) {
	// Your Code Here (4C).
	// 大体思路: 删除数据，删除锁，并且写记录里放一个rollback记录
	server.Latches.WaitForLatches(req.Keys)
	defer server.Latches.ReleaseLatches(req.Keys)

	r, err := server.storage.Reader(req.Context)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	// 去重
	keys := removeDuplicateElement(req.Keys)

	rsp := &kvrpcpb.BatchRollbackResponse{}

	txn := mvcc.NewMvccTxn(r, req.StartVersion)
	for _, key := range keys {
		// 检查key是不是已经被提交了
		write, _, err := txn.CurrentWrite(key)
		if err != nil {
			return nil, err
		}
		// 如果write不是nil
		if write != nil {
			// 如果write not rollback类型的
			if write.Kind != mvcc.WriteKindRollback {
				// 静默失败就行
				rsp.Error = &kvrpcpb.KeyError{}
				rsp.Error.Abort = "Abort"
				continue
				// 如果write已经被提交了, 加一个Abort
			} else {
				continue
			}
		}
		// key还没被提交的逻辑

		// 先获取锁
		l, err := txn.GetLock(key)
		if err != nil {
			return nil, err
		}
		// 只有当锁存在并且锁的ts等于事务的start_ts时才去删除锁
		if l != nil && l.Ts == txn.StartTS {
			txn.DeleteLock(key)
		}
		// 尝试删除value, 只会删除ts对应的value
		txn.DeleteValue(key)
		// 写入一个回滚记录
		txn.PutWrite(key, txn.StartTS, &mvcc.Write{
			StartTS: txn.StartTS,
			Kind:    mvcc.WriteKindRollback,
		})
	}

	err = server.storage.Write(req.Context, txn.Writes())
	return rsp, nil
}

func (server *Server) KvResolveLock(_ context.Context, req *kvrpcpb.ResolveLockRequest) (*kvrpcpb.ResolveLockResponse, error) {
	r, err := server.storage.Reader(req.Context)
	defer r.Close()

	rsp := &kvrpcpb.ResolveLockResponse{}

	if err != nil {
		return nil, err
	}

	// 获取给定start_ts的所有锁
	it := r.IterCF(engine_util.CfLock)
	defer it.Close()

	allKeys := [][]byte{}
	for ; it.Valid(); it.Next() {
		b, err := it.Item().Value()
		if err != nil {
			continue
		}

		l, err := mvcc.ParseLock(b)
		if err != nil {
			continue
		}

		if l != nil && l.Ts == req.StartVersion {
			allKeys = append(allKeys, it.Item().Key())
		}
	}
	if len(allKeys) == 0 {
		return rsp, nil
	}

	// 如果commitVersion == 0, rollback
	if req.CommitVersion == 0 {
		nrsp, err := server.KvBatchRollback(nil, &kvrpcpb.BatchRollbackRequest{
			Context:      req.Context,
			StartVersion: req.StartVersion,
			Keys:         allKeys,
		})
		if err != nil {
			return nil, err
		}
		rsp.Error = nrsp.Error
		rsp.RegionError = nrsp.RegionError
		// 否则commit
	} else {
		nrsp, err := server.KvCommit(nil, &kvrpcpb.CommitRequest{
			Context:       req.Context,
			StartVersion:  req.StartVersion,
			Keys:          allKeys,
			CommitVersion: req.CommitVersion,
		})
		if err != nil {
			return nil, err
		}
		rsp.Error = nrsp.Error
		rsp.RegionError = nrsp.RegionError
	}
	return rsp, nil
}

// SQL push down commands.
func (server *Server) Coprocessor(_ context.Context, req *coppb.Request) (*coppb.Response, error) {
	resp := new(coppb.Response)
	reader, err := server.storage.Reader(req.Context)
	if err != nil {
		if regionErr, ok := err.(*raft_storage.RegionError); ok {
			resp.RegionError = regionErr.RequestErr
			return resp, nil
		}
		return nil, err
	}
	switch req.Tp {
	case kv.ReqTypeDAG:
		return server.copHandler.HandleCopDAGRequest(reader, req), nil
	case kv.ReqTypeAnalyze:
		return server.copHandler.HandleCopAnalyzeRequest(reader, req), nil
	}
	return nil, nil
}

func removeDuplicateElement(keys [][]byte) [][]byte {
	result := make([][]byte, 0, len(keys))
	temp := map[string]struct{}{}
	for _, item := range keys {
		if _, ok := temp[string(item)]; !ok {
			temp[string(item)] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}
