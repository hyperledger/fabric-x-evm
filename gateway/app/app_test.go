/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	sdk "github.com/hyperledger/fabric-x-sdk"

	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
)

func splitModeConfig() config.Config {
	return config.Config{
		Gateway: config.Gateway{
			Endorsers: []common.ClientConfig{
				{
					Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 1234},
					TLS: common.TLSConfig{
						Mode:        "mtls",
						CertPath:    "/no/such/cert.pem",
						KeyPath:     "/no/such/key.pem",
						CACertPaths: []string{"/no/such/ca.pem"},
					},
				},
			},
		},
	}
}

// Test RPC needs a local KVS to snapshot/revert against, which split
// deployment doesn't have - it must be rejected before any dialing happens.
func TestNewApp_TestRPCRejectedInSplitMode(t *testing.T) {
	_, err := newApp(context.Background(), splitModeConfig(), nil, true, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "test RPC is not supported") {
		t.Errorf("error = %q, want it to mention test RPC is not supported", err.Error())
	}
}

// Without test RPC, newApp routes split-mode configs into newSplitApp - a
// dial failure there surfaces immediately, proving the routing happened.
func TestNewApp_RoutesToSplitMode(t *testing.T) {
	_, err := newApp(context.Background(), splitModeConfig(), nil, false, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "dial endorser 0") {
		t.Errorf("error = %q, want it to have gone through newSplitApp", err.Error())
	}
}

// A dial failure for one endorser must fail newSplitApp outright, before ever
// reaching buildApp (no chain/db/network setup should be attempted).
func TestNewSplitApp_DialFailureReturnsError(t *testing.T) {
	logger := sdk.NewStdLogger("gateway")
	_, err := newSplitApp(context.Background(), splitModeConfig(), nil, logger, false, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "dial endorser 0") {
		t.Errorf("error = %q, want it to mention the failing endorser index", err.Error())
	}
}

// If a later endorser fails to dial, connections already opened for earlier
// ones must be closed rather than leaked.
func TestNewSplitApp_ClosesEarlierConnsOnLaterFailure(t *testing.T) {
	cfg := config.Config{
		Gateway: config.Gateway{
			Endorsers: []common.ClientConfig{
				// Dials successfully: no TLS, no live server needed (lazy dial).
				{Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 1}},
				// Fails at credential loading, before any network I/O.
				{
					Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: 2},
					TLS: common.TLSConfig{
						Mode:        "mtls",
						CertPath:    "/no/such/cert.pem",
						KeyPath:     "/no/such/key.pem",
						CACertPaths: []string{"/no/such/ca.pem"},
					},
				},
			},
		},
	}
	logger := sdk.NewStdLogger("gateway")

	_, err := newSplitApp(context.Background(), cfg, nil, logger, false, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "dial endorser 1") {
		t.Errorf("error = %q, want it to mention the second (failing) endorser", err.Error())
	}
}

func endpoint(port int) common.ClientConfig {
	return common.ClientConfig{Endpoint: &common.Endpoint{Host: "127.0.0.1", Port: port}}
}

// splitAppConfig returns a config that dials one endorser successfully
// (lazy dial, no live server needed) and is otherwise ready for buildApp.
func splitAppConfig(t *testing.T, dbConnString string) config.Config {
	t.Helper()
	return config.Config{
		Network: common.Network{Protocol: "fabric-x", Channel: "mychannel", Namespace: "basic", NsVersion: "1.0", ChainID: 4011},
		Gateway: config.Gateway{
			Database:       config.DB{ConnString: dbConnString},
			Orderers:       []common.ClientConfig{endpoint(1)},
			Committer:      endpoint(2),
			Endorsers:      []common.ClientConfig{endpoint(3)},
			SubmitterCount: 1,
		},
	}
}

// The wiring underneath buildApp (orderer/committer/peer clients) dials
// lazily, the same way endorser/client.Dial does - none of it requires a
// reachable server to construct successfully. So a full split-deployment App
// can be built and torn down with nothing but local disk (the chain DB) and
// syntactically valid addresses.
func TestNewSplitApp_Success(t *testing.T) {
	dbPath := "file:" + filepath.Join(t.TempDir(), "gw.db")
	cfg := splitAppConfig(t, dbPath)
	logger := sdk.NewStdLogger("gateway")

	app, err := newSplitApp(context.Background(), cfg, nil, logger, false, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(app.endorserConns) != 1 {
		t.Errorf("endorserConns = %d, want 1", len(app.endorserConns))
	}

	if err := app.Shutdown(); err != nil {
		t.Errorf("Shutdown: unexpected error: %v", err)
	}
}

// If buildApp fails after every endorser dialed successfully, the already-open
// endorser connections must still be closed rather than leaked.
func TestNewSplitApp_BuildAppFailureClosesConns(t *testing.T) {
	// A DB path inside a directory that doesn't exist: dialing the endorser
	// succeeds first, then core.NewChain fails to open the database.
	cfg := splitAppConfig(t, "file:/no/such/directory/gw.db")
	logger := sdk.NewStdLogger("gateway")

	_, err := newSplitApp(context.Background(), cfg, nil, logger, false, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to create chain") {
		t.Errorf("error = %q, want it to come from chain creation", err.Error())
	}
}

// Shutdown must not fail or block just because closing an endorser connection
// errors - it logs and continues, the same way it already treats gateway.Stop
// and chain.Close errors as non-fatal.
func TestApp_Shutdown_ToleratesEndorserCloseError(t *testing.T) {
	dbPath := "file:" + filepath.Join(t.TempDir(), "gw.db")
	cfg := splitAppConfig(t, dbPath)
	logger := sdk.NewStdLogger("gateway")

	app, err := newSplitApp(context.Background(), cfg, nil, logger, false, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Close it once ourselves so Shutdown's own Close() call errors (closing
	// an already-closed *grpc.ClientConn returns a non-nil error).
	if err := app.endorserConns[0].Close(); err != nil {
		t.Fatalf("pre-close: unexpected error: %v", err)
	}

	if err := app.Shutdown(); err != nil {
		t.Errorf("Shutdown: unexpected error: %v", err)
	}
}
