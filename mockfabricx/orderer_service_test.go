/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package mockfabricx

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestOrdererServiceCutsBlocks(t *testing.T) {
	tests := map[string]struct {
		cfg   ordererServiceConfig
		txIDs []string
	}{
		"by max tx per block": {
			cfg:   ordererServiceConfig{maxTxPerBlock: 2, blockTimeout: time.Hour, queueSize: 4},
			txIDs: []string{"tx-1", "tx-2"},
		},
		"by timeout": {
			cfg:   ordererServiceConfig{maxTxPerBlock: 100, blockTimeout: 10 * time.Millisecond, queueSize: 4},
			txIDs: []string{"tx-1"},
		},
		"immediate mode": {
			cfg:   ordererServiceConfig{maxTxPerBlock: 1, blockTimeout: 0, queueSize: 4},
			txIDs: []string{"tx-1"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ledger, svc, ctx := newRunningOrdererService(t, tt.cfg)

			for _, txID := range tt.txIDs {
				require.NoError(t, svc.submit(ctx, newTxEnvelope(t, txID)))
			}

			require.Eventually(t, func() bool { return ledger.Height() == 2 }, time.Second, 10*time.Millisecond)
		})
	}
}

func TestAtomicBroadcastSubmitsToOrdererService(t *testing.T) {
	ledger, svc, _ := newRunningOrdererService(t, ordererServiceConfig{maxTxPerBlock: 1, blockTimeout: 0, queueSize: 4})

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer()
	RegisterOrdererServices(srv, svc)
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	stream, err := orderer.NewAtomicBroadcastClient(conn).Broadcast(t.Context())
	require.NoError(t, err)
	require.NoError(t, stream.Send(newTxEnvelope(t, "tx-1")))
	resp, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, common.Status_SUCCESS, resp.Status)
	require.Eventually(t, func() bool { return ledger.Height() == 2 }, time.Second, 10*time.Millisecond)
}

func newRunningOrdererService(t *testing.T, cfg ordererServiceConfig) (*Ledger, *ordererService, context.Context) {
	t.Helper()

	ledger := NewLedger()
	svc := newOrdererService(ledger, cfg)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	go func() { _ = svc.run(ctx) }()

	return ledger, svc, ctx
}

func newTxEnvelope(t *testing.T, txID string) *common.Envelope {
	t.Helper()
	return newTestEnvelope(t, txID, &applicationpb.Tx{Namespaces: []*applicationpb.TxNamespace{{NsId: "basic"}}})
}
