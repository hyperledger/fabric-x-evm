/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"testing"

	"github.com/hyperledger/fabric-x-sdk/state"
)

// TestVersionedDBWrapper_NewSnapshotNilVsZero checks that nil means latest and
// an explicit 0 is not rewritten to the tip (issue #293).
func TestVersionedDBWrapper_NewSnapshotNilVsZero(t *testing.T) {
	db, err := state.NewWriteDB("ch", "file:vdb_snap_nilzero?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	w := NewVersionedDBWrapper(db)

	latestH, err := db.BlockNumber(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	rNil, err := w.NewSnapshot(nil)
	if err != nil {
		t.Fatalf("nil: %v", err)
	}
	defer rNil.Close()
	if got := rNil.(*VersionedDBSnapshot).blockNumber; got != latestH {
		t.Fatalf("nil resolved to %d, want latest %d", got, latestH)
	}

	r0, err := w.NewSnapshot(BlockAt(0))
	if err != nil {
		t.Fatalf("block 0: %v", err)
	}
	defer r0.Close()
	if got := r0.(*VersionedDBSnapshot).blockNumber; got != 0 {
		t.Fatalf("block 0 resolved to %d, want 0", got)
	}
}
