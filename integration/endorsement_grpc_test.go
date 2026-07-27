/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"math/big"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"github.com/hyperledger/fabric-x-committer/utils/serve"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/hyperledger/fabric-x-evm/common"
	eapi "github.com/hyperledger/fabric-x-evm/endorser/api"
	eclient "github.com/hyperledger/fabric-x-evm/endorser/client"
	econf "github.com/hyperledger/fabric-x-evm/endorser/config"
	ecore "github.com/hyperledger/fabric-x-evm/endorser/core"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	eserver "github.com/hyperledger/fabric-x-evm/endorser/server"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

const grpcTestChainID int64 = 4011

// newTestEndorser builds a fresh, standalone in-process endorser backed by an
// empty in-memory KVS - no priming, no gateway, no network. It is the same
// eapi.Service the gateway drives in production; only the transport changes
// between the tests below.
func newTestEndorser(t *testing.T) *ecore.Endorser {
	t.Helper()
	ecfg := econf.Endorser{
		Name: "endorser1",
		Database: econf.DB{
			Database:    "memory",
			ConnString:  filepath.Join(t.TempDir(), "endorser.db"),
			HistorySize: 1,
		},
	}
	evmConfig := execution.EVMConfig{ChainConfig: common.BuildChainConfig(grpcTestChainID)}
	_, _, end := NewEndorser(t, ecfg, "mychannel", "basic", evmConfig, "fabric-x")
	return end
}

// startGRPCServer wires svc behind a real mTLS gRPC listener and starts
// serving in the background until the test ends. It returns the listener's
// address.
func startGRPCServer(t *testing.T, svc eapi.Service, certs mtlsCerts) string {
	t.Helper()
	cfg := &eserver.Config{
		GRPC: serve.ServerConfig{
			Endpoint: connection.Endpoint{Host: "127.0.0.1", Port: 0},
			TLS: connection.TLSConfig{
				Mode:        connection.MutualTLSMode,
				CertPath:    certs.certPath,
				KeyPath:     certs.keyPath,
				CACertPaths: []string{certs.caCertPath},
			},
		},
	}
	serve.PreAllocateListener(t, &cfg.GRPC)
	addr := cfg.GRPC.Endpoint.Address()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	srv := eserver.New(svc)
	go func() {
		if err := srv.Serve(ctx, cfg); err != nil && ctx.Err() == nil {
			t.Logf("server exited: %v", err)
		}
	}()

	return addr
}

// clientTLSCreds builds gRPC transport credentials from a client-side
// connection.TLSConfig, using the same code path production dialing will use.
func clientTLSCreds(t *testing.T, cfg connection.TLSConfig) credentials.TransportCredentials {
	t.Helper()
	tlsCreds, err := connection.NewClientTLSCredentials(cfg)
	if err != nil {
		t.Fatalf("NewClientTLSCredentials: %v", err)
	}
	creds, err := connection.NewClientGRPCTransportCredentials(tlsCreds)
	if err != nil {
		t.Fatalf("NewClientGRPCTransportCredentials: %v", err)
	}
	return creds
}

// dialWithCreds opens a gRPC client connection to addr using creds. Dialing is
// lazy (grpc.NewClient does not connect eagerly), so failures surface on the
// first RPC, not here.
func dialWithCreds(t *testing.T, addr string, creds credentials.TransportCredentials) *eclient.Client {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return eclient.New(conn)
}

// dialMTLS is the happy-path dial: valid client cert, valid trusted CA.
func dialMTLS(t *testing.T, addr string, client, ca mtlsCerts) *eclient.Client {
	t.Helper()
	creds := clientTLSCreds(t, connection.TLSConfig{
		Mode:        connection.MutualTLSMode,
		CertPath:    client.certPath,
		KeyPath:     client.keyPath,
		CACertPaths: []string{ca.caCertPath},
	})
	return dialWithCreds(t, addr, creds)
}

// grpcCode extracts the gRPC status code from err, or codes.OK if err is nil.
func grpcCode(err error) codes.Code {
	return status.Code(err)
}

// newSignedTx generates a fresh sender key and returns it plus a tx signed for
// nonce 0, so the caller can reuse the key to build further txs (e.g. to
// exercise nonce reuse).
func newSignedTx(t *testing.T) (*ecdsa.PrivateKey, *types.Transaction) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key, signTxWithKey(t, key, 0)
}

