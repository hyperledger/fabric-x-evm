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
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/identity"
	"github.com/hyperledger/fabric-x-sdk/network"
	"golang.org/x/sync/errgroup"
	_ "modernc.org/sqlite"

	"github.com/hyperledger/fabric-x-evm/common"
	eapi "github.com/hyperledger/fabric-x-evm/endorser/api"
	eapp "github.com/hyperledger/fabric-x-evm/endorser/app"
	eclient "github.com/hyperledger/fabric-x-evm/endorser/client"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	estorage "github.com/hyperledger/fabric-x-evm/endorser/storage"
	"github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/api/filters"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/gateway/storage"
	"github.com/hyperledger/fabric-x-evm/gateway/testimpl"
)

var appLogger = flogging.MustGetLogger("gateway.app")

// App represents the gateway application with all its components.
type App struct {
	cfg           config.Config
	endorserConns []*eclient.Client // set only in split deployment; closed on Shutdown
	synchronizer  Synchronizer
	gateway       *core.Gateway
	chain         *core.Chain
	blockFeed     *filters.BlockFeed
	rpcServer     *rpc.Server
	httpServer    *http.Server
}

// Gateway returns the inner gateway, e.g. for use in tests.
func (a *App) Gateway() *core.Gateway { return a.gateway }

// EnsureGenesisBlock inserts an empty block 0 if the chain has no blocks yet.
func (a *App) EnsureGenesisBlock(ctx context.Context) error {
	return a.chain.EnsureGenesisBlock(ctx)
}

// New creates a new gateway application from the provided configuration.
// It loads the gateway signer from the MSP directory configured in cfg. Test RPC is never enabled.
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
	return newApp(ctx, cfg, gwSigner, false, "")
}

// NewTestNodeWithConfig builds a gateway against a real ordering backend described by cfg,
// with test RPC forced on.
func NewTestNodeWithConfig(ctx context.Context, cfg config.Config, testAccountsPath string) (*App, error) {
	gwSigner, err := identity.SignerFromMSP(cfg.Gateway.Identity.MSPDir, cfg.Gateway.Identity.MspID)
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway signer: %w", err)
	}
	return newApp(ctx, cfg, gwSigner, true, testAccountsPath)
}

func newApp(ctx context.Context, cfg config.Config, gwSigner sdk.Signer, enableTestRPC bool, testAccountsPath string) (*App, error) {
	logger := sdk.NewStdLogger("gateway")

	if len(cfg.Gateway.Endorsers) > 0 {
		if enableTestRPC {
			return nil, fmt.Errorf("test RPC is not supported with split deployment (gateway.endorsers configured)")
		}
		return newSplitApp(ctx, cfg, gwSigner, logger, enableTestRPC, testAccountsPath)
	}

	if cfg.Endorser == nil {
		return nil, fmt.Errorf("one of endorser or gateway.endorsers is required")
	}
	ecfg := *cfg.Endorser
	// Test RPC needs a large sequential history window (see testnode); production
	// only needs a couple of snapshots for the synchronizer.
	if enableTestRPC {
		ecfg.Database.HistorySize = 16384
	} else if ecfg.Database.HistorySize == 0 {
		ecfg.Database.HistorySize = 2
	}
	eSigner, err := identity.SignerFromMSP(ecfg.Identity.MSPDir, ecfg.Identity.MspID)
	if err != nil {
		return nil, fmt.Errorf("failed to create signer: %w", err)
	}

	evmConfig := execution.EVMConfig{
		ChainConfig: common.BuildChainConfig(cfg.Network.ChainID),
		MaxTxGas:    cfg.Network.MaxTxGas,
		DebugLogs:   ecfg.DebugLogs,
	}
	end, kvs, _, err := eapp.NewEndorserCore(ecfg.Database, cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.Protocol, eSigner, evmConfig, enableTestRPC, ecfg)
	if err != nil {
		return nil, fmt.Errorf("endorser (%s): %w", ecfg.Name, err)
	}

	return buildApp(ctx, cfg, gwSigner, logger, []eapi.Service{end}, kvs, enableTestRPC, testAccountsPath, kvs)
}

// newSplitApp builds the gateway in split-deployment mode: every endorsers is
// dialed over gRPC (co-located ones on localhost, remote ones on their configured address)
// instead of built in-process
func newSplitApp(ctx context.Context, cfg config.Config, gwSigner sdk.Signer, logger sdk.Logger, enableTestRPC bool, testAccountsPath string) (*App, error) {
	conns := make([]*eclient.Client, 0, len(cfg.Gateway.Endorsers))
	closeAll := func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}
	for i, ecfg := range cfg.Gateway.Endorsers {
		cl, err := eclient.Dial(ecfg)
		if err != nil {
			closeAll()
			return nil, fmt.Errorf("dial endorser %d: %w", i, err)
		}
		conns = append(conns, cl)
	}

	endorsers := make([]eapi.Service, len(conns))
	for i, c := range conns {
		endorsers[i] = c
	}

	app, err := buildApp(ctx, cfg, gwSigner, logger, endorsers, nil, enableTestRPC, testAccountsPath)
	if err != nil {
		closeAll()
		return nil, err
	}
	app.endorserConns = conns
	return app, nil
}

