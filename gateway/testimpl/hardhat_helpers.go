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
)

// HardhatAPI provides Hardhat-specific RPC methods for testing.
// These are stub implementations that allow Hardhat tests to run but don't
// actually implement the full functionality.
//
// SECURITY WARNING: These methods are for testing only and should NEVER
// be enabled in production environments.
type HardhatAPI struct {
	mu sync.Mutex
}

// NewHardhatAPI creates a new Hardhat API instance.
func NewHardhatAPI() *HardhatAPI {
	return &HardhatAPI{}
}

// SetCode sets the code at a given address (hardhat_setCode).
// This is a stub implementation that returns success but doesn't actually modify state.
// TODO: Implement proper code modification if needed for specific test scenarios.
func (api *HardhatAPI) SetCode(ctx context.Context, address common.Address, code hexutil.Bytes) (bool, error) {
	// Stub: return success without actually modifying state
	// In a full implementation, this would modify the account's code in the state DB
	return true, nil
}

// EvmAPI provides EVM-specific RPC methods for testing, particularly snapshot/revert.
// These are stub implementations that allow tests to run but don't actually
// preserve or restore state.
//
// SECURITY WARNING: These methods are for testing only and should NEVER
// be enabled in production environments.
type EvmAPI struct {
	mu              sync.Mutex
	snapshotCounter uint64
}

// NewEvmAPI creates a new EVM API instance.
func NewEvmAPI() *EvmAPI {
	return &EvmAPI{
		snapshotCounter: 0,
	}
}

// Snapshot creates a snapshot of the current state (evm_snapshot).
// This is a stub implementation that returns a snapshot ID but doesn't actually
// preserve state.
// TODO: Implement proper state snapshotting using StateDB.Copy() for full functionality.
func (api *EvmAPI) Snapshot(ctx context.Context) (string, error) {
	api.mu.Lock()
	defer api.mu.Unlock()

	api.snapshotCounter++
	snapshotID := fmt.Sprintf("0x%x", api.snapshotCounter)

	// Stub: return snapshot ID without actually preserving state
	// In a full implementation, this would:
	// 1. Create a copy of the current StateDB using StateDB.Copy()
	// 2. Store it in a map with the snapshot ID
	// 3. Return the snapshot ID

	return snapshotID, nil
}

// Revert reverts the state to a previous snapshot (evm_revert).
// This is a stub implementation that returns success but doesn't actually
// restore state.
// TODO: Implement proper state restoration for full functionality.
func (api *EvmAPI) Revert(ctx context.Context, snapshotID string) (bool, error) {
	api.mu.Lock()
	defer api.mu.Unlock()

	// Stub: return success without actually restoring state
	// In a full implementation, this would:
	// 1. Look up the snapshot by ID
	// 2. Restore the StateDB from the snapshot
	// 3. Remove snapshots created after this one
	// 4. Return success

	return true, nil
}

// Mine mines a new block (evm_mine).
// This is a stub that returns success. In fabric-evm, blocks are created
// by the Fabric consensus mechanism, not by mining.
func (api *EvmAPI) Mine(ctx context.Context) (string, error) {
	// Stub: In fabric-evm, blocks are created by Fabric consensus
	// Return success to allow tests to proceed
	return "0x0", nil
}

// IncreaseTime increases the timestamp of the next block (evm_increaseTime).
// This is a stub that returns the time increase amount.
func (api *EvmAPI) IncreaseTime(ctx context.Context, seconds hexutil.Uint64) (hexutil.Uint64, error) {
	// Stub: return the requested time increase
	// In a full implementation, this would affect the timestamp of the next block
	return seconds, nil
}

// SetNextBlockTimestamp sets the timestamp of the next block (evm_setNextBlockTimestamp).
// This is a stub that returns success.
func (api *EvmAPI) SetNextBlockTimestamp(ctx context.Context, timestamp hexutil.Uint64) (bool, error) {
	// Stub: return success
	// In a full implementation, this would set the timestamp for the next block
	return true, nil
}
