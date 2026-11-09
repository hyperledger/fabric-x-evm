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

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
	"github.com/hyperledger/fabric-x-sdk/notification"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/hyperledger/fabric-x-evm/common"
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

// TestBlockSequenceChecker_RecordsPath covers the per-block path recording that
// testHybridRestartAfterNotificationLoss uses to assert the during-outage block was
// replayed by delivery.  That assertion is the one thing tying the test to the
// restart machinery, so the mechanism behind it should not be taken on trust: a
// pathFor that reported pathDelivery regardless would make the test unfalsifiable.
func TestBlockSequenceChecker_RecordsPath(t *testing.T) {
	c := &blockSequenceChecker{}

	// Before watchPaths the path is unknown rather than guessed — the checker is
	// registered as a handler before a synchronizer exists to ask.
	if err := c.Handle(t.Context(), blocks.Block{Number: 5}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got, seen := c.pathFor(5); !seen || got != pathUnknown {
		t.Errorf("block 5 recorded as (%q, seen=%v), want (%q, true)", got, seen, pathUnknown)
	}

	// Then the path follows whatever the source reports at the time of the call, so
	// each block carries the path that dispatched it rather than the latest one.
	path := pathDelivery
	c.watchPaths(func() blockPath { return path })

	if err := c.Handle(t.Context(), blocks.Block{Number: 6}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	path = pathNotification
	if err := c.Handle(t.Context(), blocks.Block{Number: 7}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	for _, want := range []struct {
		block uint64
		path  blockPath
	}{{6, pathDelivery}, {7, pathNotification}} {
		if got, seen := c.pathFor(want.block); !seen || got != want.path {
			t.Errorf("block %d recorded as (%q, seen=%v), want (%q, true)", want.block, got, seen, want.path)
		}
	}

	// A block that was never dispatched must report as unseen, not as some default
	// path: the test treats "never applied" as a distinct failure.
	if got, seen := c.pathFor(99); seen {
		t.Errorf("block 99 was never dispatched but pathFor reported (%q, seen=true)", got)
	}
}

// TestTxWatcher covers the watcher testHybridRestartAfterNotificationLoss uses to
// decide when the outage has lasted long enough.  It gates the whole point of that
// test: signalling early would release the hold before the chain had moved on,
// leaving no gap to replay and quietly removing the test's reason to exist.
func TestTxWatcher(t *testing.T) {
	// A committed EVM event for tx, in the shape the SDK hands to a handler: the
	// network boundary has already decoded the metadata, so InputArgs carries the raw
	// Ethereum transaction.
	event := func(tx *types.Transaction, status blocks.Status) notification.CommittedTxEvent {
		t.Helper()
		raw, err := tx.MarshalBinary()
		if err != nil {
			t.Fatalf("marshal tx: %v", err)
		}
		return notification.CommittedTxEvent{
			EventName: common.ProposalTypeEVMTx,
			InputArgs: [][]byte{raw},
			Status:    status}
	}

	want := types.NewTx(&types.LegacyTx{Nonce: 1, Gas: 21000})
	other := types.NewTx(&types.LegacyTx{Nonce: 2, Gas: 21000})

	t.Run("signals on the watched transaction committing", func(t *testing.T) {
		w := &txWatcher{want: want.Hash(), committed: make(chan struct{})}

		// Another transaction, and a non-EVM event, must not signal: releasing the
		// outage on someone else's traffic is exactly the failure mode here.
		if err := w.HandleBatch(t.Context(), notification.AllTxBatch{Events: []notification.CommittedTxEvent{
			event(other, blocks.StatusCommitted),
			{EventName: "not-an-evm-tx"},
		}}); err != nil {
			t.Fatalf("HandleBatch: %v", err)
		}
		select {
		case <-w.committed:
			t.Fatal("signalled on a block that did not carry the watched transaction")
		default:
		}

		if err := w.HandleBatch(t.Context(), notification.AllTxBatch{
			Events: []notification.CommittedTxEvent{event(want, blocks.StatusCommitted)},
		}); err != nil {
			t.Fatalf("HandleBatch: %v", err)
		}
		select {
		case <-w.committed:
		default:
			t.Error("did not signal when the watched transaction committed")
		}

		// A repeat must not re-close the channel.
		if err := w.HandleBatch(t.Context(), notification.AllTxBatch{
			Events: []notification.CommittedTxEvent{event(want, blocks.StatusCommitted)},
		}); err != nil {
			t.Fatalf("HandleBatch on a repeated event: %v", err)
		}
	})

	t.Run("does not signal on a rejected transaction", func(t *testing.T) {
		// A rejection leaves the chain where it was, so there would be no gap to
		// replay.  The watcher records it for the error message but must not release
		// the hold.
		w := &txWatcher{want: want.Hash(), committed: make(chan struct{})}
		if err := w.HandleBatch(t.Context(), notification.AllTxBatch{
			Events: []notification.CommittedTxEvent{event(want, blocks.StatusMVCCConflict)},
		}); err != nil {
			t.Fatalf("HandleBatch: %v", err)
		}

		select {
		case <-w.committed:
			t.Error("signalled on a rejected transaction; the chain never moved on, so there is no gap to replay")
		default:
		}
		if status, seen := w.outcome(); !seen || status != blocks.StatusMVCCConflict {
			t.Errorf("outcome() = (%v, %v), want (%v, true)", status, seen, blocks.StatusMVCCConflict)
		}
	})

	t.Run("reports nothing seen before any event", func(t *testing.T) {
		w := &txWatcher{want: want.Hash(), committed: make(chan struct{})}
		if status, seen := w.outcome(); seen {
			t.Errorf("outcome() = (%v, true) before any event, want seen=false", status)
		}
	})
}
