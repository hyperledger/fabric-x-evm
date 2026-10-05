/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package synchronizer

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nsTx(id string, namespaces ...string) blocks.Transaction {
	tx := blocks.Transaction{ID: id}
	for _, ns := range namespaces {
		tx.NsRWS = append(tx.NsRWS, blocks.NsReadWriteSet{
			Namespace: ns,
			RWS:       blocks.ReadWriteSet{Writes: []blocks.KVWrite{{Key: ns + "-key"}}},
		})
	}
	return tx
}

type recordingHandler struct {
	seen []blocks.Block
	err  error
}

func (r *recordingHandler) Handle(_ context.Context, b blocks.Block) error {
	r.seen = append(r.seen, b)
	return r.err
}

func TestNsFilter(t *testing.T) {
	first, second := &recordingHandler{}, &recordingHandler{}
	f := nsFilter{namespace: "evm", handlers: []blocks.BlockHandler{first, second}}

	b := blocks.Block{Number: 7, Transactions: []blocks.Transaction{
		nsTx("a", "evm"), nsTx("b", "other"), nsTx("c"), nsTx("d", "other", "evm"),
	}}
	require.NoError(t, f.Handle(context.Background(), b))

	for _, h := range []*recordingHandler{first, second} {
		require.Len(t, h.seen, 1)
		got := h.seen[0]
		assert.Equal(t, uint64(7), got.Number)
		require.Len(t, got.Transactions, 2)
		assert.Equal(t, "a", got.Transactions[0].ID)
		assert.Equal(t, "d", got.Transactions[1].ID)
		assert.Equal(t, []blocks.NsReadWriteSet{nsTx("", "evm").NsRWS[0]}, got.Transactions[1].NsRWS)
	}
	assert.Len(t, b.Transactions, 4, "input block must not be modified")
	assert.Len(t, b.Transactions[3].NsRWS, 2, "input transaction must not be modified")

	// A block with nothing of ours is still forwarded, empty.
	require.NoError(t, f.Handle(context.Background(), blocks.Block{Number: 8, Transactions: []blocks.Transaction{nsTx("e", "other")}}))
	require.Len(t, first.seen, 2)
	assert.Equal(t, uint64(8), first.seen[1].Number)
	assert.Empty(t, first.seen[1].Transactions)

	// An error stops the chain.
	first.err = errors.New("boom")
	require.Error(t, f.Handle(context.Background(), b))
	assert.Len(t, second.seen, 2)
}
