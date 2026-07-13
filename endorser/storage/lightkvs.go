/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync/atomic"

	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

var (
	// ErrKeyNotFound is returned when a key is not found in the store.
	ErrKeyNotFound = errors.New("key not found")
)

// KVS is implemented by both LightKVS and VersionedDBWrapper.
// It combines snapshot reads, block handling, and lifecycle management.
type KVS interface {
	execution.KVSSnapshotter
	blocks.BlockHandler
	blocks.RecordGetter
	BlockNumber(context.Context) (uint64, error)
	Close() error
}

// LightKVS is a lightweight versioned key-value store with snapshot isolation.
// It supports concurrent readers and a single writer.
type LightKVS struct {
	// Atomic pointer to current snapshot
	// Readers get this atomically, writers swap it atomically
	Current atomic.Pointer[Snapshot]

	// Ring buffer of recent snapshots for history preservation
	// Size determines how many snapshots to keep (e.g., 2 for last 2 snapshots)
	History []atomic.Pointer[Snapshot]

	// Next index in the ring buffer to write to
	NextIndex atomic.Uint32
}

// Snapshot represents an immutable point-in-time view of the key-value store.
type Snapshot struct {
	// BlockNumber is the block number of this snapshot
	BlockNumber uint64

	// Data is the map from key to pointer to immutable value
	// Multiple snapshots can share pointers to unchanged values
	Data map[string]*ValueVersion
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

	// IsDelete indicates if this is a delete operation
	IsDelete bool
}

