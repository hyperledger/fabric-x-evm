/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/stretchr/testify/require"
)

// fakeGate parks every transaction it is given and counts what it sees.
type fakeGate struct {
	parked   map[common.Hash]*types.Transaction
	observed int
}

func (f *fakeGate) Admit(_ context.Context, tx *types.Transaction) error {
	f.parked[tx.Hash()] = tx
	return nil
}
func (f *fakeGate) Observe([]domain.Transaction)               { f.observed++ }
func (f *fakeGate) IsPending(h common.Hash) *types.Transaction { return f.parked[h] }

func TestResettableGate_ResetInstallsAFreshGate(t *testing.T) {
	var built []*fakeGate
	gate := NewResettableGate()
	gate.Bind(func() core.NonceSequencer {
		f := &fakeGate{parked: map[common.Hash]*types.Transaction{}}
		built = append(built, f)
		return f
	})
	tx := types.NewTx(&types.LegacyTx{Nonce: 3})

	require.NoError(t, gate.Admit(context.Background(), tx))
	gate.Observe(nil)
	require.Equal(t, tx.Hash(), gate.IsPending(tx.Hash()).Hash(), "calls reach the current gate")
	require.Equal(t, 1, built[0].observed)

	gate.ResetNonces()

	require.Len(t, built, 2)
	require.Nil(t, gate.IsPending(tx.Hash()), "what the old gate held is gone")
	gate.Observe(nil)
	require.Equal(t, 1, built[0].observed, "the old gate is no longer called")
	require.Equal(t, 1, built[1].observed)
}
