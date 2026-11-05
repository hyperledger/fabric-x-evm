/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"testing"
	"time"

	"github.com/hyperledger/fabric-x-common/api/committerpb"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
	"github.com/hyperledger/fabric-x-sdk/notification"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/hyperledger/fabric-x-evm/integration/tcpcut"
)

// fakeNotifier is a committerpb.SidecarServiceServer that streams one block event
// and then parks, so a test can cut the connection while the stream is live.
type fakeNotifier struct {
	committerpb.UnimplementedSidecarServiceServer
	opened chan struct{} // signalled once per StreamBlocks call
}

func (f *fakeNotifier) StreamBlocks(_ *committerpb.StreamBlocksRequest, stream grpc.ServerStreamingServer[committerpb.BlockEvent]) error {
	select {
	case f.opened <- struct{}{}:
	default:
	}
	// Send one batch so the client has certainly completed the mTLS handshake and
	// received application data through the proxy, then hold the stream open.
	if err := stream.Send(&committerpb.BlockEvent{}); err != nil {
		return err
	}
	<-stream.Context().Done()
	return stream.Context().Err()
}

// serveNotifier starts a committerpb.SidecarService over mTLS on 127.0.0.1 and
// returns its address and the fake behind it.
func serveNotifier(t *testing.T, srv, ca mtlsCerts) (string, *fakeNotifier) {
	t.Helper()

	pair, err := tls.LoadX509KeyPair(srv.certPath, srv.keyPath)
	if err != nil {
		t.Fatalf("load server key pair: %v", err)
	}
	caPEM, err := os.ReadFile(ca.caCertPath)
	if err != nil {
		t.Fatalf("read CA: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("parse CA")
	}

	gs := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{pair},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
	})))
	notifier := &fakeNotifier{opened: make(chan struct{}, 4)}
	committerpb.RegisterSidecarServiceServer(gs, notifier)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = gs.Serve(l) }()
	t.Cleanup(gs.Stop)

	return l.Addr().String(), notifier
}

// TestTCPCut_CutsLiveMTLSGRPCStream is the load-bearing check behind
// testHybridRestartAfterNotificationLoss: that cutting at the TCP layer is enough
// to make a live, mTLS-protected gRPC server-stream fail at the client.
//
// It runs the real SDK stack — nfabx.NewPeer dials through the proxy with the same
// mTLS config the fabric-x test network uses, and notification.AllTxStreamer drives
// the stream exactly as hybridx does — so it also confirms that TLS is negotiated
// end-to-end through a proxy that only forwards bytes.  If this works, the
// integration test needs no reflection into gRPC internals to force a reconnect.
func TestTCPCut_CutsLiveMTLSGRPCStream(t *testing.T) {
	_, srvCerts, cliCerts := newMTLSFixture(t)
	upstream, notifier := serveNotifier(t, srvCerts, mtlsCerts{caCertPath: srvCerts.caCertPath})
	proxy := tcpcut.New(t, upstream)

	// Dial the proxy, not the server: mTLS still verifies the server's identity,
	// because the proxy never terminates the connection.
	peer, err := nfabx.NewPeer(network.PeerConf{
		Address: proxy.Addr().String(),
		TLS: network.TLSConfig{
			Mode:        network.TLSModeMTLS,
			CertPath:    cliCerts.certPath,
			KeyPath:     cliCerts.keyPath,
			CACertPaths: []string{cliCerts.caCertPath},
		},
	}, "mychannel")
	if err != nil {
		t.Fatalf("dial through proxy: %v", err)
	}

	streamer := notification.NewAllTxStreamer(peer, []notification.AllTxHandler{noopAllTxHandler{}}, sdk.NoOpLogger{}, 0)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- streamer.Stream(ctx, &notification.StreamAllRequest{}) }()

	// Wait until the stream is actually open through the proxy — this is the proof
	// that mTLS completed and application data flowed.
	select {
	case <-notifier.opened:
	case <-time.After(30 * time.Second):
		t.Fatal("stream never opened through the proxy — mTLS did not survive it")
	}

	// Nothing should have ended the stream yet.
	select {
	case err := <-errCh:
		t.Fatalf("stream ended before the cut: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	// Cut it. The client must observe the stream failing.
	proxy.Cut()
	select {
	case err := <-errCh:
		// A cut mid-stream reaches the client as a transport error.  It can also
		// surface as a clean EOF, which the SDK maps to nil — the case hybridx now
		// treats as errStreamClosed rather than reconnecting post-switch.
		t.Logf("Stream returned after the cut: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("stream survived the cut; the synchronizer would never restart")
	}
}

// noopAllTxHandler drops every batch; this test is about the transport.
type noopAllTxHandler struct{}

func (noopAllTxHandler) HandleBatch(context.Context, notification.AllTxBatch) error { return nil }

// TestBlockSequenceChecker covers the detector that
// testHybridRestartAfterNotificationLoss relies on.  That test runs only against a
// live fabric-x network, so without this its checker would be unverified — and a
// checker that silently found nothing would make the restart test pass for the
// wrong reason.
func TestBlockSequenceChecker(t *testing.T) {
	for _, tc := range []struct {
		name     string
		blocks   []uint64
		problems int
	}{
		{"clean run", []uint64{5, 6, 7, 8, 9}, 0},
		{"single block", []uint64{5}, 0},
		{"duplicate at re-switch", []uint64{5, 6, 6, 7}, 1},
		{"replayed earlier block", []uint64{5, 6, 7, 6}, 1},
		{"gap during outage", []uint64{5, 6, 9}, 1},
		{"gap then duplicate", []uint64{5, 8, 8}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &blockSequenceChecker{}
			for _, n := range tc.blocks {
				if err := c.Handle(t.Context(), blocks.Block{Number: n}); err != nil {
					t.Fatalf("Handle: %v", err)
				}
			}

			problems, seen := c.report()
			if len(problems) != tc.problems {
				t.Errorf("found %d problems, want %d: %v", len(problems), tc.problems, problems)
			}
			if len(seen) != len(tc.blocks) {
				t.Errorf("recorded %d blocks, want %d", len(seen), len(tc.blocks))
			}
		})
	}
}
