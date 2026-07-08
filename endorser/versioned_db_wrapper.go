/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package endorser

import (
	"context"

	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
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

// NewSnapshot creates a new snapshot of the state at the specified block number.
// It returns a VersionedDBSnapshot that will use this block number for all Get operations,
// providing snapshot isolation. If blockNumber is 0 it resolves to the latest committed block.
func (w *VersionedDBWrapper) NewSnapshot(blockNumber uint64) (ReadStore, error) {
	if blockNumber == 0 {
		latest, err := w.db.BlockNumber(context.Background())
		if err != nil {
			return nil, err
		}
		blockNumber = latest
	}
	return &VersionedDBSnapshot{
		db:          w.db,
		blockNumber: blockNumber,
	}, nil
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

// Get retrieves the value for a key as of the given block number.
func (w *VersionedDBWrapper) Get(namespace, key string, lastBlock uint64) (*blocks.WriteRecord, error) {
	return w.db.Get(namespace, key, lastBlock)
}

// Handle implements blocks.BlockHandler by delegating to the underlying VersionedDB.
func (w *VersionedDBWrapper) Handle(ctx context.Context, b blocks.Block) error {
	return w.db.Handle(ctx, b)
}

// HandleTx implements the core.TxHandler interface.
// It creates a synthetic block with the transactions and delegates to the underlying VersionedDB.
func (w *VersionedDBWrapper) HandleTx(ctx context.Context, notifs []core.TxNotification) error {
	if len(notifs) == 0 {
		return nil
	}

	// Group notifications by block number
	blockMap := make(map[uint64][]core.TxNotification)
	for _, notif := range notifs {
		blockMap[notif.BlockNum] = append(blockMap[notif.BlockNum], notif)
	}

	// Process each block
	for blockNum, blockNotifs := range blockMap {
		// Create a synthetic block with transactions from this block
		txs := make([]blocks.Transaction, 0, len(blockNotifs))
		for _, notif := range blockNotifs {
			txs = append(txs, blocks.Transaction{
				Number: int64(notif.TxNum),
				ID:     notif.FabricTxID,
				Valid:  notif.Status == committerpb.Status_COMMITTED, // Status = COMMITTED = valid
				NsRWS:  notif.NsRWS,
			})
		}

		syntheticBlock := blocks.Block{
			Number:       blockNum,
			Transactions: txs,
		}

		if err := w.db.Handle(ctx, syntheticBlock); err != nil {
			return err
		}
	}

	return nil
}

// BlockNumber returns the last processed block number.
func (w *VersionedDBWrapper) BlockNumber(ctx context.Context) (uint64, error) {
	return w.db.BlockNumber(ctx)
}

// Close closes the underlying VersionedDB.
func (w *VersionedDBWrapper) Close() error {
	return w.db.Close()
}
