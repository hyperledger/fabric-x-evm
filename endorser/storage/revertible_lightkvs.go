/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"context"
	"fmt"
	"maps"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

var revertLogger = flogging.MustGetLogger("endorser.storage.revertible_lightkvs")

// Revertible is implemented by KVS backends that support interactive rollback to a
// previously committed block — e.g. the Hardhat evm_revert RPC on a single-node
// testnode.
type Revertible interface {
	BlockNumber(ctx context.Context) (uint64, error)
	RevertToBlock(blockNumber uint64) error
}

// RevertibleLightKVS extends LightKVS with snapshot/revert support for interactive
// use (Hardhat's evm_snapshot/evm_revert on a testnode). It embeds the base LightKVS
// and switches its history from a wrapping ring buffer to sequential, non-wrapping
// storage. It can be rewound to any point in its bounded window, but (unlike plain
// LightKVS) it panics once that window is exhausted, so it must never be used for a
// long-running, continuously-committing endorser.
type RevertibleLightKVS struct {
	*LightKVS
}

// NewRevertibleLightKVS wraps an existing LightKVS with revert support.
// The lightKVS parameter must not be nil.
// Resets NextIndex to 0 for sequential storage mode.
func NewRevertibleLightKVS(lightKVS *LightKVS) *RevertibleLightKVS {
	// Reset NextIndex to 0 for sequential storage (not ring buffer)
	lightKVS.NextIndex.Store(0)
	return &RevertibleLightKVS{
		LightKVS: lightKVS,
	}
}

// NewSnapshot starts a new read transaction and returns a Reader for the specified block number.
// nil means latest; a non-nil value is that exact height (including 0 for genesis).
//
// Historical lookups search history by block number (exact match, else the latest
// preserved snapshot at or before the request). Hop-by-distance is wrong when
// empty blocks leave no Update entry: Fabric block numbers then skip, and a
// distance-based index misses snapshots that are still in the ring.
// Readers must call Close() when done to allow garbage collection of old snapshots.
func (kvs *RevertibleLightKVS) NewSnapshot(blockNumber *uint64) (execution.ReadStore, error) {
	current := kvs.Current.Load()
	count := int(kvs.NextIndex.Load())

	if blockNumber == nil {
		revertLogger.Debugf("RevertibleLightKVS.NewSnapshot() returning current snapshot: requested=latest returned=%d historyCount=%d",
			current.BlockNumber, count)
		return &Reader{Snapshot: current}, nil
	}

	bn := *blockNumber
	if bn >= current.BlockNumber {
		revertLogger.Debugf("RevertibleLightKVS.NewSnapshot() returning current snapshot: requested=%d returned=%d historyCount=%d",
			bn, current.BlockNumber, count)
		return &Reader{Snapshot: current}, nil
	}

	// Exact match on a preserved snapshot, else the nearest older one (state is
	// unchanged across empty blocks that never called Update). Keep the LAST exact
	// match in a block.
	//
	// Scans the whole array rather than [0, NextIndex): matches RevertToBlock's
	// approach below, and doesn't rely on NextIndex being an exact populated-count
	// rather than a ring position — a distinction that only holds because Handle
	// now shares applyBlockSequential's panic-on-exhaustion behavior instead of
	// silently wrapping.
	var exact, best *Snapshot
	for i := range kvs.History {
		snap := kvs.History[i].Load()
		if snap == nil {
			continue
		}
		if snap.BlockNumber == bn {
			exact = snap
			continue
		}
		if snap.BlockNumber < bn && (best == nil || snap.BlockNumber > best.BlockNumber) {
			best = snap
		}
	}
	if exact != nil {
		revertLogger.Debugf("RevertibleLightKVS.NewSnapshot() exact history hit: requested=%d", bn)
		return &Reader{Snapshot: exact}, nil
	}
	if best != nil {
		revertLogger.Debugf("RevertibleLightKVS.NewSnapshot() nearest history hit: requested=%d returned=%d",
			bn, best.BlockNumber)
		return &Reader{Snapshot: best}, nil
	}

	err := fmt.Errorf("snapshot not found for block number %d", bn)
	revertLogger.Debugf("RevertibleLightKVS.NewSnapshot() returning error: requested=%d current=%d historyCount=%d err=%v",
		bn, current.BlockNumber, count, err)
	return nil, err
}

// Get overrides the promoted LightKVS.Get so it resolves through this type's
// own NewSnapshot (with its older-snapshot fallback) instead of LightKVS's
// exact-match-only one.
func (kvs *RevertibleLightKVS) Get(namespace, key string, lastBlock uint64) (*blocks.WriteRecord, error) {
	r, err := kvs.NewSnapshot(blockRefFromLastBlock(lastBlock))
	if err != nil {
		return nil, err
	}
	defer r.Close()

	return r.Get(namespace, key)
}

