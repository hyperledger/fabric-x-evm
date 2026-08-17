/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"context"
	"fmt"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
)

var storeLogger = flogging.MustGetLogger("gateway.storage.snapshot_store")

// Revertible is implemented by stores that support interactive snapshot/rollback —
// e.g. the Hardhat evm_snapshot/evm_revert RPCs on a single-node testnode. The
// production Store alone does not implement this; only a SnapshotStore wrapping one
// does, and only when test-RPC is explicitly enabled.
type Revertible interface {
	Snapshot(ctx context.Context) (uint64, error)
	RevertToBlock(ctx context.Context, blockNumber uint64) error
}

// SnapshotStore adds snapshot/revert to a Store by treating a revert as a
// truncation of the block history.
//
// Blocks, transactions and logs are append-only: nothing below the chain tip is
// ever rewritten. So the state at height N is fully described by "the rows at or
// below N", and returning to N means deleting everything above it. There is
// nothing to copy and nothing to keep.
//
// That matches evm_revert's own contract, which invalidates every snapshot taken
// after the target — history only ever moves backwards, never forwards again.
type SnapshotStore struct {
	*Store
}

// NewSnapshotStore creates a new SnapshotStore that wraps a Store
func NewSnapshotStore(store *Store) *SnapshotStore {
	return &SnapshotStore{Store: store}
}

// Snapshot names the current chain tip so a later RevertToBlock can come back to it.
func (s *SnapshotStore) Snapshot(ctx context.Context) (uint64, error) {
	blockNumber := s.CachedBlockNumber.Load()
	storeLogger.Debugf("Snapshot at block %d", blockNumber)
	return blockNumber, nil
}

// RevertToBlock drops every block above blockNumber, along with its transactions
// and logs, and moves the cached tip back.
func (s *SnapshotStore) RevertToBlock(ctx context.Context, blockNumber uint64) error {
	current := s.CachedBlockNumber.Load()
	storeLogger.Debugf("Reverting to block %d (current %d)", blockNumber, current)

	if blockNumber > current {
		return fmt.Errorf("cannot revert to block %d: chain tip is %d", blockNumber, current)
	}
	if blockNumber == current {
		return nil
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin revert transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	// Children first: transactions and logs both reference blocks(block_number)
	// and the connection runs with foreign_keys=ON.
	for _, stmt := range []string{
		"DELETE FROM logs WHERE block_number > ?",
		"DELETE FROM transactions WHERE block_number > ?",
		"DELETE FROM blocks WHERE block_number > ?",
	} {
		if _, err := tx.ExecContext(ctx, stmt, blockNumber); err != nil {
			return fmt.Errorf("failed to revert to block %d (%q): %w", blockNumber, stmt, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit revert to block %d: %w", blockNumber, err)
	}

	// Only after the delete commits, so a failed revert leaves the cached tip
	// agreeing with what is still in the database.
	s.CachedBlockNumber.Store(blockNumber)
	storeLogger.Debugf("Reverted to block %d", blockNumber)
	return nil
}
