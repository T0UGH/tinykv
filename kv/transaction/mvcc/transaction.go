package mvcc

import (
	"bytes"
	"encoding/binary"
	"github.com/pingcap-incubator/tinykv/kv/util/engine_util"
	"math"

	"github.com/pingcap-incubator/tinykv/kv/storage"
	"github.com/pingcap-incubator/tinykv/kv/util/codec"
	"github.com/pingcap-incubator/tinykv/proto/pkg/kvrpcpb"
	"github.com/pingcap-incubator/tinykv/scheduler/pkg/tsoutil"
)

// KeyError is a wrapper type so we can implement the `error` interface.
// KeyError是一个包装类型，它实现了error借口

type KeyError struct {
	kvrpcpb.KeyError
}

func (ke *KeyError) Error() string {
	return ke.String()
}

// MvccTxn groups together writes as part of a single transaction. It also provides an abstraction over low-level
// storage, lowering the concepts of timestamps, writes, and locks into plain keys and values.
// MvccTxn将写入分组作为单个事务的一部分。它还提供了对低级存储的抽象，将时间戳、写入和锁定的概念降低到普通键和值中。
type MvccTxn struct {
	StartTS uint64
	Reader  storage.StorageReader
	writes  []storage.Modify
}

func NewMvccTxn(reader storage.StorageReader, startTs uint64) *MvccTxn {
	return &MvccTxn{
		Reader:  reader,
		StartTS: startTs,
	}
}

// Writes returns all changes added to this transaction.
func (txn *MvccTxn) Writes() []storage.Modify {
	return txn.writes
}

// PutWrite records a write at key and ts.
// PutWrite使用key和ts来记录写操作。
func (txn *MvccTxn) PutWrite(key []byte, ts uint64, write *Write) {
	// Your Code Here (4A).
	data := storage.Put{Cf: engine_util.CfWrite, Key: EncodeKey(key, ts), Value: write.ToBytes()}
	txn.writes = append(txn.writes, storage.Modify{Data: data})
}

// GetLock returns a lock if key is locked. It will return (nil, nil) if there is no lock on key, and (nil, err)
// if an error occurs during lookup.
// 如果Key上面有个锁, GetLock会返回一个lock。
// 如果key上没有lock，它将返回(nil, nil)
// 如果发生了错误，它将返回(nil, err)
func (txn *MvccTxn) GetLock(key []byte) (*Lock, error) {
	// Your Code Here (4A).
	data, err := txn.Reader.GetCF(engine_util.CfLock, key)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	lock, err := ParseLock(data)
	if err != nil {
		return nil, err
	}
	return lock, nil
}

// PutLock adds a key/lock to this transaction.
// PutLock向该事务添加一个key/lock。
func (txn *MvccTxn) PutLock(key []byte, lock *Lock) {
	// Your Code Here (4A).
	data := storage.Put{Cf: engine_util.CfLock, Key: key, Value: lock.ToBytes()}
	txn.writes = append(txn.writes, storage.Modify{Data: data})
}

// DeleteLock向该事务添加一个删除锁。
// DeleteLock adds a delete lock to this transaction.
func (txn *MvccTxn) DeleteLock(key []byte) {
	// Your Code Here (4A).
	data := storage.Delete{Cf: engine_util.CfLock, Key: key}
	txn.writes = append(txn.writes, storage.Modify{Data: data})
}

// GetValue finds the value for key, valid at the start timestamp of this transaction.
// I.e., the most recent value committed before the start of this transaction.
// GetValue查找key的值，该值在该事务的开始时间戳时有效。
// 这个值具体来说是：在事务开始之前提交的最新值
func (txn *MvccTxn) GetValue(key []byte) ([]byte, error) {
	// Your Code Here (4A).
	// todo
	// 先从cfWrite里面找
	it := txn.Reader.IterCF(engine_util.CfWrite)
	defer it.Close()
	for it.Seek(EncodeKey(key, txn.StartTS)); it.Valid(); it.Next() {
		writeBytes, err := it.Item().Value()
		if err != nil {
			continue
		}
		write, err := ParseWrite(writeBytes)
		if err != nil {
			continue
		}
		// 如果是WriteKindRollback就继续往前找
		switch write.Kind {
		case WriteKindPut:
			return txn.Reader.GetCF(engine_util.CfDefault, EncodeKey(key, write.StartTS))
		case WriteKindDelete:
			return nil, nil
		}
	}
	return nil, nil
}