// Update atomically applies a batch of updates to the store, deriving the
// block number from the batch's first entry (0 for an empty batch). Handle
// passes the block number explicitly instead, so an empty block still
// advances height correctly — see applyBlockSequential, which both share.
func (kvs *RevertibleLightKVS) Update(updates []KeyValueVersion) error {
	blockNum := uint64(0)
	if len(updates) > 0 {
		blockNum = updates[0].BlockNum
	}
	return kvs.applyBlockSequential(blockNum, updates)
}

// Handle overrides the promoted LightKVS.Handle so synchronizer-delivered
// blocks get the same sequential, non-wrapping history Update already gives
// direct callers — LightKVS.Handle instead wraps the ring buffer, silently
// losing old entries that NewSnapshot's bounded scan can no longer tell from
// still-populated ones.
//
// The replay guard itself mirrors LightKVS.Handle exactly: older blocks are
// skipped unverified, and a tip redelivery goes through verifyReplay.
func (kvs *RevertibleLightKVS) Handle(ctx context.Context, b blocks.Block) error {
	current := kvs.Current.Load()
	if kvs.hasCheckpoint.Load() && b.Number < current.BlockNumber {
		return nil
	}

	var updates []KeyValueVersion
	for _, tx := range b.Transactions {
		collectWrites(&updates, tx.NsRWS, b.Number, uint64(tx.Number), tx.ID, tx.Valid)
	}

	if kvs.hasCheckpoint.Load() && b.Number == current.BlockNumber {
		return verifyReplay(current, updates)
	}

	return kvs.applyBlockSequential(b.Number, updates)
}

// applyBlockSequential computes a new snapshot at blockNum from updates,
// using the same per-key version computation as LightKVS.applyBlock, but
// appends to history sequentially instead of wrapping — panicking once the
// window is exhausted rather than silently overwriting older entries, so
// this type must never be used for a long-running, continuously-committing
// endorser (see the type doc comment).
func (kvs *RevertibleLightKVS) applyBlockSequential(blockNum uint64, updates []KeyValueVersion) error {
	// Load current snapshot
	oldSnapshot := kvs.Current.Load()

	newSnapshot := &Snapshot{
		BlockNumber: blockNum,
		Data:        applyUpdates(oldSnapshot.Data, updates),
	}

	// Sequential storage: append to history without wrapping
	count := int(kvs.NextIndex.Load())
	if count >= len(kvs.History) {
		panic(fmt.Sprintf("snapshot history exhausted at block %d", blockNum))
	}

	if len(kvs.History) > 0 {
		kvs.History[count].Store(oldSnapshot)
		kvs.NextIndex.Store(uint32(count + 1))
	}

	// Atomically swap in the new snapshot
	kvs.Current.Store(newSnapshot)
	kvs.hasCheckpoint.Store(true)

	return nil
}