// signTxWithKey signs a simple value transfer at the given nonce with key.
func signTxWithKey(t *testing.T, key *ecdsa.PrivateKey, nonce uint64) *types.Transaction {
	t.Helper()
	to := ethcommon.HexToAddress("0x1111111111111111111111111111111111111111")
	tx := types.NewTransaction(nonce, to, big.NewInt(0), 21_000, big.NewInt(1), nil)
	signed, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(grpcTestChainID)), key)
	if err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	return signed
}

// signTxWrongChainID signs the same shape of tx as signTxWithKey, but for a
// different chain ID than the endorser is configured with. Sender recovery
// then fails deterministically - a rejection that needs no prior committed
// state, unlike nonce reuse (Execute here never commits between calls).
func signTxWrongChainID(t *testing.T, key *ecdsa.PrivateKey) *types.Transaction {
	t.Helper()
	to := ethcommon.HexToAddress("0x1111111111111111111111111111111111111111")
	tx := types.NewTransaction(0, to, big.NewInt(0), 21_000, big.NewInt(1), nil)
	signed, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(grpcTestChainID+1)), key)
	if err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	return signed
}

// newInvocation builds the endorsement invocation a real gateway would send
// alongside ethTx.
func newInvocation(t *testing.T, ethTx *types.Transaction) endorsement.Invocation {
	t.Helper()
	raw, err := ethTx.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal tx: %v", err)
	}
	inv, err := endorsement.NewInvocation(localSigner{}, "mychannel", "basic", "1.0",
		[][]byte{{byte(common.ProposalTypeEVMTx)}, raw})
	if err != nil {
		t.Fatalf("new invocation: %v", err)
	}
	return inv
}

// TestGRPCEndorsement_Integration exercises Execute, Call, and the four state
// readers through a real gRPC connection over mTLS, against a real endorser.
func TestGRPCEndorsement_Integration(t *testing.T) {
	end := newTestEndorser(t)
	_, server, client := newMTLSFixture(t)
	addr := startGRPCServer(t, end, server)
	c := dialMTLS(t, addr, client, server)

	ctx := context.Background()
	account := ethcommon.HexToAddress("0x2222222222222222222222222222222222222222")

	if bal, err := c.BalanceAt(ctx, account, nil); err != nil || bal.Sign() != 0 {
		t.Errorf("BalanceAt = (%v, %v), want (0, nil)", bal, err)
	}
	if code, err := c.CodeAt(ctx, account, nil); err != nil || len(code) != 0 {
		t.Errorf("CodeAt = (%x, %v), want (empty, nil)", code, err)
	}
	if nonce, err := c.NonceAt(ctx, account, nil); err != nil || nonce != 0 {
		t.Errorf("NonceAt = (%d, %v), want (0, nil)", nonce, err)
	}
	if val, err := c.StorageAt(ctx, account, ethcommon.Hash{}, nil); err != nil || !bytes.Equal(val, ethcommon.Hash{}.Bytes()) {
		t.Errorf("StorageAt = (%x, %v), want (32 zero bytes, nil)", val, err)
	}

	// A call to an address with no code succeeds with empty output - standard
	// EVM semantics, needs no funded account.
	msg := &ethereum.CallMsg{To: &account}
	if out, err := c.Call(ctx, msg, nil); err != nil || len(out) != 0 {
		t.Errorf("Call = (%x, %v), want (empty, nil)", out, err)
	}

	// A valid tx is accepted; the same tx signed for the wrong chain ID fails
	// sender recovery and is deterministically rejected.
	key, tx1 := newSignedTx(t)
	resp1, err := c.Execute(ctx, newInvocation(t, tx1), tx1)
	if err != nil {
		t.Fatalf("Execute (first): transport error %v", err)
	}
	if resp1.GetResponse().GetStatus() != common.StatusOK {
		t.Errorf("Execute (first) status = %d, want %d (StatusOK); message=%q",
			resp1.GetResponse().GetStatus(), common.StatusOK, resp1.GetResponse().GetMessage())
	}

	tx2 := signTxWrongChainID(t, key)
	resp2, err := c.Execute(ctx, newInvocation(t, tx2), tx2)
	if err != nil {
		t.Fatalf("Execute (wrong chain ID): transport error %v", err)
	}
	if resp2.GetResponse().GetStatus() != common.StatusTxRejected {
		t.Errorf("Execute (wrong chain ID) status = %d, want %d (StatusTxRejected); message=%q",
			resp2.GetResponse().GetStatus(), common.StatusTxRejected, resp2.GetResponse().GetMessage())
	}
}

