/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later

WARNING: This package contains test-only/unsafe RPC implementations.
DO NOT use in production environments.
*/

package testimpl

import (
	"context"
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	estorage "github.com/hyperledger/fabric-x-evm/endorser/storage"
	"github.com/hyperledger/fabric-x-evm/gateway/storage"
)

var hardhatLogger = flogging.MustGetLogger("gateway.testimpl.hardhat")

// HardhatAPI provides Hardhat-specific RPC methods for testing.
// These are stub implementations that allow Hardhat tests to run but don't
// actually implement the full functionality.
//
// SECURITY WARNING: These methods are for testing only and should NEVER
// be enabled in production environments.
type HardhatAPI struct{}

// NewHardhatAPI creates a new Hardhat API instance.
func NewHardhatAPI() *HardhatAPI {
	return &HardhatAPI{}
}

// SetCode sets the code at a given address (hardhat_setCode).
// This is a stub implementation that returns success but doesn't actually modify state.
// TODO: Implement proper code modification if needed for specific test scenarios.
func (api *HardhatAPI) SetCode(ctx context.Context, address common.Address, code hexutil.Bytes) (bool, error) {
	hardhatLogger.Debugf("HardhatAPI.SetCode() called with address=%s, code length=%d", address.Hex(), len(code))
	// Stub: return success without actually modifying state
	// In a full implementation, this would modify the account's code in the state DB
	hardhatLogger.Debugf("HardhatAPI.SetCode() returning: true")
	return true, nil
}

// EvmAPI provides EVM-specific RPC methods for testing, particularly snapshot/revert.
// Uses LightKVS history mechanism to capture and restore ledger state, and Store
// for database snapshot/revert.
//
// SECURITY WARNING: These methods are for testing only and should NEVER
// be enabled in production environments.
type EvmAPI struct {
	mu       sync.Mutex
	lightKVS estorage.Revertible
	store    storage.Revertible
	// Map snapshot IDs (hex strings) to block numbers
	snapshots map[string]uint64
}

// NewEvmAPI creates a new EVM API instance with LightKVS and Store for state management.
func NewEvmAPI(lightKVS estorage.Revertible, store storage.Revertible) *EvmAPI {
	return &EvmAPI{
		lightKVS:  lightKVS,
		store:     store,
		snapshots: make(map[string]uint64),
	}
}

// Snapshot creates a snapshot of the current state (evm_snapshot).
// Returns the current block number as the snapshot ID.
// Snapshots both the LightKVS state and the Store database.
func (api *EvmAPI) Snapshot(ctx context.Context) (string, error) {
	hardhatLogger.Debugf("EvmAPI.Snapshot() called")
	api.mu.Lock()
	defer api.mu.Unlock()

	// Snapshot the Store database - this returns the current block number
	hardhatLogger.Debugf("EvmAPI.Snapshot() creating Store snapshot")
	blockNumber, err := api.store.Snapshot(ctx)
	if err != nil {
		hardhatLogger.Debugf("EvmAPI.Snapshot() Store snapshot error: %v", err)
		return "", fmt.Errorf("failed to snapshot Store: %w", err)
	}
	hardhatLogger.Debugf("EvmAPI.Snapshot() Store snapshot created successfully at block %d", blockNumber)

	// Use block number as snapshot ID (in hex format for compatibility)
	snapshotID := fmt.Sprintf("0x%x", blockNumber)

	// Store the mapping
	api.snapshots[snapshotID] = blockNumber

	hardhatLogger.Debugf("EvmAPI.Snapshot() stored snapshot: ID=%s -> block=%d", snapshotID, blockNumber)
	hardhatLogger.Debugf("EvmAPI.Snapshot() all snapshots: %v", api.snapshots)

	hardhatLogger.Debugf("EvmAPI.Snapshot(): Created snapshot ID=%s for block=%d", snapshotID, blockNumber)
	hardhatLogger.Debugf("EvmAPI.Snapshot(): Total snapshots in map: %d", len(api.snapshots))
	for id, bn := range api.snapshots {
		hardhatLogger.Debugf("EvmAPI.Snapshot():   - ID=%s -> block=%d", id, bn)
	}

	hardhatLogger.Debugf("EvmAPI.Snapshot() returning: %s (block %d)", snapshotID, blockNumber)
	return snapshotID, nil
}

