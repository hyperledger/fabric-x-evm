/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-evm/endorser"
)

var logger = flogging.MustGetLogger("endorser.testimpl.lightkvs")

// LightKVSExt extends LightKVS with additional test-specific functionality.
// It embeds the base LightKVS and adds methods for snapshot management and revert operations.
// Uses the base LightKVS.NextIndex for sequential storage instead of ring buffer.
type LightKVSExt struct {
	*endorser.LightKVS
}

// NewLightKVSExt wraps an existing LightKVS with extended test-specific features.
// The lightKVS parameter must not be nil.
// Resets NextIndex to 0 for sequential storage mode.
func NewLightKVSExt(lightKVS *endorser.LightKVS) *LightKVSExt {
	// Reset NextIndex to 0 for sequential storage (not ring buffer)
	lightKVS.NextIndex.Store(0)
	return &LightKVSExt{
		LightKVS: lightKVS,
	}
}

// NewSnapshot starts a new read transaction and returns a Reader for the specified block number.
// The Reader will see a consistent snapshot of the store at the requested block number.
// If blockNumber is 0, it returns the current snapshot (latest state).
// Otherwise, if the requested snapshot is in the past, the preserved snapshot is selected
// by hop distance from the current block number.
// Readers must call Close() when done to allow garbage collection of old snapshots.
func (kvs *LightKVSExt) NewSnapshot(blockNumber uint64) (endorser.ReadStore, error) {
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

	logger.Debugf("LightKVSExt.NewSnapshot() called: requested=%d current=%d historyCount=%d available=%v",
		blockNumber, current.BlockNumber, count, availableBlocks)

	if blockNumber == 0 || blockNumber >= current.BlockNumber {
		logger.Debugf("LightKVSExt.NewSnapshot() returning current snapshot: requested=%d returned=%d available=%v",
			blockNumber, current.BlockNumber, availableBlocks)
		return &endorser.Reader{
			Snapshot: current,
			Kvs:      kvs.LightKVS,
		}, nil
	}

	distance := current.BlockNumber - blockNumber
	if distance > uint64(count) {
		err := fmt.Errorf("snapshot not found for block number %d", blockNumber)
		logger.Debugf("LightKVSExt.NewSnapshot() returning error: requested=%d current=%d distance=%d historyCount=%d available=%v err=%v",
			blockNumber, current.BlockNumber, distance, count, availableBlocks, err)
		return nil, err
	}

	targetIndex := count - int(distance)
	snapshot := kvs.History[targetIndex].Load()
	if snapshot == nil {
		err := fmt.Errorf("snapshot not found for block number %d", blockNumber)
		logger.Debugf("LightKVSExt.NewSnapshot() returning error: requested=%d current=%d distance=%d targetIndex=%d historyCount=%d available=%v err=%v",
			blockNumber, current.BlockNumber, distance, targetIndex, count, availableBlocks, err)
		return nil, err
	}

	logger.Debugf("LightKVSExt.NewSnapshot() returning historical snapshot: requested=%d returned=%d distance=%d targetIndex=%d historyCount=%d available=%v",
		blockNumber, snapshot.BlockNumber, distance, targetIndex, count, availableBlocks)
	return &endorser.Reader{
		Snapshot: snapshot,
		Kvs:      kvs.LightKVS,
	}, nil
}

// Update atomically applies a batch of updates to the store.
// This overrides the base Update to use sequential history storage instead of ring buffer.
func (kvs *LightKVSExt) Update(updates []endorser.KeyValueVersion) error {
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
			newData[update.Key] = &endorser.ValueVersion{
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
					"value":     truncateValue(update.Value, 32),
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
	blockNum := uint64(0)
	if len(updates) > 0 {
		blockNum = updates[0].BlockNum
	}
	newSnapshot := &endorser.Snapshot{
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
func (kvs *LightKVSExt) RevertToBlock(blockNumber uint64) error {
	logger.Debugf("LightKVSExt.RevertToBlock() called with blockNumber=%d", blockNumber)

	// Check if the requested block is already the current snapshot (no-op)
	currentSnapshot := kvs.Current.Load()
	logger.Debugf("LightKVSExt.RevertToBlock() current block number: %d", currentSnapshot.BlockNumber)

	if currentSnapshot.BlockNumber == blockNumber {
		// Already at this block - no-op, return success
		logger.Debugf("LightKVSExt.RevertToBlock() already at block %d, no-op", blockNumber)
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
	logger.Debugf("LightKVSExt.RevertToBlock() searching history snapshots: %v", availableBlocks)

	// Search through history snapshots for the requested block number
	var targetSnapshot *endorser.Snapshot
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
		logger.Debugf("LightKVSExt.RevertToBlock() snapshot not found for block %d", blockNumber)
		return fmt.Errorf("cannot revert: snapshot not found for block number %d", blockNumber)
	}

	logger.Debugf("LightKVSExt.RevertToBlock() found target snapshot at block %d, performing merge", blockNumber)

	// Create a new merged snapshot
	// Start with a clone of the target snapshot's data
	mergedData := maps.Clone(targetSnapshot.Data)

	// Process keys that exist in current but not in target (created after target)
	// These keys need to be preserved with their current version info but with nil value
	for key, currentValue := range currentSnapshot.Data {
		if _, existsInTarget := targetSnapshot.Data[key]; !existsInTarget {
			// Key was created after target snapshot
			// Keep it with nil value but preserve version info from current ledger
			logger.Debugf("LightKVSExt.RevertToBlock() key %s created after target, preserving with nil value: version=%d, blockNum=%d, txNum=%d, isDelete=false",
				key, currentValue.Version, currentValue.BlockNum, currentValue.TxNum)
			mergedData[key] = &endorser.ValueVersion{
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
			logger.Debugf("LightKVSExt.RevertToBlock() key %s deleted after target, marking as deleted: version=%d, blockNum=%d, txNum=%d, isDelete=true",
				key, deleteVersion, targetValue.BlockNum, targetValue.TxNum)
			mergedData[key] = &endorser.ValueVersion{
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
			logger.Debugf("LightKVSExt.RevertToBlock() key %s exists in both, using target value with current version: version=%d, blockNum=%d, txNum=%d, isDelete=%v",
				key, currentValue.Version, currentValue.BlockNum, currentValue.TxNum, currentValue.IsDelete)
			mergedData[key] = &endorser.ValueVersion{
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
	mergedSnapshot := &endorser.Snapshot{
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

	logger.Debugf("LightKVSExt.RevertToBlock() successfully reverted to block %d with merged state and trimmed future history", blockNumber)
	return nil
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
