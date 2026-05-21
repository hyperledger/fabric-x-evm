/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/identity"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfab "github.com/hyperledger/fabric-x-sdk/network/fabric"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
	"golang.org/x/sync/errgroup"
	_ "modernc.org/sqlite"

	eapp "github.com/hyperledger/fabric-x-evm/endorser/app"
	endorsertestimpl "github.com/hyperledger/fabric-x-evm/endorser/testimpl"
	"github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/gateway/testimpl"
)

var appLogger = flogging.MustGetLogger("gateway.app")

// App represents the gateway application with all its components.
type App struct {
	cfg           config.Config
	endorserSyncs []*network.Synchronizer
	gwSync        *network.Synchronizer
	gateway       *core.Gateway
	submitter     core.Submitter
	chain         *core.Chain
	rpcServer     *rpc.Server
	httpServer    *http.Server
}

// Gateway returns the inner gateway, e.g. for use in tests.
func (a *App) Gateway() *core.Gateway { return a.gateway }

// New creates a new gateway application from the provided configuration.
// It loads the gateway signer from the MSP directory configured in cfg.
func New(ctx context.Context, cfg config.Config) (*App, error) {
	gwSigner, err := identity.SignerFromMSP(cfg.Gateway.Identity.MSPDir, cfg.Gateway.Identity.MspID)
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway signer: %w", err)
	}
	return NewWithSigner(ctx, cfg, gwSigner)
}

// NewWithSigner builds the gateway application with the provided signer.
// Useful for callers that manage identity externally, such as integration tests.
func NewWithSigner(ctx context.Context, cfg config.Config, gwSigner sdk.Signer) (*App, error) {
	logger := sdk.NewStdLogger("gateway")

	// Create endorsers and their synchronizers.
	endorsers := make([]core.Endorser, 0, len(cfg.Endorsers))
	endorserSyncs := make([]*network.Synchronizer, 0, len(cfg.Endorsers))
	var firstKVS interface{} // Keep first endorser's KVS for test server
	for i, ecfg := range cfg.Endorsers {
		// Set history size: always 128 for test RPC (snapshot/revert), else default to 2 if not set
		if cfg.Gateway.EnableTestRPC {
			ecfg.Database.HistorySize = 128
		} else if ecfg.Database.HistorySize == 0 {
			ecfg.Database.HistorySize = 2
		}
		end, sync, kvs, err := eapp.NewEndorser(ecfg, cfg.Network, logger, false, cfg.Gateway.EnableTestRPC)
		if err != nil {
			return nil, fmt.Errorf("endorser %d (%s): %w", i, ecfg.Name, err)
		}
		endorsers = append(endorsers, end)
		endorserSyncs = append(endorserSyncs, sync)
		if i == 0 {
			firstKVS = kvs
		}
	}

	return buildApp(ctx, cfg, gwSigner, logger, endorsers, endorserSyncs, firstKVS)
}

