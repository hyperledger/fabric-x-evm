/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package query

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/hyperledger/fabric-x-sdk/blocks"

	"github.com/hyperledger/fabric-x-evm/endorser/execution"
)

// errHistorical is returned when a snapshot at a specific block is requested.
var errHistorical = errors.New("query-service store only serves the latest state, not a specific block")

// KVS is the endorser's store when state lives in the query service. It keeps
// no state of its own: every read goes to the query service.
//
// Each snapshot is a query-service view, so a transaction sees one consistent
// state, as with the other stores.
//
// It still receives blocks, but only to track the height the synchronizer
// resumes from.
type KVS struct {
	client    Client
	namespace string
	height    atomic.Uint64
}

var (
	_ execution.KVSSnapshotter = (*KVS)(nil)
	_ blocks.BlockHandler      = (*KVS)(nil)
	_ blocks.RecordGetter      = (*KVS)(nil)
)

// NewKVS returns a KVS reading namespace ns through client.
func NewKVS(client Client, ns string) *KVS {
	return &KVS{client: client, namespace: ns}
}

// NewSnapshot opens a view of the latest state. blockNumber must be nil: the
// query service has no history.
func (k *KVS) NewSnapshot(blockNumber *uint64) (execution.ReadStore, error) {
	if blockNumber != nil {
		return nil, errHistorical
	}
	viewID, err := k.client.BeginView(context.Background())
	if err != nil {
		return nil, err
	}
	return &snapshot{kvs: k, viewID: viewID, cache: map[string]*blocks.WriteRecord{}}, nil
}

// Get reads the latest committed record, or nil if there is none.
func (k *KVS) Get(namespace, key string) (*blocks.WriteRecord, error) {
	return k.get("", namespace, key)
}

func (k *KVS) get(viewID, namespace, key string) (*blocks.WriteRecord, error) {
	rows, err := k.client.GetRows(context.Background(), viewID, namespace, [][]byte{[]byte(key)})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if string(r.Key) == key {
			return &blocks.WriteRecord{Namespace: namespace, Key: key, Version: r.Version, Value: r.Value}, nil
		}
	}
	return nil, nil
}

// Handle records the block's height and nothing else: the committer has
// already applied its writes. Blocks arrive one at a time.
func (k *KVS) Handle(_ context.Context, b blocks.Block) error {
	if b.Number > k.height.Load() {
		k.height.Store(b.Number)
	}
	return nil
}

// SetBlockNumber sets the height to resume from. Call before the synchronizer
// starts.
func (k *KVS) SetBlockNumber(n uint64) { k.height.Store(n) }

// BlockNumber returns the last block seen.
func (k *KVS) BlockNumber(context.Context) (uint64, error) { return k.height.Load(), nil }

// Close closes the client.
func (k *KVS) Close() error { return k.client.Close() }

// snapshot reads through one view, caching each key so it is fetched once.
type snapshot struct {
	kvs    *KVS
	viewID string
	mu     sync.Mutex
	cache  map[string]*blocks.WriteRecord // nil value: known absent
}

func (s *snapshot) Get(namespace, key string) (*blocks.WriteRecord, error) {
	ck := namespace + "\x00" + key

	s.mu.Lock()
	rec, ok := s.cache[ck]
	s.mu.Unlock()
	if ok {
		return rec, nil
	}

	rec, err := s.kvs.get(s.viewID, namespace, key)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.cache[ck] = rec
	s.mu.Unlock()
	return rec, nil
}

// Close ends the view.
func (s *snapshot) Close() error {
	return s.kvs.client.EndView(context.Background(), s.viewID)
}
