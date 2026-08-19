/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/notification"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
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

// makeMetadata builds the wire-format Metadata[0] entry the dispatcher expects:
// a marshalled ChaincodeInput whose Args[0] is the proposal-type byte and
// Args[1] is the raw ethereum-tx bytes (opaque to the dispatcher).
func makeMetadata(t *testing.T, propType ProposalType, ethTxBytes []byte) [][]byte {
	t.Helper()
	input := &peer.ChaincodeInput{Args: [][]byte{{byte(propType)}, ethTxBytes}}
	b, err := proto.Marshal(input)
	require.NoError(t, err)
	return [][]byte{b}
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

func TestHandleBatch_SkipsEventWithEmptyMetadata(t *testing.T) {
	h := &stubHandler{}
	d := NewAllTxBatchDispatcher(h)
	err := d.HandleBatch(context.Background(), notification.AllTxBatch{
		BlockNumber: 1,
		Events: []notification.CommittedTxEvent{
			{TxID: "tx-empty-meta", Metadata: nil},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, h.seen)
}

func TestHandleBatch_SkipsEventWithMalformedProtobuf(t *testing.T) {
	h := &stubHandler{}
	d := NewAllTxBatchDispatcher(h)
	err := d.HandleBatch(context.Background(), notification.AllTxBatch{
		BlockNumber: 1,
		Events: []notification.CommittedTxEvent{
			{TxID: "tx-bad-proto", Metadata: [][]byte{{0xff, 0xff, 0xff}}},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, h.seen)
}

func TestHandleBatch_SkipsEventWithInsufficientArgs(t *testing.T) {
	// A ChaincodeInput with only one arg fails the `len(input.Args) < 2` check.
	input := &peer.ChaincodeInput{Args: [][]byte{{byte(ProposalTypeEVMTx)}}}
	metaBytes, err := proto.Marshal(input)
	require.NoError(t, err)

	h := &stubHandler{}
	d := NewAllTxBatchDispatcher(h)
	err = d.HandleBatch(context.Background(), notification.AllTxBatch{
		BlockNumber: 1,
		Events: []notification.CommittedTxEvent{
			{TxID: "tx-one-arg", Metadata: [][]byte{metaBytes}},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, h.seen)
}

func TestHandleBatch_SkipsNonEVMTx(t *testing.T) {
	// Args[0] is a made-up proposal type, not ProposalTypeEVMTx.
	h := &stubHandler{}
	d := NewAllTxBatchDispatcher(h)
	err := d.HandleBatch(context.Background(), notification.AllTxBatch{
		BlockNumber: 1,
		Events: []notification.CommittedTxEvent{
			{TxID: "tx-not-evm", Metadata: makeMetadata(t, ProposalType(0x01), []byte{0xde, 0xad})},
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
			{TxID: "evm-1", TxNum: 0, Status: committerpb.Status_COMMITTED,
				Metadata: makeMetadata(t, ProposalTypeEVMTx, []byte{0xaa})},
			{TxID: "non-evm", TxNum: 1, Status: committerpb.Status_COMMITTED,
				Metadata: makeMetadata(t, ProposalType(0x01), []byte{0xbb})},
			{TxID: "evm-2", TxNum: 2, Status: committerpb.Status_ABORTED_MVCC_CONFLICT,
				Metadata: makeMetadata(t, ProposalTypeEVMTx, []byte{0xcc})},
		},
	})
	require.NoError(t, err)
	require.Len(t, h.seen, 1, "one dispatched Block")
	b := h.seen[0]
	assert.Equal(t, uint64(42), b.Number)
	require.Len(t, b.Transactions, 2, "only the two EVM txs make it through")

	assert.Equal(t, "evm-1", b.Transactions[0].ID)
	assert.Equal(t, int64(0), b.Transactions[0].Number)
	assert.True(t, b.Transactions[0].Valid, "COMMITTED tx is Valid")
	assert.Equal(t, int(committerpb.Status_COMMITTED), b.Transactions[0].Status)
	require.Len(t, b.Transactions[0].InputArgs, 2)
	assert.Equal(t, []byte{0xaa}, b.Transactions[0].InputArgs[1])

	assert.Equal(t, "evm-2", b.Transactions[1].ID)
	assert.Equal(t, int64(2), b.Transactions[1].Number)
	assert.False(t, b.Transactions[1].Valid, "non-COMMITTED tx has Valid=false")
	assert.Equal(t, int(committerpb.Status_ABORTED_MVCC_CONFLICT), b.Transactions[1].Status)
}

func TestHandleBatch_MultipleHandlersAllReceive(t *testing.T) {
	h1, h2 := &stubHandler{}, &stubHandler{}
	d := NewAllTxBatchDispatcher(h1, h2)
	err := d.HandleBatch(context.Background(), notification.AllTxBatch{
		BlockNumber: 7,
		Events: []notification.CommittedTxEvent{
			{TxID: "evm-1", Status: committerpb.Status_COMMITTED,
				Metadata: makeMetadata(t, ProposalTypeEVMTx, []byte{0xaa})},
		},
	})
	require.NoError(t, err)
	require.Len(t, h1.seen, 1)
	require.Len(t, h2.seen, 1)
	assert.Equal(t, uint64(7), h1.seen[0].Number)
	assert.Equal(t, uint64(7), h2.seen[0].Number)
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
			{TxID: "evm-1", Status: committerpb.Status_COMMITTED,
				Metadata: makeMetadata(t, ProposalTypeEVMTx, []byte{0xaa})},
		},
	})
	t.Fatal("expected panic, HandleBatch returned normally")
}

// ---- namespacesToNsRWS ----

func TestNamespacesToNsRWS_EmptyInput(t *testing.T) {
	nsrws, events := namespacesToNsRWS(nil)
	assert.Empty(t, nsrws)
	assert.Nil(t, events)
}

func TestNamespacesToNsRWS_ReadsOnly_WithAndWithoutVersion(t *testing.T) {
	ns := []*applicationpb.TxNamespace{{
		NsId: "evm",
		ReadsOnly: []*applicationpb.Read{
			{Key: []byte("k-noversion"), Version: nil},
			{Key: []byte("k-versioned"), Version: new(uint64(3))},
		},
	}}
	nsrws, events := namespacesToNsRWS(ns)
	require.Len(t, nsrws, 1)
	assert.Nil(t, events)
	assert.Equal(t, "evm", nsrws[0].Namespace)

	reads := nsrws[0].RWS.Reads
	require.Len(t, reads, 2)
	assert.Equal(t, "k-noversion", reads[0].Key)
	assert.Nil(t, reads[0].Version, "nil version stays nil")
	assert.Equal(t, "k-versioned", reads[1].Key)
	require.NotNil(t, reads[1].Version)
	assert.Equal(t, uint64(3), reads[1].Version.BlockNum)
	assert.Empty(t, nsrws[0].RWS.Writes, "reads-only produces no writes")
}

func TestNamespacesToNsRWS_ReadWrites_AppearsInReadsAndWrites(t *testing.T) {
	ns := []*applicationpb.TxNamespace{{
		NsId: "evm",
		ReadWrites: []*applicationpb.ReadWrite{
			{Key: []byte("rw-noversion"), Version: nil, Value: []byte("v1")},
			{Key: []byte("rw-versioned"), Version: new(uint64(7)), Value: []byte("v2")},
		},
	}}
	nsrws, _ := namespacesToNsRWS(ns)
	require.Len(t, nsrws, 1)

	reads := nsrws[0].RWS.Reads
	writes := nsrws[0].RWS.Writes
	require.Len(t, reads, 2, "each ReadWrite adds a read entry")
	require.Len(t, writes, 2, "each ReadWrite adds a write entry")

	assert.Equal(t, "rw-noversion", reads[0].Key)
	assert.Nil(t, reads[0].Version)
	assert.Equal(t, "rw-versioned", reads[1].Key)
	require.NotNil(t, reads[1].Version)
	assert.Equal(t, uint64(7), reads[1].Version.BlockNum)

	assert.Equal(t, "rw-noversion", writes[0].Key)
	assert.Equal(t, []byte("v1"), writes[0].Value)
	assert.Equal(t, "rw-versioned", writes[1].Key)
	assert.Equal(t, []byte("v2"), writes[1].Value)
}

func TestNamespacesToNsRWS_BlindWrite_EventKeyCapturedNotWritten(t *testing.T) {
	ns := []*applicationpb.TxNamespace{{
		NsId: "evm",
		BlindWrites: []*applicationpb.Write{
			{Key: []byte("_event_"), Value: []byte("event-payload")},
		},
	}}
	nsrws, events := namespacesToNsRWS(ns)
	require.Len(t, nsrws, 1)
	assert.Equal(t, []byte("event-payload"), events)
	assert.Empty(t, nsrws[0].RWS.Writes, "_event_ must not appear in writes")
}

func TestNamespacesToNsRWS_BlindWrite_InputKeySkipped(t *testing.T) {
	ns := []*applicationpb.TxNamespace{{
		NsId: "evm",
		BlindWrites: []*applicationpb.Write{
			{Key: []byte("_input_"), Value: []byte("raw-input")},
		},
	}}
	nsrws, events := namespacesToNsRWS(ns)
	require.Len(t, nsrws, 1)
	assert.Nil(t, events, "_input_ does not populate events")
	assert.Empty(t, nsrws[0].RWS.Writes, "_input_ is dropped entirely")
}

func TestNamespacesToNsRWS_BlindWrite_RegularKeyGoesToWrites(t *testing.T) {
	ns := []*applicationpb.TxNamespace{{
		NsId: "evm",
		BlindWrites: []*applicationpb.Write{
			{Key: []byte("acc:0xabc:bal"), Value: []byte("100")},
		},
	}}
	nsrws, _ := namespacesToNsRWS(ns)
	require.Len(t, nsrws, 1)
	writes := nsrws[0].RWS.Writes
	require.Len(t, writes, 1)
	assert.Equal(t, "acc:0xabc:bal", writes[0].Key)
	assert.Equal(t, []byte("100"), writes[0].Value)
}

func TestNamespacesToNsRWS_MultipleNamespacesPreserveOrder(t *testing.T) {
	ns := []*applicationpb.TxNamespace{
		{NsId: "ns-a", ReadsOnly: []*applicationpb.Read{{Key: []byte("k1")}}},
		{NsId: "ns-b", ReadsOnly: []*applicationpb.Read{{Key: []byte("k2")}}},
		{NsId: "ns-c", ReadsOnly: []*applicationpb.Read{{Key: []byte("k3")}}},
	}
	nsrws, _ := namespacesToNsRWS(ns)
	require.Len(t, nsrws, 3)
	assert.Equal(t, "ns-a", nsrws[0].Namespace)
	assert.Equal(t, "ns-b", nsrws[1].Namespace)
	assert.Equal(t, "ns-c", nsrws[2].Namespace)
}

func TestNamespacesToNsRWS_MixedReadsWritesAndBlind(t *testing.T) {
	ns := []*applicationpb.TxNamespace{{
		NsId: "evm",
		ReadsOnly: []*applicationpb.Read{
			{Key: []byte("r1"), Version: new(uint64(1))},
		},
		ReadWrites: []*applicationpb.ReadWrite{
			{Key: []byte("rw1"), Version: new(uint64(2)), Value: []byte("vrw")},
		},
		BlindWrites: []*applicationpb.Write{
			{Key: []byte("bw1"), Value: []byte("vbw")},
			{Key: []byte("_event_"), Value: []byte("evt")},
			{Key: []byte("_input_"), Value: []byte("in")},
		},
	}}
	nsrws, events := namespacesToNsRWS(ns)
	require.Len(t, nsrws, 1)
	assert.Equal(t, []byte("evt"), events)
	assert.Len(t, nsrws[0].RWS.Reads, 2, "1 from ReadsOnly + 1 from ReadWrites")
	assert.Len(t, nsrws[0].RWS.Writes, 2, "1 from ReadWrites + 1 regular BlindWrite (event/input excluded)")
}