// buildApp wires up the gateway from pre-built endorsers. Used by NewWithSigner
// and directly by integration tests that manage their own endorsers.
func buildApp(ctx context.Context, cfg config.Config, gwSigner sdk.Signer, logger sdk.Logger, endorsers []core.Endorser, endorserSyncs []*network.Synchronizer, lightKVS interface{}) (*App, error) {
	ec, err := core.NewEndorsementClient(endorsers, gwSigner, cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to create endorsement client: %w", err)
	}

	orderers := make([]network.OrdererConf, len(cfg.Gateway.Orderers))
	for i, o := range cfg.Gateway.Orderers {
		orderers[i] = o.ToOrdererConf()
	}

	var submitter core.Submitter
	switch cfg.Network.Protocol {
	case "fabric":
		submitter, err = nfab.NewSubmitter(ctx, orderers, gwSigner, 0, logger)
	case "fabric-x", "":
		submitter, err = nfabx.NewSubmitter(ctx, orderers, gwSigner, 0, logger)
	default:
		return nil, fmt.Errorf("unsupported protocol: %q", cfg.Network.Protocol)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create submitter: %w", err)
	}

	chain, err := core.NewChain(cfg.Gateway.Database.ConnString, cfg.Gateway.Database.TriePath, false)
	if err != nil {
		return nil, fmt.Errorf("failed to create chain: %w", err)
	}

	gateway, err := core.New(ec, submitter, chain, cfg.Network.ChainID, cfg.Gateway.WorkerCount, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway: %w", err)
	}

	// Create synchronizer with both chain and gateway as handlers
	// Chain must be called first to persist blocks, then gateway to mark transactions complete
	var gwSync *network.Synchronizer
	switch cfg.Network.Protocol {
	case "fabric":
		gwSync, err = nfab.NewSynchronizer(chain, cfg.Network.Channel, cfg.Gateway.Committer.ToPeerConf(), gwSigner, logger, chain, gateway)
	case "fabric-x", "":
		gwSync, err = nfabx.NewSynchronizer(chain, cfg.Network.Channel, cfg.Gateway.Committer.ToPeerConf(), gwSigner, logger, chain, gateway)
	default:
		return nil, fmt.Errorf("unsupported protocol: %q", cfg.Network.Protocol)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway synchronizer: %w", err)
	}

	// Create RPC server - use test server if explicitly enabled
	var rpcServer *rpc.Server
	if cfg.Gateway.EnableTestRPC {
		// UNSAFE: Test RPC methods enabled - load test accounts
		appLogger.Warn("Test RPC methods enabled (eth_accounts, eth_sendTransaction)")
		appLogger.Warn("Server-side signing is unsafe and should never be used in production")

		testAccountMgr, err := testimpl.LoadTestAccounts(cfg.Gateway.TestAccountsPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load test accounts: %w", err)
		}

		lightKVSExt, ok := lightKVS.(*endorsertestimpl.LightKVSExt)
		if !ok {
			return nil, fmt.Errorf("test RPC enabled but lightKVS is not LightKVSExt")
		}

		// Wrap the chain's store with SnapshotStore for snapshot/revert functionality
		snapshotStore := testimpl.NewSnapshotStore(chain.Store)

		rpcServer, err = testimpl.NewTestServer(gateway, testAccountMgr.Addresses, testAccountMgr.PrivateKeys, lightKVSExt, snapshotStore)
		if err != nil {
			return nil, err
		}
	} else {
		// Production server without test methods
		rpcServer, err = api.NewServer(gateway)
		if err != nil {
			return nil, err
		}
	}

	return &App{
		cfg:           cfg,
		endorserSyncs: endorserSyncs,
		gwSync:        gwSync,
		gateway:       gateway,
		submitter:     submitter,
		chain:         chain,
		rpcServer:     rpcServer,
	}, nil
}

// Run starts the application and blocks until a signal is received or a fatal error occurs.
func (a *App) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	g, gctx := errgroup.WithContext(ctx)

	// Start synchronizers
	for _, sync := range a.endorserSyncs {
		g.Go(func() error { return sync.Start(gctx) })
	}
	g.Go(func() error { return a.gwSync.Start(gctx) })

	// Wait for initial sync before serving traffic
	for i, sync := range a.endorserSyncs {
		if err := waitUntilSynced(gctx, sync, 10*time.Second); err != nil {
			return err
		}
		appLogger.Debugf("endorser %d synced", i)
	}

	// Start gateway worker pool
	appLogger.Debugf("starting gateway with %d workers", a.cfg.Gateway.WorkerCount)
	a.gateway.Start(gctx)

	// Create HTTP server before starting goroutine so Shutdown can safely read a.httpServer
	a.httpServer = api.NewHTTPServer(a.rpcServer, a.cfg.Gateway.Listen)
	g.Go(func() error {
		if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	})

	// Shutdown trigger: fires when any goroutine fails or context is canceled
	g.Go(func() error {
		<-gctx.Done()
		return a.Shutdown()
	})

	return g.Wait()
}

// Shutdown performs graceful shutdown of all application components.
func (a *App) Shutdown() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop accepting new HTTP requests
	if a.httpServer != nil {
		appLogger.Debug("shutting down HTTP server...")
		if err := a.httpServer.Shutdown(shutdownCtx); err != nil {
			appLogger.Warnf("HTTP server shutdown error: %v", err)
		} else {
			appLogger.Debug("HTTP server stopped")
		}
	}

	// Stop gateway workers
	appLogger.Debug("stopping gateway workers...")
	if err := a.gateway.Stop(); err != nil {
		appLogger.Warnf("gateway stop error: %v", err)
	} else {
		appLogger.Debug("gateway workers stopped")
	}

	// Close chain (trie + database)
	appLogger.Debug("closing chain...")
	if err := a.chain.Close(); err != nil {
		appLogger.Warnf("chain close error: %v", err)
	} else {
		appLogger.Debug("chain closed")
	}

	appLogger.Debug("graceful shutdown complete")
	return nil
}

func waitUntilSynced(ctx context.Context, sync *network.Synchronizer, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		if err := sync.Ready(); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return errors.New("timeout waiting for sync")
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil
}
