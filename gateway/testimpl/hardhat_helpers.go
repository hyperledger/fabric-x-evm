/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later

WARNING: This package contains test-only/unsafe RPC implementations.
DO NOT use in production environments.
*/

package testimpl

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	estorage "github.com/hyperledger/fabric-x-evm/endorser/storage"
	gwapi "github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/storage"
	"github.com/hyperledger/fabric-x-evm/gateway/testimpl/primer"
)

var hardhatLogger = flogging.MustGetLogger("gateway.testimpl.hardhat")

// primeCommitTimeout bounds how long a state-changing hardhat RPC waits for its
// priming transaction to become observable through the gateway's read path.
const primeCommitTimeout = 30 * time.Second

// primePollInterval is how often that wait re-reads the value.
const primePollInterval = 50 * time.Millisecond

// HardhatAPI provides Hardhat-specific RPC methods for testing.
// Most methods are stubs that let Hardhat tests run. The three state-changing ones
// (setBalance, setCode, setStorageAt) are real: each commits a priming transaction
// via StatePrimer, which endorses locally with every endorser's key and submits
// through the normal commit flow. The endorsers themselves are never asked to
// execute anything, so this works unchanged against remote/served endorsers.
//
// SECURITY WARNING: These methods are for testing only and should NEVER
// be enabled in production environments.
type HardhatAPI struct {
	// primer is nil when the server was built without WithStatePrimer; the three
	// state-changing methods then refuse rather than silently doing nothing.
	primer  *primer.StatePrimer
	backend gwapi.Backend

	// StatePrimer wraps a single mutable StateDB, so one priming transaction must
	// finish before the next resets it.
	primeMu sync.Mutex
}

// NewHardhatAPI creates a new Hardhat API instance whose state-changing methods are
// stubs. Use NewHardhatStateAPI to back them with a real state primer.
func NewHardhatAPI() *HardhatAPI {
	return &HardhatAPI{}
}

// NewHardhatStateAPI creates a Hardhat API whose setBalance/setCode/setStorageAt
// commit real priming transactions through primer, confirming each change against
// backend before returning.
func NewHardhatStateAPI(sp *primer.StatePrimer, backend gwapi.Backend) *HardhatAPI {
	return &HardhatAPI{primer: sp, backend: backend}
}

// prime resets the primer, lets apply queue its writes, commits the resulting
// priming transaction, and returns once confirm agrees the change is readable.
// what names the operation in error messages.
//
// It short-circuits when confirm already holds: Hardhat replays the same value
// often, and an empty read-write set is not worth a round through consensus.
func (api *HardhatAPI) prime(
	ctx context.Context,
	what string,
	apply func(*primer.StatePrimer),
	confirm func(context.Context) (bool, error),
) error {
	if api.primer == nil {
		return fmt.Errorf("%s: test RPC server was built without a state primer", what)
	}

	if ok, err := confirm(ctx); err == nil && ok {
		return nil
	}

	api.primeMu.Lock()
	defer api.primeMu.Unlock()

	// Reset discards anything a previous call left queued, so this transaction
	// carries only the write we are about to make.
	if _, err := api.primer.Reset(); err != nil {
		return fmt.Errorf("%s: reset primer: %w", what, err)
	}
	apply(api.primer)

	if err := api.primer.Commit(ctx, true); err != nil {
		return fmt.Errorf("%s: commit priming transaction: %w", what, err)
	}

	// Commit returns once the priming tx is no longer pending, but the gateway's
	// state reads can still trail it by a block; poll until they agree.
	waitCtx, cancel := context.WithTimeout(ctx, primeCommitTimeout)
	defer cancel()
	ticker := time.NewTicker(primePollInterval)
	defer ticker.Stop()

	for {
		if ok, err := confirm(waitCtx); err == nil && ok {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("%s: change did not become observable: %w", what, waitCtx.Err())
		case <-ticker.C:
		}
	}
}

// SetBalance sets an account's wei balance (hardhat_setBalance), blocking until the
// change is committed and reflected in reads of the account's balance.
func (api *HardhatAPI) SetBalance(ctx context.Context, address common.Address, balance hexutil.Big) error {
	target := (*big.Int)(&balance)
	hardhatLogger.Debugf("HardhatAPI.SetBalance() called with address=%s, balance=%s", address.Hex(), target.String())

	return api.prime(ctx, "setBalance",
		func(p *primer.StatePrimer) { p.SetBalance(address, target) },
		func(ctx context.Context) (bool, error) {
			got, err := api.backend.BalanceAt(ctx, address, nil)
			return err == nil && got.Cmp(target) == 0, err
		})
}

// SetCode sets an account's code (hardhat_setCode), blocking until the change is
// committed and reflected in reads of the account's code. Empty code clears it.
//
// CreateAccount is deliberately not called: it blind-writes a zero balance and
// nonce, and a code write alone already makes the account exist.
func (api *HardhatAPI) SetCode(ctx context.Context, address common.Address, code hexutil.Bytes) (bool, error) {
	hardhatLogger.Debugf("HardhatAPI.SetCode() called with address=%s, code length=%d", address.Hex(), len(code))

	if err := api.prime(ctx, "setCode",
		func(p *primer.StatePrimer) { p.SetCode(address, code) },
		func(ctx context.Context) (bool, error) {
			got, err := api.backend.CodeAt(ctx, address, nil)
			return err == nil && bytes.Equal(got, code), err
		}); err != nil {
		return false, err
	}
	return true, nil
}

