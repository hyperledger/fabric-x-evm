/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-evm/gateway/storage"
)

var storeLogger = flogging.MustGetLogger("gateway.testimpl.store")

const snapshotRingBufferSize = 128

type snapshot struct {
	BlockNumber uint64
	DbPath      string // path to the snapshot database file
}

// SnapshotStore extends storage.Store with snapshot and revert capabilities
type SnapshotStore struct {
	*storage.Store
	SnapshotsMu   sync.RWMutex
	Snapshots     [snapshotRingBufferSize]*snapshot
	AttachCounter atomic.Uint64 // counter for unique ATTACH DATABASE aliases
}

// NewSnapshotStore creates a new SnapshotStore that wraps a storage.Store
func NewSnapshotStore(store *storage.Store) *SnapshotStore {
	return &SnapshotStore{
		Store: store,
	}
}

// Snapshot creates a backup of the current database state at the current block number.
// The snapshot is stored in a ring buffer indexed by block number modulo 128.
// Uses ATTACH DATABASE to create a separate database file for the snapshot.
func (s *SnapshotStore) Snapshot(ctx context.Context) (uint64, error) {
	currentBlockNumber := s.CachedBlockNumber.Load()
	storeLogger.Debugf("Starting snapshot for block %d", currentBlockNumber)

	// Use a unique ID for this snapshot to avoid conflicts
	attachID := s.AttachCounter.Add(1)

	// Create a temporary file for the snapshot with a unique name
	// This ensures multiple snapshots at the same block number don't overwrite each other
	tmpDir := os.TempDir()
	snapshotPath := filepath.Join(tmpDir, fmt.Sprintf("fabric-evm-snapshot-blk%d-id%d.db", currentBlockNumber, attachID))
	storeLogger.Debugf("Snapshot path: %s (attachID=%d)", snapshotPath, attachID)

	// Remove the file if it exists (shouldn't happen with unique IDs, but just in case)
	os.Remove(snapshotPath)

	snapshotAlias := fmt.Sprintf("snap_%d", attachID)
	storeLogger.Debugf("Using alias: %s", snapshotAlias)

	// Get a dedicated connection for this operation
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		storeLogger.Debugf("Failed to get connection: %v", err)
		return 0, fmt.Errorf("failed to get database connection: %w", err)
	}
	defer conn.Close()

	// Use ATTACH DATABASE and copy all tables to the snapshot
	// This works reliably with shared memory databases
	attachSQL := fmt.Sprintf("ATTACH DATABASE '%s' AS %s", snapshotPath, snapshotAlias)
	storeLogger.Debugf("Executing: %s", attachSQL)
	if _, err := conn.ExecContext(ctx, attachSQL); err != nil {
		storeLogger.Debugf("ATTACH failed: %v", err)
		return 0, fmt.Errorf("failed to attach snapshot database: %w", err)
	}

	// Copy schema and data to snapshot database
	// Execute each statement separately to ensure they all complete
	statements := []string{
		fmt.Sprintf("CREATE TABLE \"%s\".blocks AS SELECT * FROM main.blocks", snapshotAlias),
		fmt.Sprintf("CREATE TABLE \"%s\".transactions AS SELECT * FROM main.transactions", snapshotAlias),
		fmt.Sprintf("CREATE TABLE \"%s\".logs AS SELECT * FROM main.logs", snapshotAlias),
		fmt.Sprintf("CREATE INDEX \"%s\".idx_blocks_number ON blocks(block_number)", snapshotAlias),
		fmt.Sprintf("CREATE INDEX \"%s\".idx_blocks_hash ON blocks(block_hash)", snapshotAlias),
		fmt.Sprintf("CREATE INDEX \"%s\".idx_transactions_hash ON transactions(tx_hash)", snapshotAlias),
		fmt.Sprintf("CREATE INDEX \"%s\".idx_transactions_block_hash ON transactions(block_hash)", snapshotAlias),
		fmt.Sprintf("CREATE INDEX \"%s\".idx_transactions_block_number ON transactions(block_number)", snapshotAlias),
		fmt.Sprintf("CREATE INDEX \"%s\".idx_logs_block_number ON logs(block_number)", snapshotAlias),
		fmt.Sprintf("CREATE INDEX \"%s\".idx_logs_tx_hash ON logs(tx_hash)", snapshotAlias),
		fmt.Sprintf("CREATE INDEX \"%s\".idx_logs_address ON logs(address)", snapshotAlias),
		fmt.Sprintf("CREATE INDEX \"%s\".idx_logs_topic0 ON logs(topic0)", snapshotAlias),
	}

	for i, stmt := range statements {
		storeLogger.Debugf("Executing statement %d: %s", i+1, stmt)
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			storeLogger.Debugf("Statement %d failed: %v", i+1, err)
			// Detach before returning on error
			detachSQL := fmt.Sprintf("DETACH DATABASE %s", snapshotAlias)
			conn.ExecContext(ctx, detachSQL)
			os.Remove(snapshotPath)
			return 0, fmt.Errorf("failed to execute snapshot statement: %w", err)
		}
	}

	storeLogger.Debugf("All statements executed successfully")

	// Explicitly detach the database now that we're done
	detachSQL := fmt.Sprintf("DETACH DATABASE %s", snapshotAlias)
	storeLogger.Debugf("Executing: %s", detachSQL)
	if _, err := conn.ExecContext(ctx, detachSQL); err != nil {
		storeLogger.Debugf("DETACH failed (non-fatal): %v", err)
	}

	// Close the connection to ensure the detach takes effect
	storeLogger.Debugf("Closing connection")
	conn.Close()

	// Store snapshot in ring buffer
	s.SnapshotsMu.Lock()
	index := currentBlockNumber % snapshotRingBufferSize
	// Clean up old snapshot if it exists
	if s.Snapshots[index] != nil {
		storeLogger.Debugf("Cleaning up old snapshot at index %d: %s", index, s.Snapshots[index].DbPath)
		os.Remove(s.Snapshots[index].DbPath)
	}
	s.Snapshots[index] = &snapshot{
		BlockNumber: currentBlockNumber,
		DbPath:      snapshotPath,
	}
	storeLogger.Debugf("Stored snapshot at index %d for block %d", index, currentBlockNumber)
	s.SnapshotsMu.Unlock()

	storeLogger.Debugf("Snapshot complete for block %d", currentBlockNumber)
	return currentBlockNumber, nil
}

