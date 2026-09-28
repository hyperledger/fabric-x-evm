/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package query

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/hyperledger/fabric-x-sdk/blocks"

	"github.com/hyperledger/fabric-x-evm/endorser/execution"
)

// KVS is the endorser's store when state lives in the query service. It keeps
// no state of its own: every read goes to the query service.
//
// Reads of different keys can see different block heights. That is safe: each
// read's version goes into the read set, and the committer rejects the
// transaction if any of them has changed. A key read as absent is recorded with
// no version, which the committer rejects if the key exists by then.
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

// NewSnapshot returns a reader for one transaction. blockNumber is ignored:
// the query service only serves the latest committed state.
func (k *KVS) NewSnapshot(_ *uint64) (execution.ReadStore, error) {
	return &snapshot{kvs: k, cache: map[string]*blocks.WriteRecord{}}, nil
}

// Get reads the current committed record, or nil if there is none.
func (k *KVS) Get(namespace, key string) (*blocks.WriteRecord, error) {
	rows, err := k.client.GetRows(context.Background(), namespace, [][]byte{[]byte(key)})
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

// snapshot caches reads, so a transaction that reads a key twice asks once and
// sees the same value both times.
type snapshot struct {
	kvs   *KVS
	mu    sync.Mutex
	cache map[string]*blocks.WriteRecord // nil value: known absent
}

func (s *snapshot) Get(namespace, key string) (*blocks.WriteRecord, error) {
	ck := namespace + "\x00" + key

	s.mu.Lock()
	rec, ok := s.cache[ck]
	s.mu.Unlock()
	if ok {
		return rec, nil
	}

	rec, err := s.kvs.Get(namespace, key)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.cache[ck]; ok {
		return prev, nil // a concurrent read got here first; keep its value
	}
	s.cache[ck] = rec
	return rec, nil
}

func (s *snapshot) Close() error { return nil }
