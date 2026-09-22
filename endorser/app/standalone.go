/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"context"
	"fmt"

	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-sdk/identity"
	"golang.org/x/sync/errgroup"

	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/core"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-evm/endorser/server"
	"github.com/hyperledger/fabric-x-evm/endorser/storage"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	"github.com/hyperledger/fabric-x-evm/synchronizer"
)

var standaloneLogger = flogging.MustGetLogger("endorser.app")

// App runs a standalone endorser process: no gateway, no chain, no HTTP —
// just the embedded endorser, the synchronizer feeding it, and the gRPC
// server other orgs dial.
type App struct {
	cfg          config.Config
	endorser     *core.Endorser
	kvs          storage.KVS
	synchronizer synchronizer.Synchronizer
}

// New builds a standalone endorser App from cfg. cfg.Endorser must be set;
// this is the endorser-only counterpart to gateway/app.New, used when
// cfg.Gateway is nil.
func New(ctx context.Context, cfg config.Config) (*App, error) {
	if cfg.Endorser == nil {
		return nil, fmt.Errorf("endorser config is required")
	}
	ecfg := *cfg.Endorser

	signer, err := identity.SignerFromMSP(ecfg.Identity.MSPDir, ecfg.Identity.MspID)
	if err != nil {
		return nil, fmt.Errorf("failed to create endorser signer: %w", err)
	}

	evmConfig := execution.EVMConfig{
		ChainConfig: common.BuildChainConfig(cfg.Network.ChainID),
		MaxTxGas:    cfg.Network.MaxTxGas,
		DebugLogs:   ecfg.DebugLogs,
	}

	end, kvs, _, err := NewEndorserCore(ecfg.Database, cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.Protocol, signer, evmConfig, false, ecfg)
	if err != nil {
		return nil, fmt.Errorf("endorser (%s): %w", ecfg.Name, err)
	}

	logger := flogging.MustGetLogger("endorser-" + ecfg.Name)
	// The synchronizer's sole handler is this endorser's own KVS — no chain,
	// no gateway to feed, unlike a gateway's process-wide synchronizer.
	sync, err := synchronizer.New(cfg.Network.Protocol, kvs, cfg.Network.Channel, cfg.Network.Namespace, cfg.Committer.ToPeerConf(), signer, logger, kvs)
	if err != nil {
		return nil, fmt.Errorf("failed to create synchronizer: %w", err)
	}

	return &App{
		cfg:          cfg,
		endorser:     end,
		kvs:          kvs,
		synchronizer: sync,
	}, nil
}

// Run starts the application and blocks until a signal is received or a fatal error occurs.
func (a *App) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error { return a.synchronizer.Start(gctx) })

	// Wait for initial sync before serving traffic.
	if err := synchronizer.WaitUntilSynced(gctx, a.synchronizer, a.cfg.Synchronizer.SyncTimeout()); err != nil {
		return err
	}

	// Validate() requires endorser.server for a standalone process — this
	// path is pointless without it — so this is never nil here.
	g.Go(func() error {
		return server.ServeEndorser(gctx, a.endorser, a.cfg.Endorser.Server)
	})

	// Shutdown trigger: fires when any goroutine fails or context is canceled.
	g.Go(func() error {
		<-gctx.Done()
		return a.Shutdown()
	})

	return g.Wait()
}

// Shutdown performs graceful shutdown of all application components.
func (a *App) Shutdown() error {
	standaloneLogger.Debug("closing endorser kvs...")
	if err := a.kvs.Close(); err != nil {
		standaloneLogger.Warnf("kvs close error: %v", err)
	} else {
		standaloneLogger.Debug("kvs closed")
	}

	standaloneLogger.Debug("graceful shutdown complete")
	return nil
}
