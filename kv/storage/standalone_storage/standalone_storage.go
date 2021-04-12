package standalone_storage

import (
	"github.com/Connor1996/badger"
	"github.com/pingcap-incubator/tinykv/kv/config"
	"github.com/pingcap-incubator/tinykv/kv/storage"
	"github.com/pingcap-incubator/tinykv/kv/util/engine_util"
	"github.com/pingcap-incubator/tinykv/proto/pkg/kvrpcpb"
	"os"
	"path/filepath"
)

// StandAloneStorage is an implementation of `Storage` for a single-node TinyKV instance. It does not
// communicate with other nodes and all data is stored locally.
// StandAloneStorage是单节点TinyKV实例的`Storage`的实现。 它不与其他节点通信，所有数据都存储在本地。
type StandAloneStorage struct {
	// Your Data Here (1).
	engine *badger.DB
	config *config.Config
}

func NewStandAloneStorage(conf *config.Config) *StandAloneStorage {
	// Your Code Here (1).
	dbPath := conf.DBPath
	kvPath := filepath.Join(dbPath, "kv")
	os.MkdirAll(kvPath, os.ModePerm)
	kvDB := engine_util.CreateDB(kvPath, false)

	return &StandAloneStorage{engine: kvDB, config: conf}
}

func (s *StandAloneStorage) Start() error {
	// Your Code Here (1).
	return nil
}

func (s *StandAloneStorage) Stop() error {
	// Your Code Here (1).
	if err := s.engine.Close(); err != nil {
		return err
	}
	return nil
}

func (s *StandAloneStorage) Reader(ctx *kvrpcpb.Context) (storage.StorageReader, error) {
	// Your Code Here (1).
	return &StandAloneReader{s.engine.NewTransaction(false)}, nil
}

//todo: 如果产生了异常只返回第一个异常
func (s *StandAloneStorage) Write(ctx *kvrpcpb.Context, batch []storage.Modify) error {
	// Your Code Here (1).
	errs := make([]error, 0)
	for _, m := range batch {
		switch data := m.Data.(type) {
		case storage.Put:
			if err := engine_util.PutCF(s.engine, data.Cf, data.Key, data.Value); err != nil {
				errs = append(errs, err)
			}
		case storage.Delete:
			if err := engine_util.DeleteCF(s.engine, data.Cf, data.Key); err != nil {
				errs = append(errs, err)
			}
		}
	}

	if len(errs) > 0 {
		return errs[0]
	}

	return nil
}

// StandAloneReader is a StorageReader which reads from a StandAloneStorage.
type StandAloneReader struct {
	txn *badger.Txn
}

func (r *StandAloneReader) GetCF(cf string, key []byte) ([]byte, error) {
	return engine_util.GetCFFromTxn(r.txn, cf, key)
}

// 这个方法肯定是要返回一个崭新的迭代器
func (r *StandAloneReader) IterCF(cf string) engine_util.DBIterator {
	return engine_util.NewCFIterator(cf, r.txn)
}

// 关闭reader的时候要关闭事务，但是不用关闭iter，因为已经把iter传出去了
func (r *StandAloneReader) Close() {
	r.txn.Discard()
}