// PutValue adds a key/value write to this transaction.
// PutValue向该事务添加一个键/值写入。
func (txn *MvccTxn) PutValue(key []byte, value []byte) {
	// Your Code Here (4A).
	data := storage.Put{Cf: engine_util.CfDefault, Key: EncodeKey(key, txn.StartTS), Value: value}
	txn.writes = append(txn.writes, storage.Modify{Data: data})
}

// DeleteValue removes a key/value pair in this transaction.
// DeleteValue删除事务中的键/值对
func (txn *MvccTxn) DeleteValue(key []byte) {
	// Your Code Here (4A).
	data := storage.Delete{Cf: engine_util.CfDefault, Key: EncodeKey(key, txn.StartTS)}
	txn.writes = append(txn.writes, storage.Modify{Data: data})
}

// CurrentWrite searches for a write with this transaction's start timestamp. It returns a Write from the DB and that
// write's commit timestamp, or an error.
// CurrentWrite搜索具有该事务开始时间戳的写操作。它从DB返回一个Write的提交时间戳，或者一个错误。
func (txn *MvccTxn) CurrentWrite(key []byte) (*Write, uint64, error) {
	// Your Code Here (4A).
	writeIter := txn.Reader.IterCF(engine_util.CfWrite)
	defer writeIter.Close()
	for writeIter.Seek(EncodeKey(key, math.MaxUint64)); writeIter.Valid(); writeIter.Next() {
		curr := writeIter.Item()
		writeKeyAndTs := curr.Key()
		writeKey := DecodeUserKey(writeKeyAndTs)
		if !bytes.Equal(writeKey, key) {
			return nil, 0, nil
		}
		// 把write解析出来拿到startTs
		data, err := curr.Value()
		if err != nil {
			return nil, 0, err
		}
		write, err := ParseWrite(data)
		if err != nil {
			return nil, 0, err
		}
		if write.StartTS == txn.StartTS {
			commitTs := decodeTimestamp(curr.Key())
			return write, commitTs, nil
		}
	}
	return nil, 0, nil
}

// MostRecentWrite finds the most recent write with the given key. It returns a Write from the DB and that
// write's commit timestamp, or an error.
//使用给定的键查找最近的写操作。它从DB返回一个Write，并且该Write的提交时间戳，或者一个错误。
func (txn *MvccTxn) MostRecentWrite(key []byte) (*Write, uint64, error) {
	// Your Code Here (4A).
	return txn.searchWrite(key, math.MaxUint64)
}

// EncodeKey encodes a user key and appends an encoded timestamp to a key. Keys and timestamps are encoded so that
// timestamped keys are sorted first by key (ascending), then by timestamp (descending). The encoding is based on
// https://github.com/facebook/mysql-5.6/wiki/MyRocks-record-format#memcomparable-format.
func EncodeKey(key []byte, ts uint64) []byte {
	encodedKey := codec.EncodeBytes(key)
	newKey := append(encodedKey, make([]byte, 8)...)
	binary.BigEndian.PutUint64(newKey[len(encodedKey):], ^ts)
	return newKey
}

// DecodeUserKey takes a key + timestamp and returns the key part.
func DecodeUserKey(key []byte) []byte {
	_, userKey, err := codec.DecodeBytes(key)
	if err != nil {
		panic(err)
	}
	return userKey
}

// decodeTimestamp takes a key + timestamp and returns the timestamp part.
func decodeTimestamp(key []byte) uint64 {
	left, _, err := codec.DecodeBytes(key)
	if err != nil {
		panic(err)
	}
	return ^binary.BigEndian.Uint64(left)
}

// PhysicalTime returns the physical time part of the timestamp.
// PhysicalTime返回时间戳的物理时间部分。
func PhysicalTime(ts uint64) uint64 {
	return ts >> tsoutil.PhysicalShiftBits
}

func (txn *MvccTxn) searchWrite(key []byte, ts uint64) (*Write, uint64, error) {
	// Your Code Here (4A).
	writeIter := txn.Reader.IterCF(engine_util.CfWrite)
	defer writeIter.Close()
	writeIter.Seek(EncodeKey(key, ts))
	if !writeIter.Valid() {
		return nil, 0, nil
	}
	item := writeIter.Item()
	// 从数据库拿出来的userKey要和查找的userKey相等才行
	writeKeyAndTs := item.Key()
	writeKey := DecodeUserKey(writeKeyAndTs)
	if !bytes.Equal(writeKey, key) {
		return nil, 0, nil
	}
	// 把write解析出来拿到startTs
	data, err := item.Value()
	if err != nil {
		return nil, 0, err
	}
	write, err := ParseWrite(data)
	if err != nil {
		return nil, 0, err
	}
	// 解析key拿到commitTs
	commitTs := decodeTimestamp(item.Key())
	return write, commitTs, nil
}
