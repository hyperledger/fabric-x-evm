/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"sync"
	"testing"

	cb "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/network"
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

func TestNewFabricXSynchronizerWithPeerUsesCompatParser(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := &captureBlockHandler{}
	peer := &fakeSyncPeer{
		cancel: cancel,
		block: buildFabricXTestBlock(t,
			[]byte{2, 3},
			buildFabricXTestEnvelope(t, "tx-0"),
			buildFabricXTestEnvelope(t, "tx-1"),
		),
	}
	db := fakeBlockHeightReader(0)

	syncer, err := newFabricXSynchronizerWithPeer(db, peer, sdk.NoOpLogger{}, handler)
	require.NoError(t, err)
	require.NotNil(t, syncer)

	err = syncer.Start(ctx)
	require.NoError(t, err)

	require.Equal(t, 1, handler.blocksSeen())
	got, ok := handler.last()
	require.True(t, ok)
	require.Len(t, got.Transactions, 2)
	require.Equal(t, 2, got.Transactions[0].Status)
	require.True(t, got.Transactions[0].Valid)
	require.Equal(t, 3, got.Transactions[1].Status)
	require.False(t, got.Transactions[1].Valid)
}

func TestNewFabricXSynchronizerWithPeerCancellationWithoutBlocks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := &captureBlockHandler{}
	peer := &fakeSyncPeer{cancel: cancel}
	db := fakeBlockHeightReader(0)

	syncer, err := newFabricXSynchronizerWithPeer(db, peer, sdk.NoOpLogger{}, handler)
	require.NoError(t, err)
	require.NotNil(t, syncer)

	err = syncer.Start(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, handler.blocksSeen())
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

type fakeSyncPeer struct {
	cancel func()
	block  *cb.Block
}

func (f *fakeSyncPeer) SubscribeBlocks(ctx context.Context, _ uint64, p network.BlockProcessor) error {
	if f.block != nil {
		if err := p.ProcessBlock(ctx, f.block); err != nil {
			return err
		}
	}
	if f.cancel != nil {
		f.cancel()
	}
	return nil
}

func (f *fakeSyncPeer) BlockHeight(context.Context) (uint64, error) { return 1, nil }
func (f *fakeSyncPeer) Close() error                                { return nil }

type fakeBlockHeightReader uint64

func (f fakeBlockHeightReader) BlockNumber(context.Context) (uint64, error) {
	return uint64(f), nil
}

type captureBlockHandler struct {
	mu     sync.Mutex
	blocks []blocks.Block
}

func (c *captureBlockHandler) Handle(_ context.Context, b blocks.Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blocks = append(c.blocks, b)
	return nil
}

func (c *captureBlockHandler) blocksSeen() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.blocks)
}

func (c *captureBlockHandler) last() (blocks.Block, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.blocks) == 0 {
		return blocks.Block{}, false
	}
	return c.blocks[len(c.blocks)-1], true
}
