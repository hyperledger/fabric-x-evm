/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

// Package endorser implements a lightweight versioned key-value store (LightKVS)
// with snapshot isolation for concurrent readers and a single writer.
//
// LightKVS uses structural sharing with atomic pointer swaps to provide:
// - Lock-free reads with snapshot isolation
// - Atomic batch updates
// - Automatic garbage collection via Go's GC
// - Space-efficient storage (only changed values are copied)
//
// Design:
// - Each snapshot is a map[string]*ValueVersion
// - Writer creates new snapshot by cloning the map and updating changed entries
// - Readers hold references to their snapshot, preventing GC
// - Go's GC automatically cleans up unreferenced snapshots and values
package endorser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync/atomic"

	"github.com/hyperledger/fabric-x-sdk/blocks"
)

var (
	// ErrKeyNotFound is returned when a key is not found in the store.
	ErrKeyNotFound = errors.New("key not found")
)

// LightKVS is a lightweight versioned key-value store with snapshot isolation.
// It supports concurrent readers and a single writer.
type LightKVS struct {
	// Atomic pointer to current snapshot
	// Readers get this atomically, writers swap it atomically
	current atomic.Pointer[Snapshot]
}

// Snapshot represents an immutable point-in-time view of the key-value store.
type Snapshot struct {
	// blockNumber is the block number of this snapshot
	blockNumber uint64

	// Map from key to pointer to immutable value
	// Multiple snapshots can share pointers to unchanged values
	data map[string]*ValueVersion
}

// ValueVersion represents a versioned value in the store.
type ValueVersion struct {
	// Value is the binary blob stored for this key
	Value []byte

	// BlockNum is the block number where this write occurred
	BlockNum uint64

	// TxNum is the transaction number within the block
	TxNum uint64

	// Version is the monotonically increasing version number for this key
	Version uint64

	// TxID is the transaction ID
	TxID string
}

// Reader provides a consistent view of the store at a specific point in time.
// All Get operations see the state as it was when Begin() was called.
// Reader implements the ReadStore interface for compatibility with StateDB.
type Reader struct {
	// snapshot holds a reference to the immutable snapshot
	// This prevents the snapshot from being garbage collected
	snapshot *Snapshot

	// kvs reference for BlockNumber queries
	kvs *LightKVS
}

// KeyValueVersion represents a key-value pair with version for batch updates.
type KeyValueVersion struct {
	Key      string
	Value    []byte // Can be nil for storing nil values
	BlockNum uint64
	TxNum    uint64
	TxID     string
	IsDelete bool // True to delete the key, false to store Value (even if nil)
}

// truncateValue truncates a byte slice to maxLen bytes for logging
func truncateValue(v []byte, maxLen int) string {
	if v == nil {
		return "<nil>"
	}
	if len(v) <= maxLen {
		return fmt.Sprintf("%x", v)
	}
	return fmt.Sprintf("%x...", v[:maxLen])
}

// logUpdate is a helper type for JSON logging with truncated values
type logUpdate struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	BlockNum uint64 `json:"block_num"`
	TxNum    uint64 `json:"tx_num"`
	TxID     string `json:"tx_id"`
	IsDelete bool   `json:"is_delete"`
}

func (u KeyValueVersion) toLogUpdate() logUpdate {
	return logUpdate{
		Key:      u.Key,
		Value:    truncateValue(u.Value, 20),
		BlockNum: u.BlockNum,
		TxNum:    u.TxNum,
		TxID:     u.TxID,
		IsDelete: u.IsDelete,
	}
}

// NewLightKVS creates a new empty versioned key-value store.
func NewLightKVS() *LightKVS {
	kvs := &LightKVS{}
	initial := &Snapshot{
		blockNumber: 0,
		data:        make(map[string]*ValueVersion),
	}
	kvs.current.Store(initial)
	return kvs
}

// NewSnapshot starts a new read transaction and returns a Reader.
// The Reader will see a consistent snapshot of the store at this point in time,
// regardless of subsequent writes.
//
// Readers must call Close() when done to allow garbage collection of old snapshots.
func (kvs *LightKVS) NewSnapshot() ReadStore {
	// Atomically load the current snapshot
	snapshot := kvs.current.Load()

	// Return a reader that holds a reference to this snapshot
	return &Reader{
		snapshot: snapshot,
		kvs:      kvs,
	}
}

func (kvs *LightKVS) Get(namespace, key string, lastBlock uint64) (*blocks.WriteRecord, error) {
	r := kvs.NewSnapshot()
	defer r.Close()

	return r.Get(namespace, key)
}

// Get retrieves the value and version for a key from the reader's snapshot.
// This implements the ReadStore interface with the signature:
// Get(namespace, key string) (*blocks.WriteRecord, error)
func (r *Reader) Get(namespace, key string) (*blocks.WriteRecord, error) {
	if r.snapshot == nil {
		return nil, errors.New("reader is closed")
	}

	// Prepend namespace to key
	fullKey := namespace + ":" + key

	if vv, ok := r.snapshot.data[fullKey]; ok {
		record := &blocks.WriteRecord{
			Namespace: namespace,
			Key:       key,
			BlockNum:  vv.BlockNum,
			TxNum:     vv.TxNum,
			Version:   vv.Version,
			Value:     vv.Value,
			IsDelete:  false,
			TxID:      vv.TxID,
		}

		if false {
			// Debug: JSON dump the record with truncated value
			logRec := map[string]interface{}{
				"namespace": namespace,
				"key":       key,
				"block_num": vv.BlockNum,
				"tx_num":    vv.TxNum,
				"version":   vv.Version,
				"value":     truncateValue(vv.Value, 20),
				"tx_id":     vv.TxID,
			}
			if jsonData, err := json.MarshalIndent(logRec, "", "  "); err == nil {
				fmt.Printf("[LightKVS Get] %s\n", string(jsonData))
			}
		}

		return record, nil
	}

	if false {
		// Key not found - return nil record (not an error)
		fmt.Printf("[LightKVS Get] key not found: %s\n", fullKey)
	}
	return nil, nil
}

