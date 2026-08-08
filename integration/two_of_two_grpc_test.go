/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/params"
	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"github.com/hyperledger/fabric-x-committer/utils/serve"
	"github.com/hyperledger/fabric-x-evm/common"
	eapp "github.com/hyperledger/fabric-x-evm/endorser/app"
	eserver "github.com/hyperledger/fabric-x-evm/endorser/server"
	"github.com/hyperledger/fabric-x-evm/gateway/app"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/identity"
)

// startEndorserGRPCServer builds a real, synced endorser from a standalone
// config file and serves it over mTLS gRPC, returning its bound address.
func startEndorserGRPCServer(t *testing.T, configFile string, trustedCAs []string) string {
	t.Helper()

	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatalf("load %s: %v", configFile, err)
	}
	ecfg := cfg.Endorsers[0]
	if ecfg.Database.HistorySize == 0 {
		ecfg.Database.HistorySize = 1
	}

	signer, err := identity.SignerFromMSP(ecfg.Identity.MSPDir, ecfg.Identity.MspID)
	if err != nil {
		t.Fatalf("%s: signer: %v", ecfg.Name, err)
	}

	// A plain logger, not TestLogger: the synchronizer logs from goroutines
	// that outlive the test, and t.Logf panics once the test has finished.
	logger := sdk.NewStdLogger("endorser-" + ecfg.Name)
	end, sync, _, err := eapp.NewEndorser(ecfg, cfg.Network, signer, logger, false)
	if err != nil {
		t.Fatalf("%s: NewEndorser: %v", ecfg.Name, err)
	}

	ctx := t.Context()
	go func() {
		if err := sync.Start(ctx); err != nil && ctx.Err() == nil {
			t.Logf("%s: sync exited: %v", ecfg.Name, err)
		}
	}()
	if err := app.WaitUntilSynced(ctx, sync, 10*time.Second); err != nil {
		t.Fatalf("%s: sync: %v", ecfg.Name, err)
	}

	// The endorser's peer identity carries TLS server material alongside its MSP.
	tlsDir := filepath.Join(filepath.Dir(ecfg.Identity.MSPDir), "tls")
	serverCfg := &eserver.Config{
		GRPC: serve.ServerConfig{
			Endpoint: connection.Endpoint{Host: "127.0.0.1", Port: 0},
			TLS: connection.TLSConfig{
				Mode:        connection.MutualTLSMode,
				CertPath:    filepath.Join(tlsDir, "server.crt"),
				KeyPath:     filepath.Join(tlsDir, "server.key"),
				CACertPaths: trustedCAs,
			},
		},
	}
	serve.PreAllocateListener(t, &serverCfg.GRPC)
	addr := serverCfg.GRPC.Endpoint.Address()

	srv := eserver.New(end)
	go func() {
		if err := srv.Serve(ctx, serverCfg); err != nil && ctx.Err() == nil {
			t.Logf("%s: gRPC server exited: %v", ecfg.Name, err)
		}
	}()

	return addr
}

// buildSplitGatewayApp loads a split-deployment gateway config, points its
// two gateway.endorsers entries at the live addresses, and runs it. It also
// returns the chain config the network's chain ID implies, which transaction
// signing has to match.
func buildSplitGatewayApp(t *testing.T, configFile, org1Addr, org2Addr string) (*app.App, *params.ChainConfig) {
	t.Helper()

	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatalf("load %s: %v", configFile, err)
	}
	if len(cfg.Gateway.Endorsers) != 2 {
		t.Fatalf("%s: want 2 gateway.endorsers entries, got %d", configFile, len(cfg.Gateway.Endorsers))
	}
	setPort := func(e *common.ClientConfig, addr string) {
		_, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			t.Fatalf("split addr %q: %v", addr, err)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			t.Fatalf("parse port %q: %v", portStr, err)
		}
		e.Endpoint.Port = port
	}
	setPort(&cfg.Gateway.Endorsers[0], org1Addr)
	setPort(&cfg.Gateway.Endorsers[1], org2Addr)

	gwSigner, err := identity.SignerFromMSP(cfg.Gateway.Identity.MSPDir, cfg.Gateway.Identity.MspID)
	if err != nil {
		t.Fatalf("gateway signer: %v", err)
	}

	application, err := app.NewWithSigner(t.Context(), cfg, gwSigner)
	if err != nil {
		t.Fatalf("build split app: %v", err)
	}

	ctx := t.Context()
	go func() {
		if err := application.Run(ctx); err != nil && ctx.Err() == nil {
			t.Logf("split gateway exited: %v", err)
		}
	}()

	// Run starts the worker pool before the HTTP listener, so a successful
	// dial here confirms the worker pool is up too.
	_, listenPort, err := net.SplitHostPort(cfg.Gateway.Listen)
	if err != nil {
		t.Fatalf("parse gateway.listen %q: %v", cfg.Gateway.Listen, err)
	}
	waitForTCP(t, net.JoinHostPort("127.0.0.1", listenPort))

	return application, common.BuildChainConfig(cfg.Network.ChainID)
}

func waitForTCP(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to accept connections", addr)
}
