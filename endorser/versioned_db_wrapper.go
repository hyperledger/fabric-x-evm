/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package endorser

import (
	"context"

	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/state"
)

// VersionedDBWrapper wraps a VersionedDB to implement KVSSnapshotter.
// It provides snapshot isolation by capturing the current block number
// when NewSnapshot() is called, and using that block number for all
// subsequent Get operations on the snapshot.
type VersionedDBWrapper struct {
	db *state.VersionedDB
}

// NewVersionedDBWrapper creates a new wrapper around a VersionedDB.
func NewVersionedDBWrapper(db *state.VersionedDB) *VersionedDBWrapper {
	return &VersionedDBWrapper{
		db: db,
	}
}

// NewSnapshot creates a new snapshot of the current state.
// It queries the current block number and returns a VersionedDBSnapshot
// that will use this block number for all Get operations, providing
// snapshot isolation.
func (w *VersionedDBWrapper) NewSnapshot() ReadStore {
	// Query the current block number to establish the snapshot point
	blockNum, err := w.db.BlockNumber(context.Background())
	if err != nil {
		// If we can't get the block number, default to 0 (latest)
		blockNum = 0
	}

	return &VersionedDBSnapshot{
		db:          w.db,
		blockNumber: blockNum,
	}
}

// VersionedDBSnapshot represents a point-in-time snapshot of the VersionedDB.
// All Get operations will read state as of the snapshot's block number.
// It implements the ReadStore interface required by StateDB.
type VersionedDBSnapshot struct {
	db          *state.VersionedDB
	blockNumber uint64
}

// Get retrieves the value for a key as of the snapshot's block number.
// This implements the ReadStore interface with the signature:
// Get(namespace, key string) (*blocks.WriteRecord, error)
//
// The snapshot's block number is automatically appended as the lastBlock
// parameter when calling the underlying VersionedDB.Get method.
func (s *VersionedDBSnapshot) Get(namespace, key string) (*blocks.WriteRecord, error) {
	// Use the VersionedDB's Get method with the snapshot's block number
	return s.db.Get(namespace, key, s.blockNumber)
}

// Close is a no-op for VersionedDBSnapshot since VersionedDB doesn't
// require explicit snapshot cleanup. It's provided for interface compatibility.
func (s *VersionedDBSnapshot) Close() error {
	return nil
}
