/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package execution

import (
	"context"

	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/state"
)

// testVersionedDBSnapshotter adapts a *state.VersionedDB to KVSSnapshotter for tests
// in this package. It intentionally duplicates storage.VersionedDBWrapper's small
// adapter logic rather than importing the storage package, which itself depends on
// execution — tests exercise the ports this package defines, not a concrete backend.
type testVersionedDBSnapshotter struct {
	db *state.VersionedDB
}

func (w *testVersionedDBSnapshotter) NewSnapshot(blockNumber uint64) (ReadStore, error) {
	if blockNumber == 0 {
		latest, err := w.db.BlockNumber(context.Background())
		if err != nil {
			return nil, err
		}
		blockNumber = latest
	}
	return &testVersionedDBReader{db: w.db, blockNumber: blockNumber}, nil
}

type testVersionedDBReader struct {
	db          *state.VersionedDB
	blockNumber uint64
}

func (r *testVersionedDBReader) Get(namespace, key string) (*blocks.WriteRecord, error) {
	return r.db.Get(namespace, key, r.blockNumber)
}

func (r *testVersionedDBReader) Close() error { return nil }
