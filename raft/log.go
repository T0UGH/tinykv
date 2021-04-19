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

import (
	"github.com/pingcap-incubator/tinykv/log"
	pb "github.com/pingcap-incubator/tinykv/proto/pkg/eraftpb"
)

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

	// 论文中的applied 已提交的就可以找时间apply
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

	// 所有还没有被压缩的日志
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
	hardState, _, _ := storage.InitialState()
	lastIndex, _ := storage.LastIndex()
	entries := make([]pb.Entry, 1)
	firstIndex, _ := storage.FirstIndex()
	if lastIndex >= firstIndex {
		storageEntries, _ := storage.Entries(firstIndex, lastIndex+1)
		entries = append(entries, storageEntries...)
	}

	return &RaftLog{
		storage:   storage,
		entries:   entries,
		applied:   0, //todo:初始值问题
		committed: hardState.Commit,
		stabled:   lastIndex,
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
	unstable := l.stabled + 1
	if unstable > l.LastIndex() {
		return make([]pb.Entry, 0)
	}
	return l.entries[unstable:]
}

// 返回所有提交了但是还没applied的entries
// nextEnts returns all the committed but not applied entries
func (l *RaftLog) nextEnts() []pb.Entry {
	// Your Code Here (2A).
	unApplied := l.applied + 1
	if unApplied > l.LastIndex() {
		return make([]pb.Entry, 0)
	}
	return l.entries[unApplied : l.committed+1]
}

// 返回i之后的所有条目
func (l *RaftLog) Entries(lo, hi uint64) ([]pb.Entry, error) {
	offset := l.entries[0].Index
	if lo <= offset {
		return nil, ErrCompacted
	}
	if hi > l.LastIndex()+1 {
		log.Panicf("entries' hi(%d) is out of bound lastindex(%d)", hi, l.LastIndex())
	}

	ents := l.entries[lo-offset : hi-offset]
	if len(l.entries) == 1 && len(ents) != 0 {
		// only contains dummy entries.
		return nil, ErrUnavailable
	}
	return ents, nil
}

// 啥都没有的时候一般会返回0
// 返回log entries的最后一个索引
// LastIndex return the last index of the log entries
func (l *RaftLog) LastIndex() uint64 {
	// Your Code Here (2A).
	return l.entries[0].Index + uint64(len(l.entries)) - 1
}

func (l *RaftLog) FirstIndex() uint64 {
	return l.entries[0].Index + 1
}

// 返回entry i的term
// Term return the term of the entry in the given index
func (l *RaftLog) Term(i uint64) (uint64, error) {
	offset := l.entries[0].Index
	if i < offset {
		return 0, ErrCompacted
	}
	if int(i-offset) >= len(l.entries) {
		return 0, ErrUnavailable
	}
	return l.entries[i-offset].Term, nil
}

func (l *RaftLog) Append(entries []pb.Entry) error {
	if len(entries) == 0 {
		return nil
	}

	first := l.FirstIndex()
	last := entries[0].Index + uint64(len(entries)) - 1

	// shortcut if there is no new entry.
	if last < first {
		return nil
	}
	// truncate compacted entries
	if first > entries[0].Index {
		entries = entries[first-entries[0].Index:]
	}

	offset := entries[0].Index - l.entries[0].Index
	switch {
	case uint64(len(l.entries)) > offset:
		l.entries = append([]pb.Entry{}, l.entries[:offset]...)
		l.entries = append(l.entries, entries...)
	case uint64(len(l.entries)) == offset:
		l.entries = append(l.entries, entries...)
	default:
		log.Panicf("missing log entry [last: %d, append at: %d]",
			l.LastIndex(), entries[0].Index)
	}
	return nil
}

func (l *RaftLog) UpdateCommit(commit uint64) bool {
	if commit > l.committed {
		if commit > l.LastIndex() {
			l.committed = l.LastIndex()
		} else {
			l.committed = commit
		}
		return true
	}
	return false
}
