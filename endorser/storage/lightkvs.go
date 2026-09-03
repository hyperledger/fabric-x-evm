/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"sync/atomic"

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

	// hasCheckpoint distinguishes an applied block 0 from a fresh store, which
	// Current.BlockNumber alone cannot.
	hasCheckpoint atomic.Bool
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
// nil means latest; a non-nil value is that exact height (including 0 for genesis).
// If no matching snapshot is found, it returns an error.
//
// Readers must call Close() when done to allow garbage collection of old snapshots.
func (kvs *LightKVS) NewSnapshot(blockNumber *uint64) (execution.ReadStore, error) {
	current := kvs.Current.Load()

	// Latest tip.
	if blockNumber == nil {
		return &Reader{Snapshot: current}, nil
	}

	bn := *blockNumber

	// At or past the tip: serve current (covers current height and "future" asks).
	if bn >= current.BlockNumber {
		return &Reader{Snapshot: current}, nil
	}

	// Historical: exact match only.
	for i := range kvs.History {
		snapshot := kvs.History[i].Load()
		if snapshot != nil && snapshot.BlockNumber == bn {
			return &Reader{Snapshot: snapshot}, nil
		}
	}

	return nil, fmt.Errorf("snapshot not found for block number %d", bn)
}

func (kvs *LightKVS) Get(namespace, key string, lastBlock uint64) (*blocks.WriteRecord, error) {
	// lastBlock 0 keeps the historical Get convention of "latest".
	r, err := kvs.NewSnapshot(blockRefFromLastBlock(lastBlock))
	if err != nil {
		return nil, err
	}
	defer r.Close()

	return r.Get(namespace, key)
}

// blockRefFromLastBlock maps the older Get lastBlock convention (0 = latest)
// onto NewSnapshot's pointer form.
func blockRefFromLastBlock(lastBlock uint64) *uint64 {
	if lastBlock == 0 {
		return nil
	}
	return &lastBlock
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

		return record, nil
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
//
// The block number comes from the batch's first entry; Handle passes it
// explicitly instead, so an empty block still advances the checkpoint.
func (kvs *LightKVS) Update(updates []KeyValueVersion) error {
	blockNum := uint64(0)
	if len(updates) > 0 {
		blockNum = updates[0].BlockNum
	}
	return kvs.applyBlock(blockNum, updates)
}

// applyUpdates computes new snapshot data by applying updates on top of
// oldData, assigning each write the existing version + 1 (or 0 for a new
// key). Shared by LightKVS.applyBlock and
// RevertibleLightKVS.applyBlockSequential, which only differ in how they
// append the resulting snapshot to history.
func applyUpdates(oldData map[string]*ValueVersion, updates []KeyValueVersion) map[string]*ValueVersion {
	// Nothing to apply: share the old map. Snapshots are immutable and every
	// mutation path clones first.
	newData := oldData
	if len(updates) > 0 {
		// Shallow clone the map - copies map structure, shares value pointers
		// This is O(n) but highly optimized in Go's runtime
		newData = maps.Clone(oldData)
	}

	// Update changed entries with new ValueVersion structs
	// Only these allocations are new; unchanged entries share pointers
	for _, update := range updates {
		if update.IsDelete {
			// Delete: remove the key from the map
			delete(newData, update.Key)
		} else {
			// Compute next version for this key: existing version + 1, or 0 if new
			nextVersion := uint64(0)
			if existing, ok := oldData[update.Key]; ok {
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
		}
	}

	return newData
}

// applyBlock swaps in a new snapshot at blockNum with updates applied.
//
// It advances even when updates is empty, keeping height equal to ledger height
// so this KVS can serve as a synchronizer's height reader.
func (kvs *LightKVS) applyBlock(blockNum uint64, updates []KeyValueVersion) error {
	// Load current snapshot
	oldSnapshot := kvs.Current.Load()

	newSnapshot := &Snapshot{
		BlockNumber: blockNum,
		Data:        applyUpdates(oldSnapshot.Data, updates),
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
	kvs.hasCheckpoint.Store(true)

	return nil
}

// collectWrites is a private helper that extracts writes from namespace read-write sets
// and appends them to the provided updates slice.
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

// Handle implements blocks.BlockHandler, applying a block's valid writes
// atomically.
//
// A tip redelivery is verified, not just skipped: Update derives versions
// from current state, so re-applying it would double-bump MVCC read-set
// versions and the committer would reject later transactions on those keys.
// A shared synchronizer always redelivers identical content on resume, so an
// identical replay is a no-op and a differing one is a loud error.
//
// Anything older than the tip is skipped unverified — LightKVS only retains
// historySize recent snapshots, so there's nothing left to check it against.
func (kvs *LightKVS) Handle(ctx context.Context, b blocks.Block) error {
	current := kvs.Current.Load()
	if kvs.hasCheckpoint.Load() && b.Number < current.BlockNumber {
		return nil
	}

	// Collect all writes from all transactions in the block
	var allUpdates []KeyValueVersion

	for _, tx := range b.Transactions {
		collectWrites(&allUpdates, tx.NsRWS, b.Number, uint64(tx.Number), tx.ID, tx.Valid)
	}

	if kvs.hasCheckpoint.Load() && b.Number == current.BlockNumber {
		return verifyReplay(current, allUpdates)
	}

	// Applied even with no writes, so the checkpoint tracks ledger height.
	return kvs.applyBlock(b.Number, allUpdates)
}

// verifyReplay checks a redelivery of the current tip against what's already
// stored, erroring if the net effect of updates (last write per key wins,
// matching applyBlock) differs from current. LightKVS stores only the final
// per-key value after a block's writes, not each write individually, so
// comparison happens at that same granularity.
func verifyReplay(current *Snapshot, updates []KeyValueVersion) error {
	final := make(map[string]KeyValueVersion, len(updates))
	for _, u := range updates {
		final[u.Key] = u
	}
	for key, u := range final {
		existing, ok := current.Data[key]
		if u.IsDelete {
			if ok {
				return fmt.Errorf("conflicting write for %q at block=%d: existing value present, replayed is a delete", key, u.BlockNum)
			}
			continue
		}
		if !ok || !bytes.Equal(existing.Value, u.Value) || existing.TxID != u.TxID {
			return fmt.Errorf("conflicting write for %q at block=%d: replayed content differs from existing", key, u.BlockNum)
		}
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
