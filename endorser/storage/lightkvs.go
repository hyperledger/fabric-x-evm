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
	"slices"
	"sync"
	"sync/atomic"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

var lkvLogger = flogging.MustGetLogger("endorser.storage.lightkvs")

// ErrHistoryUnavailable is returned for a snapshot at a height below the
// history a store retains.
var ErrHistoryUnavailable = errors.New("history unavailable at requested height")

// KVS is a versioned world-state store: snapshot reads, block handling, and
// lifecycle management.
// There is one read API per question a caller can ask. Get (from
// blocks.RecordGetter) answers "what is the current value", and NewSnapshot (from
// execution.KVSSnapshotter) answers "what was the value at block N" — note that its
// nil means latest while its 0 means genesis, so the two cannot be collapsed into one
// signature.
//
// A snapshot at a height past the tip serves the tip. A height inside the
// retained history serves the state as of that height, also when no block was
// delivered at it. A height below it returns ErrHistoryUnavailable.
type KVS interface {
	execution.KVSSnapshotter
	blocks.BlockHandler
	blocks.RecordGetter
	BlockNumber(context.Context) (uint64, error)
	Close() error
}

// Revertible is implemented by KVS backends that support interactive rollback to a
// previously committed block — e.g. the Hardhat evm_revert RPC on a single-node
// testnode.
type Revertible interface {
	BlockNumber(ctx context.Context) (uint64, error)
	RevertToBlock(blockNumber uint64) error
}

// LightKVS is an in-memory versioned key-value store with snapshot isolation
// and a bounded history of recent blocks. It supports concurrent readers and a
// single writer.
//
// Every block clones the whole state map, so it suits tests, dev nodes and
// small state.
type LightKVS struct {
	// current is the latest snapshot. Readers load it without locking; writers
	// swap it atomically.
	current atomic.Pointer[snapshot]

	// mu guards history.
	mu sync.Mutex
	// history holds up to historySize earlier snapshots, oldest first.
	history     []*snapshot
	historySize int
}

// snapshot is an immutable point-in-time view of the store.
type snapshot struct {
	// BlockNumber is the block number of this snapshot
	BlockNumber uint64

	// Data is the map from key to pointer to immutable value
	// Multiple snapshots can share pointers to unchanged values
	Data map[string]*valueVersion

	// Placeholder is true only for the pre-genesis snapshot NewLightKVS
	// constructs, distinguishing it from a real commit of block 0.
	Placeholder bool
}