// Revert reverts the state to a previous snapshot (evm_revert).
// Uses LightKVS.RevertToBlock to restore ledger state and Store.RevertToBlock
// to restore the database state.
func (api *EvmAPI) Revert(ctx context.Context, snapshotID string) (bool, error) {
	hardhatLogger.Debugf("EvmAPI.Revert() called with snapshotID=%s", snapshotID)
	api.mu.Lock()
	defer api.mu.Unlock()

	hardhatLogger.Debugf("EvmAPI.Revert() all snapshots before revert: %v", api.snapshots)

	// Get current block number for logging
	currentBlock, err := api.lightKVS.BlockNumber(ctx)
	if err == nil {
		hardhatLogger.Debugf("EvmAPI.Revert() current block number before revert: %d", currentBlock)
	}

	// Look up the block number for this snapshot ID
	blockNumber, ok := api.snapshots[snapshotID]
	if !ok {
		hardhatLogger.Debugf("EvmAPI.Revert() returning error: invalid snapshot ID: %s", snapshotID)
		return false, fmt.Errorf("invalid snapshot ID: %s", snapshotID)
	}
	hardhatLogger.Debugf("EvmAPI.Revert() found snapshot ID %s -> block %d", snapshotID, blockNumber)

	hardhatLogger.Debugf("EvmAPI.Revert(): Reverting to snapshot ID=%s (block=%d)", snapshotID, blockNumber)
	hardhatLogger.Debugf("EvmAPI.Revert(): Available snapshots before revert:")
	for id, bn := range api.snapshots {
		hardhatLogger.Debugf("EvmAPI.Revert():   - ID=%s -> block=%d", id, bn)
	}

	hardhatLogger.Debugf("EvmAPI.Revert() calling LightKVS.RevertToBlock(%d)", blockNumber)

	// Revert the LightKVS to the snapshot's block number
	if err := api.lightKVS.RevertToBlock(blockNumber); err != nil {
		hardhatLogger.Debugf("EvmAPI.Revert() LightKVS.RevertToBlock returned error: %v", err)
		return false, fmt.Errorf("failed to revert LightKVS to block %d: %w", blockNumber, err)
	}

	hardhatLogger.Debugf("EvmAPI.Revert() successfully reverted LightKVS to block %d", blockNumber)

	// Revert the Store database to the same block number
	hardhatLogger.Debugf("EvmAPI.Revert() reverting Store to block %d", blockNumber)
	if err := api.store.RevertToBlock(ctx, blockNumber); err != nil {
		hardhatLogger.Debugf("EvmAPI.Revert() Store.RevertToBlock returned error: %v", err)
		return false, fmt.Errorf("failed to revert Store to block %d: %w", blockNumber, err)
	}
	hardhatLogger.Debugf("EvmAPI.Revert() Store reverted successfully to block %d", blockNumber)

	// Remove snapshots created after this one
	removedSnapshots := []string{}
	for id, bn := range api.snapshots {
		if bn > blockNumber {
			removedSnapshots = append(removedSnapshots, fmt.Sprintf("%s(block %d)", id, bn))
			delete(api.snapshots, id)
		}
	}
	if len(removedSnapshots) > 0 {
		hardhatLogger.Debugf("EvmAPI.Revert() removed snapshots: %v", removedSnapshots)
	}

	hardhatLogger.Debugf("EvmAPI.Revert() all snapshots after revert: %v", api.snapshots)
	hardhatLogger.Debugf("EvmAPI.Revert() returning: true (reverted to block %d)", blockNumber)
	return true, nil
}

// Mine mines a new block (evm_mine).
// This is a stub that returns success. In fabric-evm, blocks are created
// by the Fabric consensus mechanism, not by mining.
func (api *EvmAPI) Mine(ctx context.Context) (string, error) {
	hardhatLogger.Debugf("EvmAPI.Mine() called")
	// Stub: In fabric-evm, blocks are created by Fabric consensus
	// Return success to allow tests to proceed
	hardhatLogger.Debugf("EvmAPI.Mine() returning: 0x0")
	return "0x0", nil
}

// IncreaseTime increases the timestamp of the next block (evm_increaseTime).
// This is a stub that returns the time increase amount.
func (api *EvmAPI) IncreaseTime(ctx context.Context, seconds hexutil.Uint64) (hexutil.Uint64, error) {
	hardhatLogger.Debugf("EvmAPI.IncreaseTime() called with seconds=%d", seconds)
	// Stub: return the requested time increase
	// In a full implementation, this would affect the timestamp of the next block
	hardhatLogger.Debugf("EvmAPI.IncreaseTime() returning: %d", seconds)
	return seconds, nil
}

// SetNextBlockTimestamp sets the timestamp of the next block (evm_setNextBlockTimestamp).
// This is a stub that returns success.
func (api *EvmAPI) SetNextBlockTimestamp(ctx context.Context, timestamp hexutil.Uint64) (bool, error) {
	hardhatLogger.Debugf("EvmAPI.SetNextBlockTimestamp() called with timestamp=%d", timestamp)
	// Stub: return success
	// In a full implementation, this would set the timestamp for the next block
	hardhatLogger.Debugf("EvmAPI.SetNextBlockTimestamp() returning: true")
	return true, nil
}
