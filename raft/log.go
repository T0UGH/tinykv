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
	// 接收到的未持久化snapshot
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
	entries[0].Index = firstIndex - 1
	if lastIndex >= firstIndex {
		storageEntries, _ := storage.Entries(firstIndex, lastIndex+1)
		entries = append(entries, storageEntries...)
	}
	return &RaftLog{
		storage:   storage,
		entries:   entries,
		applied:   storage.AppliedIndex(),
		committed: hardState.Commit,
		stabled:   lastIndex,
	}
}

// 我们需要在某些时间压缩日志条目，例如存储紧凑的稳定日志条目，以防止日志条目在内存中无限增长
// We need to compact the log entries in some point of time like
// storage compact stabled log entries prevent the log entries
// grow unlimitedly in memory
// 压缩内存中的日志
func (l *RaftLog) maybeCompact() {
	// Your Code Here (2C).
}

// 返回所有还没持久化的日志
// unstableEntries return all the unstable entries
func (l *RaftLog) unstableEntries() []pb.Entry {
	// Your Code Here (2A).
	offset := l.entries[0].Index
	unstable := l.stabled + 1
	if unstable > l.LastIndex() {
		return make([]pb.Entry, 0)
	}
	return l.entries[unstable-offset:]
}

// 返回所有提交了但是还没applied的entries
// nextEnts returns all the committed but not applied entries
func (l *RaftLog) nextEnts() []pb.Entry {
	// Your Code Here (2A).
	offset := l.entries[0].Index
	unApplied := l.applied + 1
	if unApplied > l.LastIndex() {
		return make([]pb.Entry, 0)
	}
	return l.entries[unApplied-offset : l.committed+1-offset]
}

// 返回i之后的所有条目
func (l *RaftLog) getEntries(lo, hi uint64) ([]pb.Entry, error) {
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

	// return if there is no new entry.  如果没有新的日志就直接返回
	if last < first {
		return nil
	}
	// truncate compacted entries 截断已经被压缩的日志
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
	// 如果小于，说明中间有空，这时候就不添加日志了, 但这里直接panic可能不太好
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

// 压缩内存中的日志
// 如果不压缩，内存中的日志将无限增长
func (l *RaftLog) Compact(compactIndex, compactTerm uint64) {

	var remain []pb.Entry
	if compactIndex < l.LastIndex() {
		// todo 这里有个异常没处理
		remain, _ = l.getEntries(compactIndex+1, l.LastIndex()+1)
	}
	l.entries = make([]pb.Entry, 1)
	l.entries[0].Index = compactIndex
	l.entries[0].Term = compactTerm
	if remain != nil {
		l.entries = append(l.entries, remain...)
	}
}

func (l *RaftLog) ResetForSnapshot(snapshot *pb.Snapshot) {
	l.entries = make([]pb.Entry, 1)
	l.pendingSnapshot = snapshot
	l.entries[0].Index = snapshot.Metadata.Index
	l.applied = snapshot.Metadata.Index
	l.stabled = snapshot.Metadata.Index
	l.entries[0].Term = snapshot.Metadata.Term
	l.UpdateCommit(snapshot.Metadata.Index)

}
