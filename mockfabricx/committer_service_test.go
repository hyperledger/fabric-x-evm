/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package mockfabricx

import (
	"context"
	"math"
	"net"
	"testing"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestPeerDeliverStreamsBlocksFromStart(t *testing.T) {
	fixture := newInsecureServiceFixture(t)
	defer fixture.close()
	fixture.ledger.Commit([]*common.Envelope{newTxEnvelope(t, "tx-1")})

	client := peer.NewDeliverClient(fixture.conn)
	stream, err := client.Deliver(t.Context())
	require.NoError(t, err)
	require.NoError(t, stream.Send(newSeekEnvelope(t, 1)))

	resp, err := stream.Recv()
	require.NoError(t, err)
	blockResp := resp.GetBlock()
	require.NotNil(t, blockResp)
	require.Equal(t, uint64(1), blockResp.Header.Number)
}

func TestStreamAllTransactionsStreamsExistingAndFutureEvents(t *testing.T) {
	fixture := newInsecureServiceFixture(t)
	defer fixture.close()
	fixture.ledger.Commit([]*common.Envelope{newTxEnvelope(t, "tx-1")})

	client := committerpb.NewNotifierClient(fixture.conn)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream, err := client.StreamAllTransactions(ctx, &committerpb.StreamAllRequest{IncludeReadWriteSets: true})
	require.NoError(t, err)

	first, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, uint64(1), first.BlockNumber)
	require.Equal(t, "tx-1", first.Events[0].Ref.TxId)
	require.NotEmpty(t, first.Events[0].Namespaces)

	fixture.ledger.Commit([]*common.Envelope{newTxEnvelope(t, "tx-2")})
	second, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "tx-2", second.Events[0].Ref.TxId)
}

func TestGetBlockchainInfoReturnsHeight(t *testing.T) {
	fixture := newInsecureServiceFixture(t)
	defer fixture.close()
	fixture.ledger.Commit([]*common.Envelope{newTxEnvelope(t, "tx-1")})

	client := committerpb.NewBlockQueryServiceClient(fixture.conn)
	info, err := client.GetBlockchainInfo(t.Context(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Equal(t, uint64(2), info.Height)
}

type serviceFixture struct {
	ledger *Ledger
	server *grpc.Server
	conn   *grpc.ClientConn
}

func newInsecureServiceFixture(t *testing.T) serviceFixture {
	t.Helper()
	ledger := NewLedger()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer()
	RegisterCommitterServices(srv, ledger)
	go func() { _ = srv.Serve(lis) }()
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	return serviceFixture{ledger: ledger, server: srv, conn: conn}
}

func (f serviceFixture) close() {
	_ = f.conn.Close()
	f.server.Stop()
}

func newSeekEnvelope(t *testing.T, start uint64) *common.Envelope {
	t.Helper()
	seekInfo := mustMarshal(t, &orderer.SeekInfo{
		Start:    &orderer.SeekPosition{Type: &orderer.SeekPosition_Specified{Specified: &orderer.SeekSpecified{Number: start}}},
		Stop:     &orderer.SeekPosition{Type: &orderer.SeekPosition_Specified{Specified: &orderer.SeekSpecified{Number: math.MaxUint64}}},
		Behavior: orderer.SeekInfo_BLOCK_UNTIL_READY,
	})
	payload := mustMarshal(t, &common.Payload{
		Header: &common.Header{ChannelHeader: mustMarshal(t, &common.ChannelHeader{Type: int32(common.HeaderType_DELIVER_SEEK_INFO), ChannelId: "mychannel"})},
		Data:   seekInfo,
	})
	return &common.Envelope{Payload: payload}
}
