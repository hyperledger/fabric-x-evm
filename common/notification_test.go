/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/notification"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubHandler captures every block delivered to it and lets tests inject
// an error to exercise the panic path.
type stubHandler struct {
	seen []blocks.Block
	err  error
}

func (s *stubHandler) Handle(_ context.Context, b blocks.Block) error {
	s.seen = append(s.seen, b)
	return s.err
}

// evmEvent builds a committed event as the SDK delivers it: the ChaincodeInput has
// already been decoded at the network boundary, so InputArgs holds the proposal-type
// byte in Args[0] and the raw ethereum-tx bytes (opaque to the dispatcher) in Args[1].
func evmEvent(txID string, txNum int64, status blocks.Status, propType ProposalType, ethTxBytes []byte) notification.CommittedTxEvent {
	return notification.CommittedTxEvent{
		Transaction: blocks.Transaction{
			ID:        txID,
			Number:    txNum,
			Status:    status,
			InputArgs: [][]byte{{byte(propType)}, ethTxBytes},
		},
	}
}

// committedEVMEvent is evmEvent for the common case: a committed EVM transaction.
func committedEVMEvent(txID string) notification.CommittedTxEvent {
	return evmEvent(txID, 0, blocks.StatusCommitted, ProposalTypeEVMTx, []byte{0xaa})
}

// ---- NewAllTxBatchDispatcher ----

func TestNewAllTxBatchDispatcher_NoHandlers(t *testing.T) {
	d := NewAllTxBatchDispatcher()
	require.NotNil(t, d)
	assert.Empty(t, d.handlers)
}

func TestNewAllTxBatchDispatcher_MultipleHandlers(t *testing.T) {
	h1, h2 := &stubHandler{}, &stubHandler{}
	d := NewAllTxBatchDispatcher(h1, h2)
	require.NotNil(t, d)
	assert.Len(t, d.handlers, 2)
}

// ---- HandleBatch ----

func TestHandleBatch_EmptyBatchDoesNothing(t *testing.T) {
	h := &stubHandler{}
	d := NewAllTxBatchDispatcher(h)
	err := d.HandleBatch(context.Background(), notification.AllTxBatch{BlockNumber: 1})
	require.NoError(t, err)
	assert.Empty(t, h.seen, "no events → no dispatch")
}

// TestHandleBatch_SkipsEventWithoutEthTx covers every way an event can fail to carry
// an ethereum transaction. Metadata that was absent, or a ChaincodeInput that did not
// parse, both reach us from the SDK as an event with no InputArgs at all: the
// dispatcher sees only that outcome, never the wire format behind it.
func TestHandleBatch_SkipsEventWithoutEthTx(t *testing.T) {
	for _, tc := range []struct {
		name string
		args [][]byte
	}{
		{"no metadata or undecodable metadata", nil},
		{"empty args", [][]byte{}},
		{"proposal type but no eth tx", [][]byte{{byte(ProposalTypeEVMTx)}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &stubHandler{}
			d := NewAllTxBatchDispatcher(h)
			err := d.HandleBatch(context.Background(), notification.AllTxBatch{
				BlockNumber: 1,
				Events: []notification.CommittedTxEvent{{
					Transaction: blocks.Transaction{ID: "tx-no-eth", InputArgs: tc.args},
				}},
			})
			require.NoError(t, err)
			assert.Empty(t, h.seen)
		})
	}
}

func TestHandleBatch_SkipsNonEVMTx(t *testing.T) {
	// Args[0] is a made-up proposal type, not ProposalTypeEVMTx.
	h := &stubHandler{}
	d := NewAllTxBatchDispatcher(h)
	err := d.HandleBatch(context.Background(), notification.AllTxBatch{
		BlockNumber: 1,
		Events: []notification.CommittedTxEvent{
			evmEvent("tx-not-evm", 0, blocks.StatusCommitted, ProposalType(0x01), []byte{0xde, 0xad}),
		},
	})
	require.NoError(t, err)
	assert.Empty(t, h.seen)
}