// Reader provides a consistent view of the store at a specific point in time.
// All Get operations see the state as it was when Begin() was called.
// Reader implements the execution.ReadStore interface for compatibility with StateDB.
type Reader struct {
	// Snapshot holds a reference to the immutable snapshot
	// This prevents the snapshot from being garbage collected
	Snapshot *Snapshot

	// Kvs reference for BlockNumber queries
	Kvs *LightKVS
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
func NewLightKVS(historySize int) *LightKVS {
	kvs := &LightKVS{
		History: make([]atomic.Pointer[Snapshot], historySize),
	}
	initial := &Snapshot{
		BlockNumber: 0,
		Data:        make(map[string]*ValueVersion),
	}
	kvs.Current.Store(initial)
	// NextIndex starts at 0 - history slots are initially nil
	kvs.NextIndex.Store(0)
	return kvs
}

// NewSnapshot starts a new read transaction and returns a Reader for the specified block number.
// The Reader will see a consistent snapshot of the store at the requested block number.
// If blockNumber is 0, it returns the current snapshot (latest state).
// Otherwise, it first checks the current snapshot, then searches the history for a matching block number.
// If no matching snapshot is found, it returns an error.
//
// Readers must call Close() when done to allow garbage collection of old snapshots.
func (kvs *LightKVS) NewSnapshot(blockNumber uint64) (execution.ReadStore, error) {
	// Load the current snapshot
	current := kvs.Current.Load()

	// If blockNumber is 0, return the current snapshot (latest state)
	if blockNumber == 0 {
		return &Reader{
			Snapshot: current,
			Kvs:      kvs,
		}, nil
	}

	// Check if requested snapshot matches or is greater than the current block number
	if blockNumber >= current.BlockNumber {
		return &Reader{
			Snapshot: current,
			Kvs:      kvs,
		}, nil
	}

	// Search through history snapshots for the requested block number
	for i := range kvs.History {
		snapshot := kvs.History[i].Load()
		if snapshot != nil && snapshot.BlockNumber == blockNumber {
			return &Reader{
				Snapshot: snapshot,
				Kvs:      kvs,
			}, nil
		}
	}

	// No matching snapshot found
	return nil, fmt.Errorf("snapshot not found for block number %d", blockNumber)
}

func (kvs *LightKVS) Get(namespace, key string, lastBlock uint64) (*blocks.WriteRecord, error) {
	r, err := kvs.NewSnapshot(lastBlock)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	return r.Get(namespace, key)
}

// Get retrieves the value and version for a key from the reader's snapshot.
// This implements the execution.ReadStore interface with the signature:
// Get(namespace, key string) (*blocks.WriteRecord, error)
func (r *Reader) Get(namespace, key string) (*blocks.WriteRecord, error) {
	if r.Snapshot == nil {
		return nil, errors.New("reader is closed")
	}

	// Prepend namespace to key
	fullKey := namespace + ":" + key

	if vv, ok := r.Snapshot.Data[fullKey]; ok {
		record := &blocks.WriteRecord{
			Namespace: namespace,
			Key:       key,
			BlockNum:  vv.BlockNum,
			TxNum:     vv.TxNum,
			Version:   vv.Version,
			Value:     vv.Value,
			IsDelete:  vv.IsDelete,
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
				"value":     truncateValue(vv.Value, 32),
				"is_delete": vv.IsDelete,
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
	r.Snapshot = nil
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
	oldSnapshot := kvs.Current.Load()

	// Shallow clone the map - copies map structure, shares value pointers
	// This is O(n) but highly optimized in Go's runtime
	newData := maps.Clone(oldSnapshot.Data)

	// Update changed entries with new ValueVersion structs
	// Only these allocations are new; unchanged entries share pointers
	for _, update := range updates {
		if update.IsDelete {
			// Delete: remove the key from the map
			delete(newData, update.Key)
		} else {
			// Compute next version for this key: existing version + 1, or 0 if new
			nextVersion := uint64(0)
			if existing, ok := oldSnapshot.Data[update.Key]; ok {
				nextVersion = existing.Version + 1
			}

			// Update: set new value (Value can be nil, which is a valid stored value)
			newData[update.Key] = &ValueVersion{
				Value:    update.Value,
				BlockNum: update.BlockNum,
				TxNum:    update.TxNum,
				Version:  nextVersion,
				TxID:     update.TxID,
				IsDelete: false,
			}

			if false {
				// Debug: JSON dump the record with truncated value
				logRec := map[string]interface{}{
					"key":       update.Key,
					"block_num": update.BlockNum,
					"tx_num":    update.TxNum,
					"version":   nextVersion,
					"value":     truncateValue(update.Value, 20),
					"is_delete": false,
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
		BlockNumber: blockNum,
		Data:        newData,
	}

	// Get the next history slot to write to
	idx := kvs.NextIndex.Load()

	// Store old snapshot in the ring buffer
	kvs.History[idx].Store(oldSnapshot)

	// Advance to next slot (wraps around using modulo)
	nextIdx := (idx + 1) % uint32(len(kvs.History))
	kvs.NextIndex.Store(nextIdx)

	// Atomically swap in the new snapshot
	// New readers will see this snapshot; existing readers keep their old snapshot
	kvs.Current.Store(newSnapshot)

	return nil
}

// collectWrites is a private helper that extracts writes from namespace read-write sets
// and appends them to the provided updates slice. This is the common logic used by both Handle and HandleTx.
func collectWrites(updates *[]KeyValueVersion, nsrwsList []blocks.NsReadWriteSet, blockNum, txNum uint64, txID string, valid bool) {
	if !valid {
		// Skip invalid transactions
		return
	}

	for _, nsrws := range nsrwsList {
		for _, w := range nsrws.RWS.Writes {
			// Create a key that includes the namespace
			key := nsrws.Namespace + ":" + w.Key

			*updates = append(*updates, KeyValueVersion{
				Key:      key,
				Value:    w.Value,
				BlockNum: blockNum,
				TxNum:    txNum,
				TxID:     txID,
				IsDelete: w.IsDelete,
			})
		}
	}
}

// Handle implements the blocks.BlockHandler interface.
// It processes a block by extracting all valid transaction writes and applying them atomically.
// This is called by the synchronizer when a new block is committed to the ledger.
func (kvs *LightKVS) Handle(ctx context.Context, b blocks.Block) error {
	// Collect all writes from all transactions in the block
	var allUpdates []KeyValueVersion

	for _, tx := range b.Transactions {
		collectWrites(&allUpdates, tx.NsRWS, b.Number, uint64(tx.Number), tx.ID, tx.Valid)
	}

	// Apply all updates atomically in a single Update call
	if len(allUpdates) > 0 {
		return kvs.Update(allUpdates)
	}

	return nil
}

// HandleTx implements the core.TxHandler interface.
// It processes a batch of transaction notifications by extracting writes and applying them.
// This is called by the notification dispatcher when transactions are committed.
func (kvs *LightKVS) HandleTx(ctx context.Context, notifs []common.TxNotification) error {
	// Collect all writes from all notifications in the batch
	var allUpdates []KeyValueVersion

	for _, notif := range notifs {
		// Status check: committerpb.Status_COMMITTED means success
		valid := notif.Status == committerpb.Status_COMMITTED
		collectWrites(&allUpdates, notif.NsRWS, notif.BlockNum, notif.TxNum, notif.FabricTxID, valid)
	}

	// Apply all updates atomically in a single Update call
	if len(allUpdates) > 0 {
		return kvs.Update(allUpdates)
	}

	return nil
}

// BlockNumber returns the current snapshot block number.
// This implements the BlockHeightReader interface for the synchronizer.
func (kvs *LightKVS) BlockNumber(ctx context.Context) (uint64, error) {
	snapshot := kvs.Current.Load()
	return snapshot.BlockNumber, nil
}

// Close is a no-op for LightKVS since there are no resources to clean up.
// It's provided for interface compatibility.
func (kvs *LightKVS) Close() error {
	return nil
}
