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

import pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"

// RaftLog用来管理Log entries
// RaftLog manage the log entries, its struct look like:
//
//  snapshot/first.....applied....committed....stabled.....last
//  --------|------------------------------------------------|
//                            log entries
//
// for simplify the RaftLog implement should manage all log entries
// that not truncated
// 为简化起见，RaftLog实现应管理所有未截断的日志条目
type RaftLog struct {
	// storage contains all stable entries since the last snapshot.
	// storage中保存了自从上一次做快照的所有持久化entries
	storage Storage

	// 论文中的已提交
	// committed is the highest log position that is known to be in
	// stable storage on a quorum of nodes.
	committed uint64

	// 论文中的applied
	// applied is the highest log position that the application has
	// been instructed to apply to its state machine.
	// Invariant: applied <= committed
	applied uint64

	// 已持久化
	// log entries with index <= stabled are persisted to storage.
	// It is used to record the logs that are not persisted by storage yet.
	// 每次处理Ready的时候，unstabled logs就会被提交掉
	// Everytime handling `Ready`, the unstabled logs will be included.
	stabled uint64

	// 所有还在内存中的日志
	// all entries that have not yet compact.
	entries []pb.Entry

	// the incoming unstable snapshot, if any.
	// (Used in 2C)
	pendingSnapshot *pb.Snapshot

	// Your Data Here (2A).
}

// 初始化方法，会使用storage来恢复这些
// newLog returns log using the given storage. It recovers the log
// to the state that it just commits and applies the latest snapshot.
func newLog(storage Storage) *RaftLog {
	// Your Code Here (2A).
	return &RaftLog{
		storage: storage,
		entries: make([]pb.Entry, 0),
	}
}

// 我们需要在某些时间压缩日志条目，例如存储紧凑的稳定日志条目，以防止日志条目在内存中无限增长
// We need to compact the log entries in some point of time like
// storage compact stabled log entries prevent the log entries
// grow unlimitedly in memory
func (l *RaftLog) maybeCompact() {
	// Your Code Here (2C).
}

// 返回所有还没持久化的日志
// unstableEntries return all the unstable entries
func (l *RaftLog) unstableEntries() []pb.Entry {
	// Your Code Here (2A).
	return nil
}

// 返回所有提交了但是还没applied的entries
// nextEnts returns all the committed but not applied entries
func (l *RaftLog) nextEnts() (ents []pb.Entry) {
	// Your Code Here (2A).
	return nil
}

// 返回i 之后的所有条目
func (l *RaftLog) from(i uint64) (ents []*pb.Entry) {
	// todo 待实现 // todo 需要考虑有一些term被存到disk的问题
	return nil
}

// 返回log entries的最后一个索引
// LastIndex return the last index of the log entries
func (l *RaftLog) LastIndex() uint64 {
	// todo 待实现
	// Your Code Here (2A).
	return 0
}

// 返回entry i的term
// Term return the term of the entry in the given index
func (l *RaftLog) Term(i uint64) (uint64, error) {
	// Your Code Here (2A).
	return 0, nil
}