func TestHandleBatch_DispatchesOnlyEVMTxsFromMixedBatch(t *testing.T) {
	h := &stubHandler{}
	d := NewAllTxBatchDispatcher(h)
	err := d.HandleBatch(context.Background(), notification.AllTxBatch{
		BlockNumber: 42,
		Events: []notification.CommittedTxEvent{
			evmEvent("evm-1", 0, blocks.StatusCommitted, ProposalTypeEVMTx, []byte{0xaa}),
			evmEvent("non-evm", 1, blocks.StatusCommitted, ProposalType(0x01), []byte{0xbb}),
			evmEvent("evm-2", 2, blocks.StatusMVCCConflict, ProposalTypeEVMTx, []byte{0xcc}),
		},
	})
	require.NoError(t, err)
	require.Len(t, h.seen, 1, "one dispatched Block")
	b := h.seen[0]
	assert.Equal(t, uint64(42), b.Number)
	assert.Equal(t, blockNumberHash(42), b.Hash)
	assert.Equal(t, blockNumberHash(41), b.ParentHash)
	require.Len(t, b.Transactions, 2, "only the two EVM txs make it through")

	assert.Equal(t, "evm-1", b.Transactions[0].ID)
	assert.Equal(t, int64(0), b.Transactions[0].Number)
	assert.True(t, b.Transactions[0].Valid(), "COMMITTED tx is Valid")
	assert.Equal(t, blocks.StatusCommitted, b.Transactions[0].Status)
	require.Len(t, b.Transactions[0].InputArgs, 2)
	assert.Equal(t, []byte{0xaa}, b.Transactions[0].InputArgs[1])

	assert.Equal(t, "evm-2", b.Transactions[1].ID)
	assert.Equal(t, int64(2), b.Transactions[1].Number)
	assert.False(t, b.Transactions[1].Valid(), "non-COMMITTED tx is not Valid")
	assert.Equal(t, blocks.StatusMVCCConflict, b.Transactions[1].Status)
}

func TestHandleBatch_BlockZeroParentHashDoesNotUnderflow(t *testing.T) {
	// Block 0 realistically never reaches here (genesis has no EVM txs, so the
	// len(txs)==0 guard returns early) but the parentNum computation must not
	// wrap a uint64 to MaxUint64 if it ever does.
	h := &stubHandler{}
	d := NewAllTxBatchDispatcher(h)
	err := d.HandleBatch(context.Background(), notification.AllTxBatch{
		BlockNumber: 0,
		Events: []notification.CommittedTxEvent{
			committedEVMEvent("evm-1"),
		},
	})
	require.NoError(t, err)
	require.Len(t, h.seen, 1)
	assert.Equal(t, blockNumberHash(0), h.seen[0].Hash)
	assert.Equal(t, blockNumberHash(0), h.seen[0].ParentHash, "parent of block 0 must not underflow")
}

func TestHandleBatch_MultipleHandlersAllReceive(t *testing.T) {
	h1, h2 := &stubHandler{}, &stubHandler{}
	d := NewAllTxBatchDispatcher(h1, h2)
	err := d.HandleBatch(context.Background(), notification.AllTxBatch{
		BlockNumber: 7,
		Events: []notification.CommittedTxEvent{
			committedEVMEvent("evm-1"),
		},
	})
	require.NoError(t, err)
	require.Len(t, h1.seen, 1)
	require.Len(t, h2.seen, 1)
	assert.Equal(t, uint64(7), h1.seen[0].Number)
	assert.Equal(t, uint64(7), h2.seen[0].Number)
}

// TestHandleBatch_ForwardsEvents verifies that the revert event (and any other event)
// reaches the handler untouched. The SDK lifts it out of the wire format into
// Transaction.Events, exactly as the block parser does on the delivery path; the
// dispatcher must pass it straight through. A nil slice means no event was emitted.
func TestHandleBatch_ForwardsEvents(t *testing.T) {
	eventPayload := []byte("some-event-bytes")

	withEvent := committedEVMEvent("evm-with-event")
	withEvent.Events = eventPayload

	h := &stubHandler{}
	d := NewAllTxBatchDispatcher(h)
	err := d.HandleBatch(context.Background(), notification.AllTxBatch{
		BlockNumber: 5,
		Events: []notification.CommittedTxEvent{
			withEvent,
			committedEVMEvent("evm-no-event"), // no event emitted
		},
	})
	require.NoError(t, err)
	require.Len(t, h.seen, 1)
	require.Len(t, h.seen[0].Transactions, 2)

	assert.Equal(t, eventPayload, h.seen[0].Transactions[0].Events,
		"event bytes must be forwarded as-is")
	assert.Nil(t, h.seen[0].Transactions[1].Events,
		"a tx that emitted no event must leave Events nil")
}

func TestHandleBatch_HandlerErrorPanics(t *testing.T) {
	h := &stubHandler{err: errors.New("handler blew up")}
	d := NewAllTxBatchDispatcher(h)

	defer func() {
		r := recover()
		require.NotNil(t, r, "handler error must panic")
		err, ok := r.(error)
		require.True(t, ok, "panic value must be an error, got %T", r)
		assert.Contains(t, err.Error(), "handler failed")
		assert.Contains(t, err.Error(), "handler blew up")
	}()

	_ = d.HandleBatch(context.Background(), notification.AllTxBatch{
		BlockNumber: 1,
		Events: []notification.CommittedTxEvent{
			committedEVMEvent("evm-1"),
		},
	})
	t.Fatal("expected panic, HandleBatch returned normally")
}