// TestGRPCEndorsement_Parity drives the same inputs through two independent,
// identically-configured endorsers - one called in-process, one over the gRPC
// boundary - and asserts the outcomes match. This proves the wire round-trip
// preserves semantics; it is not a test of business logic (covered elsewhere).
func TestGRPCEndorsement_Parity(t *testing.T) {
	inProcess := newTestEndorser(t)
	overWire := newTestEndorser(t)
	_, server, client := newMTLSFixture(t)
	addr := startGRPCServer(t, overWire, server)
	c := dialMTLS(t, addr, client, server)

	ctx := context.Background()
	account := ethcommon.HexToAddress("0x3333333333333333333333333333333333333333")

	directBal, directErr := inProcess.BalanceAt(ctx, account, nil)
	wireBal, wireErr := c.BalanceAt(ctx, account, nil)
	if directErr != nil || wireErr != nil || directBal.Cmp(wireBal) != 0 {
		t.Errorf("BalanceAt parity: direct=(%v,%v) wire=(%v,%v)", directBal, directErr, wireBal, wireErr)
	}

	directNonce, directErr := inProcess.NonceAt(ctx, account, nil)
	wireNonce, wireErr := c.NonceAt(ctx, account, nil)
	if directErr != nil || wireErr != nil || directNonce != wireNonce {
		t.Errorf("NonceAt parity: direct=(%v,%v) wire=(%v,%v)", directNonce, directErr, wireNonce, wireErr)
	}

	// Same two-step scenario (valid tx accepted, then a wrong-chain-ID tx
	// rejected) driven independently through both paths; their Invocations
	// differ (fresh TxID each), but the status/message outcome at each step
	// must match.
	directKey, directTx1 := newSignedTx(t)
	directResp1, directErr := inProcess.Execute(ctx, newInvocation(t, directTx1), directTx1)
	wireKey, wireTx1 := newSignedTx(t)
	wireResp1, wireErr := c.Execute(ctx, newInvocation(t, wireTx1), wireTx1)
	if directErr != nil || wireErr != nil {
		t.Fatalf("Execute (first) transport errors: direct=%v wire=%v", directErr, wireErr)
	}
	if directResp1.GetResponse().GetStatus() != wireResp1.GetResponse().GetStatus() ||
		directResp1.GetResponse().GetMessage() != wireResp1.GetResponse().GetMessage() {
		t.Errorf("Execute (first) parity: direct=(%d,%q) wire=(%d,%q)",
			directResp1.GetResponse().GetStatus(), directResp1.GetResponse().GetMessage(),
			wireResp1.GetResponse().GetStatus(), wireResp1.GetResponse().GetMessage())
	}

	directTx2 := signTxWrongChainID(t, directKey)
	directResp2, directErr := inProcess.Execute(ctx, newInvocation(t, directTx2), directTx2)
	wireTx2 := signTxWrongChainID(t, wireKey)
	wireResp2, wireErr := c.Execute(ctx, newInvocation(t, wireTx2), wireTx2)
	if directErr != nil || wireErr != nil {
		t.Fatalf("Execute (wrong chain ID) transport errors: direct=%v wire=%v", directErr, wireErr)
	}
	if directResp2.GetResponse().GetStatus() != wireResp2.GetResponse().GetStatus() ||
		directResp2.GetResponse().GetMessage() != wireResp2.GetResponse().GetMessage() {
		t.Errorf("Execute (wrong chain ID) parity: direct=(%d,%q) wire=(%d,%q)",
			directResp2.GetResponse().GetStatus(), directResp2.GetResponse().GetMessage(),
			wireResp2.GetResponse().GetStatus(), wireResp2.GetResponse().GetMessage())
	}
}

