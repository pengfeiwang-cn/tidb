// Copyright 2021 PingCAP, Inc.
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

package copr

import (
	"context"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/pingcap/errors"
	"github.com/pingcap/tidb/kv"
	txndriver "github.com/pingcap/tidb/store/driver/txn"
	"github.com/pingcap/tidb/store/tikv"
	"github.com/pingcap/tidb/store/tikv/config"
	"github.com/pingcap/tidb/store/tikv/tikvrpc"
)

type kvStore struct {
	store *tikv.KVStore
}

// GetRegionCache returns the region cache instance.
func (s *kvStore) GetRegionCache() *tikv.RegionCache {
	return s.store.GetRegionCache()
}

// CheckVisibility checks if it is safe to read using given ts.
func (s *kvStore) CheckVisibility(startTime uint64) error {
	err := s.store.CheckVisibility(startTime)
	return txndriver.ToTiDBErr(err)
}

// GetTiKVClient gets the client instance.
func (s *kvStore) GetTiKVClient() tikv.Client {
	client := s.store.GetTiKVClient()
	return &tikvClient{c: client}
}

type tikvClient struct {
	c tikv.Client
}

func (c *tikvClient) Close() error {
	err := c.c.Close()
	return txndriver.ToTiDBErr(err)
}

// SendRequest sends Request.
func (c *tikvClient) SendRequest(ctx context.Context, addr string, req *tikvrpc.Request, timeout time.Duration) (*tikvrpc.Response, error) {
	res, err := c.c.SendRequest(ctx, addr, req, timeout)
	return res, txndriver.ToTiDBErr(err)
}

// Store wraps tikv.KVStore and provides coprocessor utilities.
type Store struct {
	*kvStore
	coprCache       *coprCache
	replicaReadSeed uint32
}

// NewStore creates a new store instance.
func NewStore(s *tikv.KVStore, coprCacheConfig *config.CoprocessorCache) (*Store, error) {
	coprCache, err := newCoprCache(coprCacheConfig)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return &Store{
		kvStore:         &kvStore{store: s},
		coprCache:       coprCache,
		replicaReadSeed: rand.Uint32(),
	}, nil
}

// Close releases resources allocated for coprocessor.
func (s *Store) Close() {
	if s.coprCache != nil {
		s.coprCache.cache.Close()
	}
}

func (s *Store) nextReplicaReadSeed() uint32 {
	return atomic.AddUint32(&s.replicaReadSeed, 1)
}

// GetClient gets a client instance.
func (s *Store) GetClient() kv.Client {
	return &CopClient{
		store:           s,
		replicaReadSeed: s.nextReplicaReadSeed(),
	}
}

// GetMPPClient gets a mpp client instance.
func (s *Store) GetMPPClient() kv.MPPClient {
	return &MPPClient{
		store: s.kvStore,
	}
}

func getEndPointType(t kv.StoreType) tikvrpc.EndpointType {
	switch t {
	case kv.TiKV:
		return tikvrpc.TiKV
	case kv.TiFlash:
		return tikvrpc.TiFlash
	case kv.TiDB:
		return tikvrpc.TiDB
	default:
		return tikvrpc.TiKV
	}
}