// SetStorageAt sets an account's storage slot (hardhat_setStorageAt), blocking until
// the change is committed and reflected in reads of the slot. Hardhat sends slot and
// value as quantity-style hex strings, so both are decoded like eth_getStorageAt.
func (api *HardhatAPI) SetStorageAt(ctx context.Context, address common.Address, slot string, value string) (bool, error) {
	hardhatLogger.Debugf("HardhatAPI.SetStorageAt() called with address=%s, slot=%s, value=%s", address.Hex(), slot, value)

	key, err := gwapi.DecodeStorageWord("storage key", slot)
	if err != nil {
		return false, err
	}
	val, err := gwapi.DecodeStorageWord("storage value", value)
	if err != nil {
		return false, err
	}

	if err := api.prime(ctx, "setStorageAt",
		func(p *primer.StatePrimer) {
			p.SetStorage(address, map[common.Hash]common.Hash{key: val})
		},
		func(ctx context.Context) (bool, error) {
			got, err := api.backend.StorageAt(ctx, address, key, nil)
			return err == nil && common.BytesToHash(got) == val, err
		}); err != nil {
		return false, err
	}
	return true, nil
}

// Mine accepts hardhat_mine for RPC compatibility with Hardhat tests.
// It does not advance chain state: fabric-evm creates blocks via Fabric
// consensus, not mining. Optional blocks/interval params match Hardhat's
// signature and are ignored.
func (api *HardhatAPI) Mine(ctx context.Context, blocks *hexutil.Uint64, interval *hexutil.Uint64) error {
	hardhatLogger.Debugf("HardhatAPI.Mine() called blocks=%v interval=%v (stub; no state change)", blocks, interval)
	return nil
}

// ImpersonateAccount accepts hardhat_impersonateAccount for RPC compatibility.
// It does not enable signing or sending as the given address.
func (api *HardhatAPI) ImpersonateAccount(ctx context.Context, address common.Address) error {
	hardhatLogger.Debugf("HardhatAPI.ImpersonateAccount() called address=%s (stub; no state change)", address.Hex())
	return nil
}

// StopImpersonatingAccount accepts hardhat_stopImpersonatingAccount for RPC
// compatibility. It is a no-op companion to ImpersonateAccount.
func (api *HardhatAPI) StopImpersonatingAccount(ctx context.Context, address common.Address) error {
	hardhatLogger.Debugf("HardhatAPI.StopImpersonatingAccount() called address=%s (stub; no state change)", address.Hex())
	return nil
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
	// Drained before a snapshot or revert touches the ledger; see txFence.
	fence *txFence
	// Map snapshot IDs (hex strings) to block numbers
	snapshots map[string]uint64
}

// NewEvmAPI creates a new EVM API instance with LightKVS and Store for state management.
func NewEvmAPI(lightKVS estorage.Revertible, store storage.Revertible, fence *txFence) *EvmAPI {
	return &EvmAPI{
		lightKVS:  lightKVS,
		store:     store,
		fence:     fence,
		snapshots: make(map[string]uint64),
	}
}

// Snapshot creates a snapshot of the current state (evm_snapshot).
// Returns the current block number as the snapshot ID.
// Snapshots both the LightKVS state and the Store database.
func (api *EvmAPI) Snapshot(ctx context.Context) (string, error) {
	hardhatLogger.Debugf("EvmAPI.Snapshot() called")
	// mu first, so only one rewind is ever draining the fence at a time.
	api.mu.Lock()
	defer api.mu.Unlock()

	// Wait out anything still in flight, so the block number recorded below is
	// one the ledger has actually settled on.
	if err := api.fence.beginRewind(ctx); err != nil {
		hardhatLogger.Debugf("EvmAPI.Snapshot() returning error: %v", err)
		return "", err
	}
	defer api.fence.endRewind()

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
	// mu first, so only one rewind is ever draining the fence at a time.
	api.mu.Lock()
	defer api.mu.Unlock()

	// Waits out transactions already accepted and holds off new ones; see
	// txFence. Waiting on the gateway alone suffices only because handlers run
	// [endorser KVS, chain, gateway] on one synchronizer (see buildApp), so a
	// transaction the gateway calls committed is already in the endorser's
	// state. Giving the endorser its own synchronizer would break that.
	if err := api.fence.beginRewind(ctx); err != nil {
		hardhatLogger.Debugf("EvmAPI.Revert() returning error: %v", err)
		return false, err
	}
	defer api.fence.endRewind()

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

// SetAutomine accepts evm_setAutomine for RPC compatibility with Hardhat tests.
// It does not change mining mode: block production is driven by Fabric consensus.
func (api *EvmAPI) SetAutomine(ctx context.Context, enabled bool) error {
	hardhatLogger.Debugf("EvmAPI.SetAutomine() called enabled=%v (stub; no state change)", enabled)
	return nil
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
