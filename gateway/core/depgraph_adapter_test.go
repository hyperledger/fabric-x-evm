/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"testing"

	"github.com/hyperledger/fabric-protos-go-apiv2/ledger/rwset"
	"github.com/hyperledger/fabric-protos-go-apiv2/ledger/rwset/kvrwset"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-common/protoutil"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// endorsementFor wraps a marshalled payload the way the endorser does.
func endorsementFor(payload []byte) sdk.Endorsement {
	return sdk.Endorsement{Responses: []*peer.ProposalResponse{{Payload: payload}}}
}

// fabricxPayload is what a fabric-x endorsement carries: a marshalled Tx.
func fabricxPayload(t *testing.T, keys ...string) []byte {
	t.Helper()
	ns := &applicationpb.TxNamespace{NsId: "evm"}
	for _, k := range keys {
		ns.ReadWrites = append(ns.ReadWrites, &applicationpb.ReadWrite{Key: []byte(k), Value: []byte("v")})
	}
	raw, err := proto.Marshal(&applicationpb.Tx{Namespaces: []*applicationpb.TxNamespace{ns}})
	require.NoError(t, err)
	return raw
}

func TestTxContent_FabricXPayload(t *testing.T) {
	got, err := txContent(endorsementFor(fabricxPayload(t, "alice", "bob")))
	require.NoError(t, err)
	require.Len(t, got.Namespaces, 1)
	require.Equal(t, "evm", got.Namespaces[0].NsId)
	require.Len(t, got.Namespaces[0].ReadWrites, 2)
}

func TestTxContent_NoResponses(t *testing.T) {
	_, err := txContent(sdk.Endorsement{})
	require.ErrorIs(t, err, errNoReadWriteSet)
}

func TestTxContent_EmptyPayload(t *testing.T) {
	_, err := txContent(endorsementFor(nil))
	require.ErrorIs(t, err, errNoReadWriteSet)
}

func TestTxContent_MalformedPayload(t *testing.T) {
	_, err := txContent(endorsementFor([]byte{0xff, 0xff, 0xff, 0xff}))
	require.Error(t, err)
}

// A classic Fabric endorsement carries a KVRWSet inside ChaincodeAction.
func TestTxContent_ClassicFabricPayload(t *testing.T) {
	kvRwSet := &kvrwset.KVRWSet{
		Writes: []*kvrwset.KVWrite{
			{Key: "k1", Value: []byte("v1")},
		},
	}
	kvBytes, err := proto.Marshal(kvRwSet)
	require.NoError(t, err)

	txRwSet := &rwset.TxReadWriteSet{
		NsRwset: []*rwset.NsReadWriteSet{
			{Namespace: "evm", Rwset: kvBytes},
		},
	}
	txRwSetBytes, err := proto.Marshal(txRwSet)
	require.NoError(t, err)

	payload, err := protoutil.GetBytesProposalResponsePayload(
		[]byte("proposal-hash"), &peer.Response{Status: 200}, txRwSetBytes, nil,
		&peer.ChaincodeID{Name: "evm", Version: "1.0"},
	)
	require.NoError(t, err)

	got, err := txContent(endorsementFor(payload))
	require.NoError(t, err)
	require.Len(t, got.Namespaces, 1)
	require.Equal(t, "evm", got.Namespaces[0].NsId)
	require.Len(t, got.Namespaces[0].ReadWrites, 1)
	require.Equal(t, []byte("k1"), got.Namespaces[0].ReadWrites[0].Key)
}

// A Tx that parses but carries nothing is not schedulable either.
func TestTxContent_NoNamespaces(t *testing.T) {
	raw, err := proto.Marshal(&applicationpb.Tx{})
	require.NoError(t, err)

	_, err = txContent(endorsementFor(raw))
	require.ErrorIs(t, err, errNoReadWriteSet)
}
