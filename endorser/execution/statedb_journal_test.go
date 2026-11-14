/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package execution

import (
	"fmt"
	"math/big"
	"math/rand/v2"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/hyperledger/fabric-x-sdk/state"
	"github.com/stretchr/testify/require"
)

// Journal lookups must return the latest write of a key, also after a revert truncated the journal.
func TestJournalLookupAfterRevert(t *testing.T) {
	backend, err := state.NewWriteDB(Channel, "file:journalidx?mode=memory&cache=shared")
	require.NoError(t, err)
	db := snapshotDB(t, backend, 0)
	addr := newAddress()
	k1, k2 := common.HexToHash("0x01"), common.HexToHash("0x02")

	db.SetState(addr, k1, common.HexToHash("0x0a"))
	snap := db.Snapshot()
	db.SetState(addr, k1, common.HexToHash("0x0b"))
	db.SetState(addr, k2, common.HexToHash("0x0c"))
	require.Equal(t, common.HexToHash("0x0b"), db.GetState(addr, k1))

	db.RevertToSnapshot(snap)
	require.Equal(t, common.HexToHash("0x0a"), db.GetState(addr, k1), "write before the snapshot is visible again")
	require.Equal(t, common.Hash{}, db.GetState(addr, k2), "write after the snapshot is gone")

	db.SetState(addr, k1, common.HexToHash("0x0d"))
	require.Equal(t, common.HexToHash("0x0d"), db.GetState(addr, k1))
}

// Writing many slots into one StateDB (e.g. priming a genesis state) used to scan the whole journal on
// every write.
func BenchmarkSetStateManySlots(b *testing.B) {
	for i := range b.N {
		backend, err := state.NewWriteDB(Channel, fmt.Sprintf("file:journalbench%d?mode=memory&cache=shared", i))
		require.NoError(b, err)
		var blockNum uint64
		snapshot, err := (&testVersionedDBSnapshotter{db: backend}).NewSnapshot(&blockNum)
		require.NoError(b, err)
		db, err := NewStateDB(b.Context(), snapshot, Namespace, false)
		require.NoError(b, err)
		addr := newAddress()
		for s := range 20_000 {
			db.SetState(addr, common.BigToHash(big.NewInt(int64(s))), common.Hash{1})
		}
		snapshot.Close()
	}
}

// The index must agree with a backward scan of the journal after any mix of
// writes, deletes, snapshots and reverts, including reverts that skip snapshots.
func TestJournalIndexMatchesScanAcrossReverts(t *testing.T) {
	db, err := NewStateDB(t.Context(), nil, Namespace, false) // putState and lookups never read the store
	require.NoError(t, err)

	scan := func(key string) ([]byte, bool) {
		for i := len(db.journal) - 1; i >= 0; i-- {
			if w, ok := db.journal[i].(writeEntry); ok && w.write.Key == key {
				if w.write.IsDelete {
					return nil, true
				}
				return w.write.Value, true
			}
		}
		return nil, false
	}

	rng := rand.New(rand.NewPCG(1, 2))
	keys := []string{"a", "b", "c", "d"}
	var snaps []int
	for step := range 5_000 {
		switch op := rng.IntN(10); {
		case op < 6:
			var value []byte // nil: a delete
			if rng.IntN(4) > 0 {
				value = []byte{byte(step), byte(step >> 8)}
			}
			db.putState(keys[rng.IntN(len(keys))], value)
		case op < 8:
			snaps = append(snaps, db.Snapshot())
		case len(snaps) > 0:
			n := rng.IntN(len(snaps))
			db.RevertToSnapshot(snaps[n])
			snaps = snaps[:n]
		}
		for _, key := range keys {
			gotVal, gotOK := db.getStateFromJournal(key)
			wantVal, wantOK := scan(key)
			require.Equal(t, wantOK, gotOK, "step %d key %s: found", step, key)
			require.Equal(t, wantVal, gotVal, "step %d key %s: value", step, key)
		}
	}
}

// A transaction with many calls that each write and then revert (static_call50000
// in the execution-specs suite) used to rebuild the whole index on every revert.
func BenchmarkRevertManyCalls(b *testing.B) {
	for range b.N {
		db, err := NewStateDB(b.Context(), nil, Namespace, false)
		require.NoError(b, err)
		for s := range 1_000 {
			db.putState(fmt.Sprintf("base%d", s), []byte{1})
		}
		for range 20_000 {
			snap := db.Snapshot()
			db.putState("callee", []byte{2})
			db.RevertToSnapshot(snap)
			db.getStateFromJournal("base0")
		}
	}
}