// valueVersion is a versioned value in the store.
type valueVersion struct {
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

// reader is a consistent view of the store at one snapshot. It implements
// execution.ReadStore.
type reader struct {
	// snap holds a reference to the immutable snapshot, which keeps it from
	// being garbage collected.
	snap *snapshot
}

// keyValueVersion is a key-value pair with version for batch updates.
type keyValueVersion struct {
	Key      string
	Value    []byte // Can be nil for storing nil values
	BlockNum uint64
	TxNum    uint64
	TxID     string
	IsDelete bool // True to delete the key, false to store Value (even if nil)
}

// NewLightKVS creates a new empty store that retains historySize earlier
// snapshots for historical reads and RevertToBlock. A historySize of zero or
// less keeps no history: only the latest state can be read.
func NewLightKVS(historySize int) *LightKVS {
	historySize = max(historySize, 0)
	kvs := &LightKVS{
		history:     make([]*snapshot, 0, historySize),
		historySize: historySize,
	}
	kvs.current.Store(&snapshot{
		BlockNumber: 0,
		Data:        make(map[string]*valueVersion),
		Placeholder: true,
	})
	return kvs
}

// HistorySize returns how many earlier snapshots the store retains.
func (kvs *LightKVS) HistorySize() int {
	return kvs.historySize
}

// NewSnapshot starts a new read transaction and returns a reader for the specified block number.
// nil means latest; a non-nil value is that height (including 0 for genesis), served
// as described on KVS.
//
// Readers must call Close() when done to allow garbage collection of old snapshots.
func (kvs *LightKVS) NewSnapshot(blockNumber *uint64) (execution.ReadStore, error) {
	current := kvs.current.Load()
	if blockNumber == nil || *blockNumber >= current.BlockNumber {
		return &reader{snap: current}, nil
	}

	kvs.mu.Lock()
	defer kvs.mu.Unlock()
	i := kvs.historyIndexLocked(*blockNumber)
	if i < 0 {
		return nil, fmt.Errorf("%w: block %d (head is %d)", ErrHistoryUnavailable, *blockNumber, current.BlockNumber)
	}
	return &reader{snap: kvs.history[i]}, nil
}

// historyIndexLocked returns the index of the newest retained snapshot at or
// below block, or -1 if there is none. A height with no snapshot of its own was
// not delivered, so the state is unchanged since the one before it. Taking the
// newest also picks the later of two snapshots at one height: startup funding
// writes at height 0 without advancing it, so block 0 exists twice, empty and
// then funded.
//
// The caller must hold mu.
func (kvs *LightKVS) historyIndexLocked(block uint64) int {
	for i, v := range slices.Backward(kvs.history) {
		if v.BlockNumber <= block {
			return i
		}
	}
	return -1
}

// Get implements blocks.RecordGetter, reading the latest committed state. For a read
// at a specific block, take a NewSnapshot instead.
func (kvs *LightKVS) Get(namespace, key string) (*blocks.WriteRecord, error) {
	r := reader{snap: kvs.current.Load()}
	return r.Get(namespace, key)
}

// Get retrieves the value and version for a key from the reader's snapshot.
func (r *reader) Get(namespace, key string) (*blocks.WriteRecord, error) {
	if r.snap == nil {
		return nil, errors.New("reader is closed")
	}

	vv, ok := r.snap.Data[namespace+":"+key]
	if !ok {
		return nil, nil
	}
	return &blocks.WriteRecord{
		Namespace: namespace,
		Key:       key,
		BlockNum:  vv.BlockNum,
		TxNum:     vv.TxNum,
		Version:   vv.Version,
		Value:     vv.Value,
		IsDelete:  vv.IsDelete,
		TxID:      vv.TxID,
	}, nil
}

// Close releases the reader's reference to its snapshot.
// After Close(), the reader cannot be used for further Get operations.
// This allows Go's GC to clean up the snapshot if no other readers reference it.
func (r *reader) Close() error {
	r.snap = nil
	return nil
}

// applyUpdates computes new snapshot data by applying updates on top of
// oldData. Each write takes the key's previous version + 1, so repeated writes
// to one key within a batch get consecutive versions. A delete is stored as a
// tombstone, keeping the counter intact across it.
func applyUpdates(oldData map[string]*valueVersion, updates []keyValueVersion) map[string]*valueVersion {
	// Nothing to apply: share the old map. Snapshots are immutable and every
	// mutation path clones first.
	newData := oldData
	if len(updates) > 0 {
		// Shallow clone the map - copies map structure, shares value pointers
		// This is O(n) but highly optimized in Go's runtime
		newData = maps.Clone(oldData)
	}

	// nextVersion holds the version to assign to the *next* write of each key
	// within this batch, so a key written more than once in one block still
	// gets consecutive versions instead of all being versioned off oldData.
	nextVersion := make(map[string]uint64, len(updates))

	for _, update := range updates {
		version, seen := nextVersion[update.Key]
		if !seen {
			version = 0
			if existing, ok := oldData[update.Key]; ok {
				version = existing.Version + 1
			}
		}
		nextVersion[update.Key] = version + 1

		// Value can be nil, which is a valid stored value; a delete is kept as
		// a tombstone record rather than removed so the version counter
		// survives it.
		newData[update.Key] = &valueVersion{
			Value:    update.Value,
			BlockNum: update.BlockNum,
			TxNum:    update.TxNum,
			Version:  version,
			TxID:     update.TxID,
			IsDelete: update.IsDelete,
		}
	}

	return newData
}

// applyBlock swaps in a new snapshot at blockNum with updates applied, and
// moves the old one into history, forgetting the oldest once history is full.
//
// It advances even when updates is empty, keeping height equal to ledger height
// so this KVS can serve as a synchronizer's height reader.
func (kvs *LightKVS) applyBlock(blockNum uint64, updates []keyValueVersion) {
	kvs.mu.Lock()
	defer kvs.mu.Unlock()

	oldSnapshot := kvs.current.Load()
	newSnapshot := &snapshot{
		BlockNumber: blockNum,
		Data:        applyUpdates(oldSnapshot.Data, updates),
	}

	if kvs.historySize > 0 {
		if len(kvs.history) == kvs.historySize {
			copy(kvs.history, kvs.history[1:])
			kvs.history = kvs.history[:len(kvs.history)-1]
		}
		kvs.history = append(kvs.history, oldSnapshot)
	}

	// New readers will see this snapshot; existing readers keep their old snapshot
	kvs.current.Store(newSnapshot)
}

// collectWrites is a private helper that extracts writes from namespace read-write sets
// and appends them to the provided updates slice.
func collectWrites(updates *[]keyValueVersion, nsrwsList []blocks.NsReadWriteSet, blockNum, txNum uint64, txID string, valid bool) {
	if !valid {
		// Skip invalid transactions
		return
	}

	for _, nsrws := range nsrwsList {
		for _, w := range nsrws.RWS.Writes {
			// Create a key that includes the namespace
			key := nsrws.Namespace + ":" + w.Key

			*updates = append(*updates, keyValueVersion{
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
	lkvLogger.Debugf("LightKVS.Handle() block=%d txs=%d", b.Number, len(b.Transactions))
	current := kvs.current.Load()
	if !current.Placeholder && b.Number < current.BlockNumber {
		lkvLogger.Debugf("LightKVS.Handle() block=%d skipped (current=%d)", b.Number, current.BlockNumber)
		return nil
	}

	// Collect all writes from all transactions in the block
	var allUpdates []keyValueVersion

	for _, tx := range b.Transactions {
		if !tx.Valid() {
			lkvLogger.Warnf("LightKVS.Handle() block=%d txID=%s fabric-invalid status=%s reason=%q", b.Number, tx.ID, tx.Status, tx.Reason)
		}
		collectWrites(&allUpdates, tx.NsRWS, b.Number, uint64(tx.Number), tx.ID, tx.Valid())
	}

	if !current.Placeholder && b.Number == current.BlockNumber {
		if err := verifyReplay(current, allUpdates); err != nil {
			lkvLogger.Warnf("LightKVS.Handle() block=%d replay mismatch: %v", b.Number, err)
			return err
		}
		return nil
	}

	// Applied even with no writes, so the checkpoint tracks ledger height.
	kvs.applyBlock(b.Number, allUpdates)
	return nil
}

// verifyReplay checks a redelivery of the current tip against what's already
// stored, erroring if the net effect of updates (last write per key wins,
// matching applyBlock) differs from current. LightKVS stores only the final
// per-key value after a block's writes, not each write individually, so
// comparison happens at that same granularity.
func verifyReplay(current *snapshot, updates []keyValueVersion) error {
	final := make(map[string]keyValueVersion, len(updates))
	for _, u := range updates {
		final[u.Key] = u
	}
	for key, u := range final {
		existing, ok := current.Data[key]
		if !ok {
			if u.IsDelete {
				return fmt.Errorf("conflicting write for %q at block=%d: nothing stored for this key, replayed is a delete", key, u.BlockNum)
			}
			return fmt.Errorf("conflicting write for %q at block=%d: nothing stored for this key, replayed is a value write", key, u.BlockNum)
		}
		if existing.IsDelete != u.IsDelete {
			if u.IsDelete {
				return fmt.Errorf("conflicting write for %q at block=%d: existing value present, replayed is a delete", key, u.BlockNum)
			}
			return fmt.Errorf("conflicting write for %q at block=%d: existing is a delete, replayed is a value write", key, u.BlockNum)
		}
		if existing.TxID != u.TxID {
			return fmt.Errorf("conflicting write for %q at block=%d: stored by tx %q, replayed by tx %q", key, u.BlockNum, existing.TxID, u.TxID)
		}
		if !u.IsDelete && !bytes.Equal(existing.Value, u.Value) {
			return fmt.Errorf("conflicting write for %q at block=%d: stored value (%d bytes) differs from replayed (%d bytes), both from tx %q", key, u.BlockNum, len(existing.Value), len(u.Value), u.TxID)
		}
	}
	return nil
}

// RevertToBlock rewinds the current state to blockNumber, for interactive use
// only (Hardhat's evm_snapshot/evm_revert on a dev node). It is not part of the
// KVS contract: the real ledger is not rolled back, and a persistent store
// could not stay consistent with it across a restart.
//
// The target is chosen as NewSnapshot would choose it, and history after it is
// dropped. Reverting to the current block is a no-op.
//
// Values and delete-states come from the target, but versions are kept from
// the current state, so later reads build MVCC read-sets that still match the
// ledger. Deletes are tombstones, so every key in the target is still present
// in the current state. A key first written after the target becomes a
// tombstone, so it reads back absent.
func (kvs *LightKVS) RevertToBlock(blockNumber uint64) error {
	kvs.mu.Lock()
	defer kvs.mu.Unlock()

	current := kvs.current.Load()
	if blockNumber == current.BlockNumber {
		return nil
	}
	if blockNumber > current.BlockNumber {
		return fmt.Errorf("cannot revert to block %d: head is %d", blockNumber, current.BlockNumber)
	}
	i := kvs.historyIndexLocked(blockNumber)
	if i < 0 {
		return fmt.Errorf("cannot revert: %w: block %d (head is %d)", ErrHistoryUnavailable, blockNumber, current.BlockNumber)
	}
	target := kvs.history[i]

	merged := make(map[string]*valueVersion, len(current.Data))
	for key, cur := range current.Data {
		vv := &valueVersion{
			BlockNum: cur.BlockNum,
			TxNum:    cur.TxNum,
			Version:  cur.Version,
			TxID:     cur.TxID,
			IsDelete: true,
		}
		if t, ok := target.Data[key]; ok {
			vv.Value, vv.IsDelete = t.Value, t.IsDelete
		}
		merged[key] = vv
	}

	kvs.current.Store(&snapshot{BlockNumber: blockNumber, Data: merged})
	clear(kvs.history[i:])
	kvs.history = kvs.history[:i]

	lkvLogger.Debugf("LightKVS.RevertToBlock() reverted to block %d (from snapshot at %d, head was %d)",
		blockNumber, target.BlockNumber, current.BlockNumber)
	return nil
}

// BlockNumber returns the current snapshot block number.
// This implements the BlockHeightReader interface for the synchronizer.
func (kvs *LightKVS) BlockNumber(ctx context.Context) (uint64, error) {
	return kvs.current.Load().BlockNumber, nil
}

// Close is a no-op for LightKVS since there are no resources to clean up.
// It's provided for interface compatibility.
func (kvs *LightKVS) Close() error {
	return nil
}