// RevertToSnapshot restores the database to a previously saved snapshot at the given block number.
// It also restores the cached block number.
// Uses ATTACH DATABASE and SQL to restore data, which works reliably with shared memory databases.
func (s *SnapshotStore) RevertToSnapshot(ctx context.Context, blockNumber uint64) error {
	storeLogger.Debugf("Starting restore to block %d", blockNumber)

	// Find the snapshot
	s.SnapshotsMu.RLock()
	index := blockNumber % snapshotRingBufferSize
	snap := s.Snapshots[index]
	s.SnapshotsMu.RUnlock()

	if snap == nil || snap.BlockNumber != blockNumber {
		storeLogger.Debugf("Snapshot not found: snap=%v, index=%d", snap, index)
		return fmt.Errorf("snapshot for block %d not found", blockNumber)
	}

	storeLogger.Debugf("Found snapshot: path=%s, blockNumber=%d", snap.DbPath, snap.BlockNumber)

	// Check if snapshot file exists
	if _, err := os.Stat(snap.DbPath); err != nil {
		storeLogger.Debugf("Snapshot file does not exist: %v", err)
		return fmt.Errorf("snapshot file does not exist: %w", err)
	}

	// Use a unique alias for this restore to avoid conflicts
	attachID := s.AttachCounter.Add(1)
	restoreAlias := fmt.Sprintf("restore_%d", attachID)
	storeLogger.Debugf("Using alias: %s (attachID=%d)", restoreAlias, attachID)

	// Use a transaction to ensure atomicity
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		storeLogger.Debugf("Failed to begin transaction: %v", err)
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Attach the snapshot database
	attachSQL := fmt.Sprintf("ATTACH DATABASE '%s' AS %s", snap.DbPath, restoreAlias)
	storeLogger.Debugf("Executing: %s", attachSQL)
	if _, err := tx.ExecContext(ctx, attachSQL); err != nil {
		storeLogger.Debugf("ATTACH failed: %v", err)
		return fmt.Errorf("failed to attach snapshot database: %w", err)
	}

	// Check what tables exist in the attached database
	checkSQL := fmt.Sprintf("SELECT name FROM \"%s\".sqlite_master WHERE type='table'", restoreAlias)
	storeLogger.Debugf("Checking tables: %s", checkSQL)
	rows, err := tx.QueryContext(ctx, checkSQL)
	if err != nil {
		storeLogger.Debugf("Failed to query tables: %v", err)
	} else {
		storeLogger.Debugf("Tables in snapshot:")
		for rows.Next() {
			var tableName string
			rows.Scan(&tableName)
			storeLogger.Debugf("  - %s", tableName)
		}
		rows.Close()
	}

	// Delete all data from main database tables
	storeLogger.Debugf("Deleting data from main.logs")
	if _, err := tx.ExecContext(ctx, "DELETE FROM main.logs"); err != nil {
		storeLogger.Debugf("Failed to delete logs: %v", err)
		return fmt.Errorf("failed to delete logs: %w", err)
	}
	storeLogger.Debugf("Deleting data from main.transactions")
	if _, err := tx.ExecContext(ctx, "DELETE FROM main.transactions"); err != nil {
		storeLogger.Debugf("Failed to delete transactions: %v", err)
		return fmt.Errorf("failed to delete transactions: %w", err)
	}
	storeLogger.Debugf("Deleting data from main.blocks")
	if _, err := tx.ExecContext(ctx, "DELETE FROM main.blocks"); err != nil {
		storeLogger.Debugf("Failed to delete blocks: %v", err)
		return fmt.Errorf("failed to delete blocks: %w", err)
	}

	// Copy data from snapshot to main database
	// Note: Use quotes around the alias to handle it as an identifier
	insertBlocksSQL := fmt.Sprintf("INSERT INTO main.blocks SELECT * FROM \"%s\".blocks", restoreAlias)
	storeLogger.Debugf("Executing: %s", insertBlocksSQL)
	if _, err := tx.ExecContext(ctx, insertBlocksSQL); err != nil {
		storeLogger.Debugf("Failed to restore blocks: %v", err)
		return fmt.Errorf("failed to restore blocks: %w", err)
	}

	insertTxSQL := fmt.Sprintf("INSERT INTO main.transactions SELECT * FROM \"%s\".transactions", restoreAlias)
	storeLogger.Debugf("Executing: %s", insertTxSQL)
	if _, err := tx.ExecContext(ctx, insertTxSQL); err != nil {
		storeLogger.Debugf("Failed to restore transactions: %v", err)
		return fmt.Errorf("failed to restore transactions: %w", err)
	}

	insertLogsSQL := fmt.Sprintf("INSERT INTO main.logs SELECT * FROM \"%s\".logs", restoreAlias)
	storeLogger.Debugf("Executing: %s", insertLogsSQL)
	if _, err := tx.ExecContext(ctx, insertLogsSQL); err != nil {
		storeLogger.Debugf("Failed to restore logs: %v", err)
		return fmt.Errorf("failed to restore logs: %w", err)
	}

	// Commit the transaction first (must commit before detaching)
	storeLogger.Debugf("Committing transaction")
	if err := tx.Commit(); err != nil {
		storeLogger.Debugf("Failed to commit: %v", err)
		return fmt.Errorf("failed to commit restore transaction: %w", err)
	}

	// Now detach the database after the transaction is committed
	// Use the main db connection, not the transaction
	detachSQL := fmt.Sprintf("DETACH DATABASE %s", restoreAlias)
	storeLogger.Debugf("Executing: %s", detachSQL)
	if _, err := s.DB.ExecContext(ctx, detachSQL); err != nil {
		storeLogger.Debugf("DETACH failed (non-fatal): %v", err)
	}

	// Restore the cached block number
	s.CachedBlockNumber.Store(blockNumber)
	storeLogger.Debugf("Restore complete to block %d", blockNumber)

	return nil
}