// TestGRPCEndorsement_MissingClientCert dials with mode=tls (CA trust only, no
// client cert offered) against a server that requires mTLS. The handshake must
// fail; the RPC must not appear to succeed.
func TestGRPCEndorsement_MissingClientCert(t *testing.T) {
	end := newTestEndorser(t)
	_, server, _ := newMTLSFixture(t)
	addr := startGRPCServer(t, end, server)

	creds := clientTLSCreds(t, connection.TLSConfig{
		Mode:        connection.OneSideTLSMode,
		CACertPaths: []string{server.caCertPath},
	})
	c := dialWithCreds(t, addr, creds)

	_, err := c.NonceAt(context.Background(), ethcommon.Address{}, nil)
	if grpcCode(err) != codes.Unavailable {
		t.Fatalf("code = %v, want Unavailable (missing client cert must be rejected)", grpcCode(err))
	}
}

// TestGRPCEndorsement_UntrustedClientCert dials with a client cert issued by an
// unrelated CA, not the one the server trusts. The handshake must fail.
func TestGRPCEndorsement_UntrustedClientCert(t *testing.T) {
	end := newTestEndorser(t)
	_, server, _ := newMTLSFixture(t)
	addr := startGRPCServer(t, end, server)

	untrustedCert, untrustedKey := newUntrustedClientCert(t)
	creds := clientTLSCreds(t, connection.TLSConfig{
		Mode:        connection.MutualTLSMode,
		CertPath:    untrustedCert,
		KeyPath:     untrustedKey,
		CACertPaths: []string{server.caCertPath},
	})
	c := dialWithCreds(t, addr, creds)

	_, err := c.NonceAt(context.Background(), ethcommon.Address{}, nil)
	if grpcCode(err) != codes.Unavailable {
		t.Fatalf("code = %v, want Unavailable (untrusted client cert must be rejected)", grpcCode(err))
	}
}

// TestGRPCEndorsement_PlaintextRefused dials with no TLS at all against a
// server that requires mTLS. The connection must not succeed.
func TestGRPCEndorsement_PlaintextRefused(t *testing.T) {
	end := newTestEndorser(t)
	_, server, _ := newMTLSFixture(t)
	addr := startGRPCServer(t, end, server)

	c := dialWithCreds(t, addr, insecure.NewCredentials())

	_, err := c.NonceAt(context.Background(), ethcommon.Address{}, nil)
	if grpcCode(err) != codes.Unavailable {
		t.Fatalf("code = %v, want Unavailable (plaintext must be refused)", grpcCode(err))
	}
}

// TestGRPCEndorsement_ServerUnavailable targets an address nothing is
// listening on and expects a retryable Unavailable error, not a hang.
func TestGRPCEndorsement_ServerUnavailable(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := lis.Addr().String()
	if err := lis.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, server, client := newMTLSFixture(t)
	creds := clientTLSCreds(t, connection.TLSConfig{
		Mode:        connection.MutualTLSMode,
		CertPath:    client.certPath,
		KeyPath:     client.keyPath,
		CACertPaths: []string{server.caCertPath},
	})
	c := dialWithCreds(t, addr, creds)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = c.NonceAt(ctx, ethcommon.Address{}, nil)
	if grpcCode(err) != codes.Unavailable {
		t.Fatalf("code = %v, want Unavailable (no server listening)", grpcCode(err))
	}
}

// TestGRPCEndorsement_DeadlineExceeded targets a listener that accepts the TCP
// connection but never completes a TLS handshake, so the RPC must time out on
// the caller's deadline rather than hang indefinitely.
func TestGRPCEndorsement_DeadlineExceeded(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			// Accept and hold the connection open without ever speaking TLS,
			// simulating a stalled server.
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()

	_, server, client := newMTLSFixture(t)
	creds := clientTLSCreds(t, connection.TLSConfig{
		Mode:        connection.MutualTLSMode,
		CertPath:    client.certPath,
		KeyPath:     client.keyPath,
		CACertPaths: []string{server.caCertPath},
	})
	c := dialWithCreds(t, lis.Addr().String(), creds)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err = c.NonceAt(ctx, ethcommon.Address{}, nil)
	if grpcCode(err) != codes.DeadlineExceeded {
		t.Fatalf("code = %v, want DeadlineExceeded", grpcCode(err))
	}
}
