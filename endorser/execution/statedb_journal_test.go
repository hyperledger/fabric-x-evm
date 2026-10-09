/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package execution

import (
	"fmt"
	"math/big"
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
