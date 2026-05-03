/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"testing"

	cb "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestFabricXCompatCommittedStatusMapping(t *testing.T) {
	require.True(t, isFabricXCommittedStatus(0))
	require.True(t, isFabricXCommittedStatus(1))
	require.True(t, isFabricXCommittedStatus(2))
	require.False(t, isFabricXCommittedStatus(3))
}

func TestFabricXCompatParserSetsStatusAndValidity(t *testing.T) {
	parser := newFabricXCompatParser(sdk.NoOpLogger{})

	block := buildFabricXTestBlock(t,
		[]byte{2, 3},
		buildFabricXTestEnvelope(t, "tx-0"),
		buildFabricXTestEnvelope(t, "tx-1"),
	)

	got, err := parser.Parse(block)
	require.NoError(t, err)
	require.Len(t, got.Transactions, 2)

	require.Equal(t, 2, got.Transactions[0].Status)
	require.True(t, got.Transactions[0].Valid)

	require.Equal(t, 3, got.Transactions[1].Status)
	require.False(t, got.Transactions[1].Valid)
}

func buildFabricXTestEnvelope(t *testing.T, txID string) *cb.Envelope {
	t.Helper()

	tx := &applicationpb.Tx{
		Namespaces: []*applicationpb.TxNamespace{
			{
				NsId: "basic",
				BlindWrites: []*applicationpb.Write{
					{Key: []byte("k"), Value: []byte("v")},
				},
			},
		},
	}
	txBytes, err := proto.Marshal(tx)
	require.NoError(t, err)

	chdrBytes, err := proto.Marshal(&cb.ChannelHeader{
		TxId: txID,
		Type: int32(cb.HeaderType_MESSAGE),
	})
	require.NoError(t, err)

	payloadBytes, err := proto.Marshal(&cb.Payload{
		Header: &cb.Header{ChannelHeader: chdrBytes},
		Data:   txBytes,
	})
	require.NoError(t, err)

	return &cb.Envelope{Payload: payloadBytes}
}

func buildFabricXTestBlock(t *testing.T, txFilter []byte, envelopes ...*cb.Envelope) *cb.Block {
	t.Helper()

	data := make([][]byte, len(envelopes))
	for i, env := range envelopes {
		var err error
		data[i], err = proto.Marshal(env)
		require.NoError(t, err)
	}

	metadata := make([][]byte, int(cb.BlockMetadataIndex_TRANSACTIONS_FILTER)+1)
	metadata[cb.BlockMetadataIndex_TRANSACTIONS_FILTER] = txFilter

	return &cb.Block{
		Header: &cb.BlockHeader{Number: 1},
		Data:   &cb.BlockData{Data: data},
		Metadata: &cb.BlockMetadata{
			Metadata: metadata,
		},
	}
}

