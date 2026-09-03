/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"context"

	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/state"
)

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
	// Use the VersionedDB's Get method with the snapshot's block number
	return s.db.Get(namespace, key, s.blockNumber)
}

// Close is a no-op for VersionedDBSnapshot since VersionedDB doesn't
// require explicit snapshot cleanup. It's provided for interface compatibility.
func (s *VersionedDBSnapshot) Close() error {
	return nil
}

// Get retrieves the value for a key as of the given block number, honoring the
// KVS convention that lastBlock 0 means "latest" (see blockRefFromLastBlock) —
// the same route LightKVS.Get and PebbleKVS.Get take. Passing 0 straight
// through to VersionedDB.Get would instead match `version_block <= 0`, so
// every latest-read would come back empty.
func (w *VersionedDBWrapper) Get(namespace, key string, lastBlock uint64) (*blocks.WriteRecord, error) {
	r, err := w.NewSnapshot(blockRefFromLastBlock(lastBlock))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.Get(namespace, key)
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
