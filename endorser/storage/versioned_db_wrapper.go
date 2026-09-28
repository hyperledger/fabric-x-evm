/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"context"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/state"
)

var vdbLogger = flogging.MustGetLogger("endorser.storage.versioned_db")

// VersionedDBWrapper wraps a VersionedDB to implement execution.KVSSnapshotter.
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

// NewSnapshot creates a new snapshot of the state at the specified block number.
// It returns a VersionedDBSnapshot that will use this block number for all Get operations,
// providing snapshot isolation. nil means latest; a non-nil value is that exact height
// (including 0 for genesis).
func (w *VersionedDBWrapper) NewSnapshot(blockNumber *uint64) (execution.ReadStore, error) {
	var bn uint64
	if blockNumber == nil {
		latest, err := w.BlockNumber(context.Background())
		if err != nil {
			return nil, err
		}
		bn = latest
	} else {
		bn = *blockNumber
	}
	return &VersionedDBSnapshot{
		db:          w.db,
		blockNumber: bn,
	}, nil
}

// VersionedDBSnapshot represents a point-in-time snapshot of the VersionedDB.
// All Get operations will read state as of the snapshot's block number.
// It implements the execution.ReadStore interface required by StateDB.
type VersionedDBSnapshot struct {
	db          *state.VersionedDB
	blockNumber uint64
}

// Get retrieves the value for a key as of the snapshot's block number.
// This implements the execution.ReadStore interface with the signature:
// Get(namespace, key string) (*blocks.WriteRecord, error)
//
// The snapshot's block number is automatically appended as the lastBlock
// parameter when calling the underlying VersionedDB.Get method.
func (s *VersionedDBSnapshot) Get(namespace, key string) (*blocks.WriteRecord, error) {
	rec, err := s.db.Get(namespace, key, s.blockNumber)
	if err != nil {
		vdbLogger.Warnf("VersionedDBSnapshot.Get() ns=%s key=%s error: %v", namespace, key, err)
		return nil, err
	}
	if rec == nil {
		vdbLogger.Debugf("VersionedDBSnapshot.Get() ns=%s key=%s not found (block=%d)", namespace, key, s.blockNumber)
		return nil, nil
	}
	vdbLogger.Debugf("VersionedDBSnapshot.Get() ns=%s key=%s block=%d isDelete=%t valueLen=%d", namespace, key, rec.BlockNum, rec.IsDelete, len(rec.Value))
	return rec, nil
}

// Close is a no-op for VersionedDBSnapshot since VersionedDB doesn't
// require explicit snapshot cleanup. It's provided for interface compatibility.
func (s *VersionedDBSnapshot) Close() error {
	return nil
}

// Get implements blocks.RecordGetter, reading the latest committed state
func (w *VersionedDBWrapper) Get(namespace, key string) (*blocks.WriteRecord, error) {
	return w.db.GetCurrent(namespace, key)
}

// Handle implements blocks.BlockHandler by delegating to the underlying VersionedDB.
func (w *VersionedDBWrapper) Handle(ctx context.Context, b blocks.Block) error {
	return w.db.Handle(ctx, b)
}

// BlockNumber returns the last processed block number.
func (w *VersionedDBWrapper) BlockNumber(ctx context.Context) (uint64, error) {
	return w.db.BlockNumber(ctx)
}

// Close closes the underlying VersionedDB.
func (w *VersionedDBWrapper) Close() error {
	return w.db.Close()
}
