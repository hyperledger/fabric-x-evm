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
// The Reader will see a consistent snapshot of the store at the requested block number.
// If blockNumber is 0, it returns the current snapshot (latest state).
// Otherwise, if the requested snapshot is in the past, the preserved snapshot is selected
// by hop distance from the current block number.
// Readers must call Close() when done to allow garbage collection of old snapshots.
func (kvs *RevertibleLightKVS) NewSnapshot(blockNumber uint64) (execution.ReadStore, error) {
	current := kvs.Current.Load()
	count := int(kvs.NextIndex.Load())
	availableBlocks := make([]uint64, 0, count+1)
	for i := 0; i < count; i++ {
		snapshot := kvs.History[i].Load()
		if snapshot != nil {
			availableBlocks = append(availableBlocks, snapshot.BlockNumber)
		}
	}
	availableBlocks = append(availableBlocks, current.BlockNumber)

	revertLogger.Debugf("RevertibleLightKVS.NewSnapshot() called: requested=%d current=%d historyCount=%d available=%v",
		blockNumber, current.BlockNumber, count, availableBlocks)

	if blockNumber == 0 || blockNumber >= current.BlockNumber {
		revertLogger.Debugf("RevertibleLightKVS.NewSnapshot() returning current snapshot: requested=%d returned=%d available=%v",
			blockNumber, current.BlockNumber, availableBlocks)
		return &Reader{
			Snapshot: current,
			Kvs:      kvs.LightKVS,
		}, nil
	}

	distance := current.BlockNumber - blockNumber
	if distance > uint64(count) {
		err := fmt.Errorf("snapshot not found for block number %d", blockNumber)
		revertLogger.Debugf("RevertibleLightKVS.NewSnapshot() returning error: requested=%d current=%d distance=%d historyCount=%d available=%v err=%v",
			blockNumber, current.BlockNumber, distance, count, availableBlocks, err)
		return nil, err
	}

	targetIndex := count - int(distance)
	snapshot := kvs.History[targetIndex].Load()
	if snapshot == nil {
		err := fmt.Errorf("snapshot not found for block number %d", blockNumber)
		revertLogger.Debugf("RevertibleLightKVS.NewSnapshot() returning error: requested=%d current=%d distance=%d targetIndex=%d historyCount=%d available=%v err=%v",
			blockNumber, current.BlockNumber, distance, targetIndex, count, availableBlocks, err)
		return nil, err
	}

	revertLogger.Debugf("RevertibleLightKVS.NewSnapshot() returning historical snapshot: requested=%d returned=%d distance=%d targetIndex=%d historyCount=%d available=%v",
		blockNumber, snapshot.BlockNumber, distance, targetIndex, count, availableBlocks)
	return &Reader{
		Snapshot: snapshot,
		Kvs:      kvs.LightKVS,
	}, nil
}

// Update atomically applies a batch of updates to the store.
// This overrides the base Update to use sequential history storage instead of ring buffer.
func (kvs *RevertibleLightKVS) Update(updates []KeyValueVersion) error {
	// Load current snapshot
	oldSnapshot := kvs.Current.Load()

	// Shallow clone the map - copies map structure, shares value pointers
	newData := maps.Clone(oldSnapshot.Data)

	// Update changed entries with new ValueVersion structs
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
		}
	}

	// Create new snapshot with the block number from updates
	blockNum := uint64(0)
	if len(updates) > 0 {
		blockNum = updates[0].BlockNum
	}
	newSnapshot := &Snapshot{
		BlockNumber: blockNum,
		Data:        newData,
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
// Returns an error if the requested block number is not found in history or current.
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

	// Search through history snapshots for the requested block number
	var targetSnapshot *Snapshot
	targetIndex := -1
	for i := range kvs.History {
		snapshot := kvs.History[i].Load()
		if snapshot != nil && snapshot.BlockNumber == blockNumber {
			targetSnapshot = snapshot
			targetIndex = i
			break
		}
	}

	if targetSnapshot == nil {
		// No matching snapshot found
		revertLogger.Debugf("RevertibleLightKVS.RevertToBlock() snapshot not found for block %d", blockNumber)
		return fmt.Errorf("cannot revert: snapshot not found for block number %d", blockNumber)
	}

	revertLogger.Debugf("RevertibleLightKVS.RevertToBlock() found target snapshot at block %d, performing merge", blockNumber)

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