// buildApp wires up the gateway from pre-built endorsers.
// extraHandlers are prepended to the synchronizer handler list, ahead of chain/gateway.
func buildApp(ctx context.Context, cfg config.Config, gwSigner sdk.Signer, logger sdk.Logger, endorsers []eapi.Service, lightKVS estorage.KVS, enableTestRPC bool, testAccountsPath string, extraHandlers ...blocks.BlockHandler) (*App, error) {
	orderers := make([]network.OrdererConf, len(cfg.Gateway.Orderers))
	for i, o := range cfg.Gateway.Orderers {
		orderers[i] = o.ToOrdererConf()
	}

	// Create multiple submitter instances for parallel submission (one per worker)
	submitters, err := NewNetworkSubmitters(ctx, cfg.Network.Protocol, orderers, gwSigner, cfg.Gateway.SubmitterCount, logger)
	if err != nil {
		return nil, err
	}

	chain, err := core.NewChain(cfg.Gateway.Database.ConnString, cfg.Gateway.Database.TriePath, false)
	if err != nil {
		return nil, fmt.Errorf("failed to create chain: %w", err)
	}

	// Gateway owns the BatchSubmitter and will handle its lifecycle
	gateway, err := BuildGateway(ctx, endorsers, gwSigner, cfg.Network, chain, submitters, cfg.Gateway.SubmitterCount, cfg.Gateway.WorkerCount, nil, cfg.Gateway.EndorsementChanSize, 0)
	if err != nil {
		return nil, err
	}

	filterAPI := filters.NewFilterAPI(gateway)
	blockFeed := filters.NewBlockFeed(filterAPI)

	// Chain must be called before gateway, to persist blocks before marking transactions complete.
	// blockFeed updates filter state synchronously in Handle, like the other handlers.
	handlers := append(extraHandlers, blockFeed, chain, gateway)
	synchronizer, err := NewSynchronizer(cfg.Network.Protocol, chain, cfg.Network.Channel, cfg.Network.Namespace, cfg.Committer.ToPeerConf(), gwSigner, logger, handlers...)
	if err != nil {
		blockFeed.Close()
		return nil, fmt.Errorf("failed to create synchronizer: %w", err)
	}

	// Create RPC server - use test server if explicitly enabled
	var rpcServer *rpc.Server
	if enableTestRPC {
		// UNSAFE: Test RPC methods enabled - load test accounts
		appLogger.Warn("Test RPC methods enabled (eth_accounts, eth_sendTransaction)")
		appLogger.Warn("Server-side signing is unsafe and should never be used in production")

		testAccountMgr, err := testimpl.LoadTestAccounts(testAccountsPath)
		if err != nil {
			blockFeed.Close()
			return nil, fmt.Errorf("failed to load test accounts: %w", err)
		}

		// Pre-fund known Hardhat test EOAs so value transfers pass the balance
		// check (issue #254). Test RPC / testnode only. Production accounts stay at zero.
		if err := testimpl.FundTestAccounts(ctx, lightKVS, cfg.Network.Namespace, testAccountMgr.Addresses, testimpl.DefaultTestAccountBalance); err != nil {
			blockFeed.Close()
			return nil, fmt.Errorf("failed to fund test accounts: %w", err)
		}
		appLogger.Infof("Funded %d test accounts with %s wei each", len(testAccountMgr.Addresses), testimpl.DefaultTestAccountBalance.String())

		revertibleKVS, ok := lightKVS.(estorage.Revertible)
		if !ok {
			blockFeed.Close()
			return nil, fmt.Errorf("test RPC enabled but lightKVS is not Revertible")
		}

		// Wrap the chain's store with SnapshotStore for snapshot/revert functionality
		snapshotStore := storage.NewSnapshotStore(chain.Store)

		rpcServer, err = testimpl.NewTestServer(gateway, testAccountMgr.Addresses, testAccountMgr.PrivateKeys, revertibleKVS, snapshotStore, gateway.TxQueue, filterAPI)
		if err != nil {
			blockFeed.Close()
			return nil, err
		}
	} else {
		// Production server without test methods
		rpcServer, err = api.NewServer(gateway, filterAPI)
		if err != nil {
			blockFeed.Close()
			return nil, err
		}
	}

	return &App{
		cfg:          cfg,
		synchronizer: synchronizer,
		gateway:      gateway,
		chain:        chain,
		blockFeed:    blockFeed,
		rpcServer:    rpcServer,
	}, nil
}

// Run starts the application and blocks until a signal is received or a fatal error occurs.
func (a *App) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error { return a.synchronizer.Start(gctx) })

	// Wait for initial sync before serving traffic
	if err := WaitUntilSynced(gctx, a.synchronizer, a.cfg.Synchronizer.SyncTimeout()); err != nil {
		return err
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

	// Stop gateway workers and batch submitter
	appLogger.Debug("stopping gateway...")
	if err := a.gateway.Stop(); err != nil {
		appLogger.Warnf("gateway stop error: %v", err)
	} else {
		appLogger.Debug("gateway stopped")
	}

	// Close chain (trie + database)
	appLogger.Debug("closing chain...")
	if err := a.chain.Close(); err != nil {
		appLogger.Warnf("chain close error: %v", err)
	} else {
		appLogger.Debug("chain closed")
	}

	if a.blockFeed != nil {
		appLogger.Debug("closing block feed...")
		a.blockFeed.Close()
		appLogger.Debug("block feed closed")
	}

	// Close dialed endorser connections (split deployment only)
	for _, c := range a.endorserConns {
		if err := c.Close(); err != nil {
			appLogger.Warnf("endorser connection close error: %v", err)
		}
	}

	appLogger.Debug("graceful shutdown complete")
	return nil
}

// WaitUntilSynced blocks until sync reports Ready or timeout elapses, polling every 100ms.
func WaitUntilSynced(ctx context.Context, sync Synchronizer, timeout time.Duration) error {
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