// Close releases the reader's reference to its snapshot.
// After Close(), the reader cannot be used for further Get operations.
// This allows Go's GC to clean up the snapshot if no other readers reference it.
func (r *Reader) Close() error {
	r.snapshot = nil
	return nil
}

// Update atomically applies a batch of updates to the store.
// All updates are applied together in a single new snapshot.
//
// This operation:
// 1. Clones the current snapshot's map (shallow copy - shares unchanged value pointers)
// 2. Updates only the changed entries with new ValueVersion structs or deletes them
// 3. Atomically swaps in the new snapshot
//
// The single writer assumption means no locking is needed for the update itself.
func (kvs *LightKVS) Update(updates []KeyValueVersion) error {
	// Load current snapshot
	oldSnapshot := kvs.current.Load()

	// Shallow clone the map - copies map structure, shares value pointers
	// This is O(n) but highly optimized in Go's runtime
	newData := maps.Clone(oldSnapshot.data)

	// Update changed entries with new ValueVersion structs
	// Only these allocations are new; unchanged entries share pointers
	for _, update := range updates {
		if update.IsDelete {
			// Delete: remove the key from the map
			delete(newData, update.Key)
		} else {
			// Compute next version for this key: existing version + 1, or 0 if new
			nextVersion := uint64(0)
			if existing, ok := oldSnapshot.data[update.Key]; ok {
				nextVersion = existing.Version + 1
			}

			// Update: set new value (Value can be nil, which is a valid stored value)
			newData[update.Key] = &ValueVersion{
				Value:    update.Value,
				BlockNum: update.BlockNum,
				TxNum:    update.TxNum,
				Version:  nextVersion,
				TxID:     update.TxID,
			}

			if false {
				// Debug: JSON dump the record with truncated value
				logRec := map[string]interface{}{
					"key":       update.Key,
					"block_num": update.BlockNum,
					"tx_num":    update.TxNum,
					"version":   nextVersion,
					"value":     truncateValue(update.Value, 20),
					"tx_id":     update.TxID,
				}
				if jsonData, err := json.MarshalIndent(logRec, "", "  "); err == nil {
					fmt.Printf("[LightKVS Put] %s\n", string(jsonData))
				}
			}

		}
	}

	// Create new snapshot with the block number from updates
	// All updates in a batch come from the same block
	blockNum := uint64(0)
	if len(updates) > 0 {
		blockNum = updates[0].BlockNum
	}
	newSnapshot := &Snapshot{
		blockNumber: blockNum,
		data:        newData,
	}

	// Atomically swap in the new snapshot
	// New readers will see this snapshot; existing readers keep their old snapshot
	kvs.current.Store(newSnapshot)

	return nil
}

// Handle implements the blocks.BlockHandler interface.
// It processes a block by extracting all valid transaction writes and applying them atomically.
// This is called by the synchronizer when a new block is committed to the ledger.
func (kvs *LightKVS) Handle(ctx context.Context, b blocks.Block) error {
	// Collect all writes from all valid transactions in the block
	var updates []KeyValueVersion

	for _, tx := range b.Transactions {
		if !tx.Valid {
			continue
		}

		// Process all namespace read-write sets
		for _, nsrws := range tx.NsRWS {
			// Process all writes in this namespace
			for _, w := range nsrws.RWS.Writes {
				// Create a key that includes the namespace
				key := nsrws.Namespace + ":" + w.Key

				updates = append(updates, KeyValueVersion{
					Key:      key,
					Value:    w.Value,
					BlockNum: b.Number,
					TxNum:    uint64(tx.Number),
					TxID:     tx.ID,
					IsDelete: w.IsDelete,
				})
			}
		}
	}

	// Debug: JSON dump all updates with truncated values
	if len(updates) > 0 {
		logUpdates := make([]logUpdate, len(updates))
		for i, u := range updates {
			logUpdates[i] = u.toLogUpdate()
		}

		if false {
			if jsonData, err := json.MarshalIndent(logUpdates, "", "  "); err == nil {
				fmt.Printf("[LightKVS Handle] Block %d, %d updates:\n%s\n", b.Number, len(updates), string(jsonData))
			}
		}
	}

	// Apply all updates atomically
	if len(updates) > 0 {
		return kvs.Update(updates)
	}

	return nil
}

// BlockNumber returns the current snapshot block number.
// This implements the BlockHeightReader interface for the synchronizer.
func (kvs *LightKVS) BlockNumber(ctx context.Context) (uint64, error) {
	snapshot := kvs.current.Load()
	return snapshot.blockNumber, nil
}

// Close is a no-op for LightKVS since there are no resources to clean up.
// It's provided for interface compatibility.
func (kvs *LightKVS) Close() error {
	return nil
}
