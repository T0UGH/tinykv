// Copyright 2017 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package schedulers

import (
	"github.com/pingcap-incubator/tinykv/log"
	"github.com/pingcap-incubator/tinykv/scheduler/server/core"
	"github.com/pingcap-incubator/tinykv/scheduler/server/schedule"
	"github.com/pingcap-incubator/tinykv/scheduler/server/schedule/operator"
	"github.com/pingcap-incubator/tinykv/scheduler/server/schedule/opt"
	"sort"
)

func init() {
	schedule.RegisterSliceDecoderBuilder("balance-region", func(args []string) schedule.ConfigDecoder {
		return func(v interface{}) error {
			return nil
		}
	})
	schedule.RegisterScheduler("balance-region", func(opController *schedule.OperatorController, storage *core.Storage, decoder schedule.ConfigDecoder) (schedule.Scheduler, error) {
		return newBalanceRegionScheduler(opController), nil
	})
}

const (
	// balanceRegionRetryLimit is the limit to retry schedule for selected store.
	balanceRegionRetryLimit = 10
	balanceRegionName       = "balance-region-scheduler"
)

type balanceRegionScheduler struct {
	*baseScheduler
	name         string
	opController *schedule.OperatorController
}

// newBalanceRegionScheduler creates a scheduler that tends to keep regions on
// each store balanced.
func newBalanceRegionScheduler(opController *schedule.OperatorController, opts ...BalanceRegionCreateOption) schedule.Scheduler {
	base := newBaseScheduler(opController)
	s := &balanceRegionScheduler{
		baseScheduler: base,
		opController:  opController,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// BalanceRegionCreateOption is used to create a scheduler with an option.
type BalanceRegionCreateOption func(s *balanceRegionScheduler)

func (s *balanceRegionScheduler) GetName() string {
	if s.name != "" {
		return s.name
	}
	return balanceRegionName
}

func (s *balanceRegionScheduler) GetType() string {
	return "balance-region"
}

func (s *balanceRegionScheduler) IsScheduleAllowed(cluster opt.Cluster) bool {
	return s.opController.OperatorCount(operator.OpRegion) < cluster.GetRegionScheduleLimit()
}

func (s *balanceRegionScheduler) Schedule(cluster opt.Cluster) *operator.Operator {
	// Your Code Here (3C).
	// 用operator.CreateMovePeerOperator()来创建对应的operator
	stores := make([]*core.StoreInfo, 0)
	for _, store := range cluster.GetStores() {
		if store.IsUp() && store.DownTime() < cluster.GetMaxStoreDownTime() {
			stores = append(stores, store)
		}
	}
	// sort by region size
	sort.Slice(stores, func(i, j int) bool {
		return stores[i].GetRegionSize() < stores[j].GetRegionSize()
	})
	// 寻找一个合适的region
	// 先pending region, 再follower region, 如果还不行就leader region
	var region *core.RegionInfo
	var originStore *core.StoreInfo
	for _, store := range stores {
		storeId := store.GetID()
		cbFunc := func(rc core.RegionsContainer) {
			// todo 用cluster.RandPendingRegion()可以筛选不同的region
			region = rc.RandomRegion(nil, nil)
			originStore = store
		}
		cluster.GetPendingRegionsWithLock(storeId, cbFunc)
		if region != nil {
			break
		}
		cluster.GetFollowersWithLock(storeId, cbFunc)
		if region != nil {
			break
		}
		cluster.GetLeadersWithLock(storeId, cbFunc)
		if region != nil {
			break
		}
	}

	if region == nil {
		return nil
	}

	// 选择一个合适的target store
	usedSIds := region.GetStoreIds()
	var targetStore *core.StoreInfo
	for i := len(stores) - 1; i >= 0; i-- {
		currId := stores[i].GetID()
		if _, ok := usedSIds[currId]; !ok {
			targetStore = stores[i]
		}
	}

	if targetStore == nil {
		return nil
	}

	if originStore.GetRegionSize()-targetStore.GetRegionSize() < 2*region.GetApproximateSize() {
		return nil
	}

	// 先在cluster上新建一个peer的meta
	peer, err := cluster.AllocPeer(targetStore.GetID())
	if err != nil {
		log.Error(err)
		return nil
	}

	// 创建对应的operator
	op, err := operator.CreateMovePeerOperator("balance-region", cluster, region,
		operator.OpBalance, originStore.GetID(), targetStore.GetID(), peer.Id)
	if err != nil {
		log.Error(err)
		return nil
	}
	return op
}
