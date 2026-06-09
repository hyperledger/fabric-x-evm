/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package mockfabricx

import (
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestParseEnvelope(t *testing.T) {
	t.Run("extracts tx id namespaces and endorsements", func(t *testing.T) {
		env := newTestEnvelope(t, "tx-1", &applicationpb.Tx{
			Namespaces:   []*applicationpb.TxNamespace{{NsId: "basic", NsVersion: 1}},
			Endorsements: []*applicationpb.Endorsements{{}},
		})

		parsed := ParseEnvelope(env)

		require.NoError(t, parsed.Err)
		require.Equal(t, "tx-1", parsed.TxID)
		require.Equal(t, committerpb.Status_COMMITTED, parsed.Status)
		require.Len(t, parsed.Namespaces, 1)
		require.Equal(t, "basic", parsed.Namespaces[0].NsId)
		require.Len(t, parsed.Endorsements, 1)
	})

	t.Run("skips non-message envelope", func(t *testing.T) {
		payloadBytes, err := proto.Marshal(&common.Payload{
			Header: &common.Header{ChannelHeader: mustMarshal(t, &common.ChannelHeader{Type: int32(common.HeaderType_CONFIG), TxId: "config-tx"})},
		})
		require.NoError(t, err)

		parsed := ParseEnvelope(&common.Envelope{Payload: payloadBytes})

		require.NoError(t, parsed.Err)
		require.Equal(t, "config-tx", parsed.TxID)
		require.True(t, parsed.SkipEvent)
		require.Equal(t, committerpb.Status_COMMITTED, parsed.Status)
	})

	t.Run("marks bad payload when tx id is known", func(t *testing.T) {
		payloadBytes, err := proto.Marshal(&common.Payload{
			Header: &common.Header{ChannelHeader: mustMarshal(t, &common.ChannelHeader{Type: int32(common.HeaderType_MESSAGE), TxId: "bad-tx"})},
			Data:   []byte("not an applicationpb.Tx"),
		})
		require.NoError(t, err)

		parsed := ParseEnvelope(&common.Envelope{Payload: payloadBytes})

		require.Error(t, parsed.Err)
		require.Equal(t, "bad-tx", parsed.TxID)
		require.Equal(t, committerpb.Status_MALFORMED_BAD_ENVELOPE_PAYLOAD, parsed.Status)
		require.Empty(t, parsed.Namespaces)
	})

	t.Run("omits when tx id unavailable", func(t *testing.T) {
		parsed := ParseEnvelope(&common.Envelope{Payload: []byte("bad payload")})

		require.Error(t, parsed.Err)
		require.Empty(t, parsed.TxID)
		require.Equal(t, committerpb.Status_MALFORMED_BAD_ENVELOPE, parsed.Status)
	})
}

func TestLedgerCommitStoresBlockAndEvent(t *testing.T) {
	ledger := NewLedger()
	env := newTestEnvelope(t, "tx-1", &applicationpb.Tx{Namespaces: []*applicationpb.TxNamespace{{NsId: "basic"}}})

	batch := ledger.Commit([]*common.Envelope{env})

	require.Equal(t, uint64(1), batch.BlockNumber)
	require.Len(t, batch.Events, 1)
	require.Equal(t, "tx-1", batch.Events[0].Ref.TxId)
	require.Equal(t, uint64(1), batch.Events[0].Ref.BlockNum)
	require.Equal(t, uint32(0), batch.Events[0].Ref.TxNum)
	require.Equal(t, committerpb.Status_COMMITTED, batch.Events[0].Status)
	require.Equal(t, uint64(2), ledger.Height())
}

func TestSubscribeBlocksFromReturnsExistingAndFutureBlocks(t *testing.T) {
	ledger := NewLedger()
	ledger.Commit([]*common.Envelope{newTestEnvelope(t, "tx-1", &applicationpb.Tx{Namespaces: []*applicationpb.TxNamespace{{NsId: "basic"}}})})

	existingBlocks, chForNewBlocks, cancel := ledger.SubscribeBlocksFrom(1)
	defer cancel()

	require.Len(t, existingBlocks, 1)
	require.Equal(t, uint64(1), existingBlocks[0].Header.Number)

	ledger.Commit([]*common.Envelope{newTestEnvelope(t, "tx-2", &applicationpb.Tx{Namespaces: []*applicationpb.TxNamespace{{NsId: "basic"}}})})

	select {
	case newBlock := <-chForNewBlocks:
		require.Equal(t, uint64(2), newBlock.Header.Number)
	case <-time.After(time.Second):
		require.Fail(t, "timed out waiting for new block")
	}
}

func TestSubscribeEventBatchesReturnsExistingAndFutureBatches(t *testing.T) {
	ledger := NewLedger()
	env1 := newTestEnvelope(t, "tx-1", &applicationpb.Tx{Namespaces: []*applicationpb.TxNamespace{{NsId: "basic"}}})
	env2 := newTestEnvelope(t, "tx-2", &applicationpb.Tx{Namespaces: []*applicationpb.TxNamespace{{NsId: "other"}}})
	ledger.Commit([]*common.Envelope{env1, env2})

	existingBatches, chForNewBatches, cancel := ledger.SubscribeEventBatches()
	defer cancel()

	require.Len(t, existingBatches, 1)
	require.Len(t, existingBatches[0].Events, 2)
	require.Equal(t, "tx-1", existingBatches[0].Events[0].Ref.TxId)
	require.Equal(t, "tx-2", existingBatches[0].Events[1].Ref.TxId)

	ledger.Commit([]*common.Envelope{newTestEnvelope(t, "tx-3", &applicationpb.Tx{Namespaces: []*applicationpb.TxNamespace{{NsId: "basic"}}})})

	select {
	case newBatch := <-chForNewBatches:
		require.Len(t, newBatch.Events, 1)
		require.Equal(t, "tx-3", newBatch.Events[0].Ref.TxId)
	case <-time.After(time.Second):
		require.Fail(t, "timed out waiting for new event batch")
	}
}

func newTestEnvelope(t *testing.T, txID string, tx *applicationpb.Tx) *common.Envelope {
	t.Helper()
	txBytes := mustMarshal(t, tx)
	payloadBytes, err := proto.Marshal(&common.Payload{
		Header: &common.Header{ChannelHeader: mustMarshal(t, &common.ChannelHeader{Type: int32(common.HeaderType_MESSAGE), TxId: txID, ChannelId: "mychannel"})},
		Data:   txBytes,
	})
	require.NoError(t, err)
	return &common.Envelope{Payload: payloadBytes}
}

func mustMarshal(t *testing.T, msg proto.Message) []byte {
	t.Helper()
	data, err := proto.Marshal(msg)
	require.NoError(t, err)
	return data
}
