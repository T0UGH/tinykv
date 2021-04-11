package engine_util

import (
	"os"

	"github.com/Connor1996/badger"
	"github.com/pingcap-incubator/tinykv/log"
)

// Engines keeps references to and data for the engines used by unistore.
// All engines are badger key/value databases.
// the Path fields are the filesystem path to where the data is stored.
// 引擎会保留对unistore使用的引擎的引用和数据。
// 所有引擎都是badge键/值数据库。
// 两个Path字段是存储数据的文件系统路径。
type Engines struct {
	// Data, including data which is committed (i.e., committed across other nodes) and un-committed (i.e., only present
	// locally).
	// 数据，包括已提交（即，在其他节点上已提交）和未提交（即，仅在本地存在）的数据。
	Kv     *badger.DB
	KvPath string
	// Metadata used by Raft.
	// 供raft使用的元数据
	Raft     *badger.DB
	RaftPath string
}

// 初始化引擎
func NewEngines(kvEngine, raftEngine *badger.DB, kvPath, raftPath string) *Engines {
	return &Engines{
		Kv:       kvEngine,
		KvPath:   kvPath,
		Raft:     raftEngine,
		RaftPath: raftPath,
	}
}

// 直接调用WriteBatch的方法，将引擎中保存的kv写入到数据库
func (en *Engines) WriteKV(wb *WriteBatch) error {
	return wb.WriteToDB(en.Kv)
}

// 直接调用WriteBatch的方法，将引擎中保存的raft元数据写入到数据库
func (en *Engines) WriteRaft(wb *WriteBatch) error {
	return wb.WriteToDB(en.Raft)
}

// 关闭db
func (en *Engines) Close() error {
	if err := en.Kv.Close(); err != nil {
		return err
	}
	if err := en.Raft.Close(); err != nil {
		return err
	}
	return nil
}

// 直接全删
func (en *Engines) Destroy() error {
	if err := en.Close(); err != nil {
		return err
	}
	if err := os.RemoveAll(en.KvPath); err != nil {
		return err
	}
	if err := os.RemoveAll(en.RaftPath); err != nil {
		return err
	}
	return nil
}

// 用路径来创建一个新DB
// CreateDB creates a new Badger DB on disk at subPath.
func CreateDB(path string, raft bool) *badger.DB {
	opts := badger.DefaultOptions
	if raft {
		// 对于raft引擎不需要write blob 因为它会被快速删除
		// Do not need to write blob for raft engine because it will be deleted soon.
		opts.ValueThreshold = 0
	}
	opts.Dir = path
	opts.ValueDir = opts.Dir
	if err := os.MkdirAll(opts.Dir, os.ModePerm); err != nil {
		log.Fatal(err)
	}
	db, err := badger.Open(opts)
	if err != nil {
		log.Fatal(err)
	}
	return db
}