// RevertToBlock reverts the current state to a specific block number from history.
// It searches through the history for a snapshot matching the requested block number,
// and if found, merges it with the current snapshot to preserve version information.
//
// The merge process handles MVCC conflicts by:
// - If a key exists in both snapshots: use target snapshot's value but current snapshot's version info
// - If a key exists only in current (created after target): keep it with nil value and current version
// - If a key exists only in target (deleted after target): mark it as deleted with current version
//
// This ensures that when we revert and simulate new transactions, the read dependencies
// will match the actual versions in the peer's ledger, avoiding MVCC conflicts.
//
// If the requested block number matches the current snapshot, it's a no-op and returns success.
// Like NewSnapshot, falls back to the nearest older snapshot when there's no exact
// match: an empty block never calls Update, so it never gets a history entry, but
// state is unchanged since the last one that did. Returns an error only when no
// snapshot at or before the requested block number exists in history or current.
func (kvs *RevertibleLightKVS) RevertToBlock(blockNumber uint64) error {
	revertLogger.Debugf("RevertibleLightKVS.RevertToBlock() called with blockNumber=%d", blockNumber)

	// Check if the requested block is already the current snapshot (no-op)
	currentSnapshot := kvs.Current.Load()
	revertLogger.Debugf("RevertibleLightKVS.RevertToBlock() current block number: %d", currentSnapshot.BlockNumber)

	if currentSnapshot.BlockNumber == blockNumber {
		// Already at this block - no-op, return success
		revertLogger.Debugf("RevertibleLightKVS.RevertToBlock() already at block %d, no-op", blockNumber)
		return nil
	}

	// Log available history snapshots
	availableBlocks := []uint64{}
	for i := range kvs.History {
		snapshot := kvs.History[i].Load()
		if snapshot != nil {
			availableBlocks = append(availableBlocks, snapshot.BlockNumber)
		}
	}
	revertLogger.Debugf("RevertibleLightKVS.RevertToBlock() searching history snapshots: %v", availableBlocks)

	// Search history for the requested block number, keeping the LAST match
	// rather than the first. Startup funding writes its state at the current
	// height without advancing it, so block 0 exists twice: empty, then funded.
	// Also track the nearest older snapshot as a fallback (see NewSnapshot):
	// an empty block leaves no exact entry to find.
	var targetSnapshot, bestSnapshot *Snapshot
	targetIndex, bestIndex := -1, -1
	for i := range kvs.History {
		snapshot := kvs.History[i].Load()
		if snapshot == nil {
			continue
		}
		if snapshot.BlockNumber == blockNumber {
			targetSnapshot = snapshot
			targetIndex = i
			continue
		}
		if snapshot.BlockNumber < blockNumber && (bestSnapshot == nil || snapshot.BlockNumber > bestSnapshot.BlockNumber) {
			bestSnapshot = snapshot
			bestIndex = i
		}
	}

	// Only fall back for a target in the past.
	if targetSnapshot == nil && blockNumber < currentSnapshot.BlockNumber {
		targetSnapshot, targetIndex = bestSnapshot, bestIndex
	}

	if targetSnapshot == nil {
		// No matching or older snapshot found
		revertLogger.Debugf("RevertibleLightKVS.RevertToBlock() snapshot not found for block %d", blockNumber)
		return fmt.Errorf("cannot revert: snapshot not found for block number %d", blockNumber)
	}

	revertLogger.Debugf("RevertibleLightKVS.RevertToBlock() found target snapshot at block %d (requested %d), performing merge", targetSnapshot.BlockNumber, blockNumber)

	// Create a new merged snapshot
	// Start with a clone of the target snapshot's data
	mergedData := maps.Clone(targetSnapshot.Data)

	// Process keys that exist in current but not in target (created after target)
	// These keys need to be preserved with their current version info but with nil value
	for key, currentValue := range currentSnapshot.Data {
		if _, existsInTarget := targetSnapshot.Data[key]; !existsInTarget {
			// Key was created after target snapshot
			// Keep it with nil value but preserve version info from current ledger
			revertLogger.Debugf("RevertibleLightKVS.RevertToBlock() key %s created after target, preserving with nil value: version=%d, blockNum=%d, txNum=%d, isDelete=false",
				key, currentValue.Version, currentValue.BlockNum, currentValue.TxNum)
			mergedData[key] = &ValueVersion{
				Value:    nil, // Nil out the value
				BlockNum: currentValue.BlockNum,
				TxNum:    currentValue.TxNum,
				Version:  currentValue.Version,
				TxID:     currentValue.TxID,
				IsDelete: false,
			}
		}
	}

	// Process keys that exist in target but not in current (deleted after target)
	// These keys need to be marked as deleted with current version info
	for key, targetValue := range targetSnapshot.Data {
		if currentValue, existsInCurrent := currentSnapshot.Data[key]; !existsInCurrent {
			// Key was deleted after target snapshot
			// Mark it as deleted but we need to infer the version from what would be in the ledger
			// Since it was deleted, the ledger has a delete record with a version
			// We'll use target's version + 1 to represent the delete operation
			deleteVersion := targetValue.Version + 1
			revertLogger.Debugf("RevertibleLightKVS.RevertToBlock() key %s deleted after target, marking as deleted: version=%d, blockNum=%d, txNum=%d, isDelete=true",
				key, deleteVersion, targetValue.BlockNum, targetValue.TxNum)
			mergedData[key] = &ValueVersion{
				Value:    targetValue.Value, // Keep target's value for reference
				BlockNum: targetValue.BlockNum,
				TxNum:    targetValue.TxNum,
				Version:  deleteVersion,
				TxID:     targetValue.TxID,
				IsDelete: true,
			}
		} else {
			// Key exists in both snapshots
			// Use target's value but update version info from current
			revertLogger.Debugf("RevertibleLightKVS.RevertToBlock() key %s exists in both, using target value with current version: version=%d, blockNum=%d, txNum=%d, isDelete=%v",
				key, currentValue.Version, currentValue.BlockNum, currentValue.TxNum, currentValue.IsDelete)
			mergedData[key] = &ValueVersion{
				Value:    targetValue.Value,     // Use target's value (the reverted state)
				BlockNum: currentValue.BlockNum, // Use current's version info
				TxNum:    currentValue.TxNum,
				Version:  currentValue.Version,
				TxID:     currentValue.TxID,
				IsDelete: currentValue.IsDelete,
			}
		}
	}

	// Create the merged snapshot
	mergedSnapshot := &Snapshot{
		BlockNumber: blockNumber,
		Data:        mergedData,
	}

	// Atomically swap in the merged snapshot
	kvs.Current.Store(mergedSnapshot)

	// Drop all future snapshots after the revert point, but keep the past snapshots.
	count := int(kvs.NextIndex.Load())
	if targetIndex >= 0 {
		for i := targetIndex; i < count; i++ {
			kvs.History[i].Store(nil)
		}
		kvs.NextIndex.Store(uint32(targetIndex))
	}

	revertLogger.Debugf("RevertibleLightKVS.RevertToBlock() successfully reverted to block %d with merged state and trimmed future history", blockNumber)
	return nil
}
