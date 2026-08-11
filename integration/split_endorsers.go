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
	eapi "github.com/hyperledger/fabric-x-evm/endorser/api"
	eapp "github.com/hyperledger/fabric-x-evm/endorser/app"
	eclient "github.com/hyperledger/fabric-x-evm/endorser/client"
	econf "github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	eserver "github.com/hyperledger/fabric-x-evm/endorser/server"
	estorage "github.com/hyperledger/fabric-x-evm/endorser/storage"
	"github.com/hyperledger/fabric-x-evm/gateway/app"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	"github.com/hyperledger/fabric-x-sdk/identity"
)

// serveEndorser serves an endorser over mTLS gRPC on an ephemeral port and
// returns the bound "host:port" address. It serves with the endorser's own
// org's TLS material; trustedCAs are the CAs whose clients it accepts, which
// is how one org's gateway is able to reach another org's endorser.
func serveEndorser(t *testing.T, svc eapi.Service, ecfg econf.Endorser, trustedCAs []string) string {
	t.Helper()

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

	ctx := t.Context()
	srv := eserver.New(svc)
	go func() {
		if err := srv.Serve(ctx, serverCfg); err != nil && ctx.Err() == nil {
			t.Logf("%s: gRPC server exited: %v", ecfg.Name, err)
		}
	}()

	return serverCfg.GRPC.Endpoint.Address()
}

// startServedEndorser builds an endorser in-process from its own config file
// and serves it over mTLS gRPC, returning a gRPC client for it alongside the
// in-process KVS and builder. dialCfg is updated with the bound port so the
// caller's gateway config points at the live listener.
//
// No synchronizer is started here: the caller registers the returned KVS as a
// block handler on the gateway's synchronizer, the same as an embedded endorser.
func startServedEndorser(t *testing.T, configFile string, evmConfig execution.EVMConfig, trustedCAs []string, dialCfg *common.ClientConfig) (estorage.KVS, endorsement.Builder, *eclient.Client) {
	t.Helper()

	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatalf("load %s: %v", configFile, err)
	}
	if cfg.Endorser == nil {
		t.Fatalf("%s: no endorser configured", configFile)
	}
	ecfg := *cfg.Endorser
	if ecfg.Database.HistorySize == 0 {
		ecfg.Database.HistorySize = 1
	}

	db, builder, end := NewEndorser(t, ecfg, cfg.Network.Channel, cfg.Network.Namespace, evmConfig, cfg.Network.Protocol)
	setDialPort(t, dialCfg, serveEndorser(t, end, ecfg, trustedCAs))

	client, err := eclient.Dial(*dialCfg)
	if err != nil {
		t.Fatalf("%s: dial endorser: %v", ecfg.Name, err)
	}
	t.Cleanup(func() { _ = client.Close() })

	return db, builder, client
}

// startEndorserGRPCServer builds a real, independently synced endorser from a
// standalone config file and serves it over mTLS gRPC, returning its bound
// address. Unlike startServedEndorser this owns its synchronizer, for tests
// that drive the real App rather than the harness.
func startEndorserGRPCServer(t *testing.T, configFile string, trustedCAs []string) string {
	t.Helper()

	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatalf("load %s: %v", configFile, err)
	}
	if cfg.Endorser == nil {
		t.Fatalf("%s: no endorser configured", configFile)
	}
	ecfg := *cfg.Endorser
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

	return serveEndorser(t, end, ecfg, trustedCAs)
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
	setDialPort(t, &cfg.Gateway.Endorsers[0], org1Addr)
	setDialPort(t, &cfg.Gateway.Endorsers[1], org2Addr)

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

// setDialPort points a client config at the ephemeral port a listener actually
// bound, since the config files can only carry a placeholder.
func setDialPort(t *testing.T, e *common.ClientConfig, addr string) {
	t.Helper()

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
