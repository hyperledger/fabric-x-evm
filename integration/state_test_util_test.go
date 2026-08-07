/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	_ "modernc.org/sqlite"
)

// Covers makePreStateWithDualState so the NewSnapshot(BlockAt(1)) call sites
// stay in the unit-test coverage profile (Codecov patch for #293).
func TestMakePreStateWithDualState(t *testing.T) {
	addr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	st := makePreStateWithDualState(
		rawdb.NewMemoryDatabase(),
		types.GenesisAlloc{
			addr: {Balance: big.NewInt(1), Nonce: 0},
		},
		false,
		rawdb.HashScheme,
	)
	if st.StateDB == nil {
		t.Fatal("expected non-nil StateDB")
	}
	t.Cleanup(st.Close)
}
