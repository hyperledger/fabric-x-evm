/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/identity"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfab "github.com/hyperledger/fabric-x-sdk/network/fabric"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
	"golang.org/x/sync/errgroup"
	_ "modernc.org/sqlite"

	"github.com/hyperledger/fabric-x-evm/endorser"
	eapi "github.com/hyperledger/fabric-x-evm/endorser/api"
	eapp "github.com/hyperledger/fabric-x-evm/endorser/app"
	"github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
)

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
func New(cfg config.Config) (*App, error) {
	gwSigner, err := identity.SignerFromMSP(cfg.Gateway.Identity.MSPDir, cfg.Gateway.Identity.MspID)
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway signer: %w", err)
	}
	return NewWithSigner(cfg, gwSigner)
}

// NewWithSigner builds the gateway application with the provided signer.
// Useful for callers that manage identity externally, such as integration tests.
func NewWithSigner(cfg config.Config, gwSigner sdk.Signer) (*App, error) {
	logger := sdk.NewStdLogger("gateway")

	// Create endorsers and their synchronizers.
	endorsers := make([]*endorser.Endorser, 0, len(cfg.Endorsers))
	endorserSyncs := make([]*network.Synchronizer, 0, len(cfg.Endorsers))
	for i, ecfg := range cfg.Endorsers {
		end, sync, err := eapp.NewEndorser(ecfg, cfg.Network, logger, false)
		if err != nil {
			return nil, fmt.Errorf("endorser %d (%s): %w", i, ecfg.Name, err)
		}
		endorsers = append(endorsers, end)
		endorserSyncs = append(endorserSyncs, sync)
	}

	return buildApp(cfg, gwSigner, logger, endorsers, endorserSyncs)
}

// buildApp wires up the gateway from pre-built endorsers. Used by NewWithSigner
// and directly by integration tests that manage their own endorsers.
func buildApp(cfg config.Config, gwSigner sdk.Signer, logger sdk.Logger, endorsers []*endorser.Endorser, endorserSyncs []*network.Synchronizer) (*App, error) {
	endorserAPIs := make([]core.Endorser, len(endorsers))
	for i, end := range endorsers {
		endorserAPIs[i] = eapi.New(cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion, end, nil)
	}

	ec, err := core.NewEndorsementClient(endorserAPIs, gwSigner, cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion, nil)
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
		submitter, err = nfab.NewSubmitter(orderers, gwSigner, cfg.Gateway.SubmitWaitTime, logger)
	case "fabric-x", "":
		submitter, err = nfabx.NewSubmitter(orderers, gwSigner, cfg.Gateway.SubmitWaitTime, logger)
	default:
		return nil, fmt.Errorf("unsupported protocol: %q", cfg.Network.Protocol)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create submitter: %w", err)
	}

	chain, err := core.NewChain(cfg.Gateway.DbConnStr, cfg.Gateway.TrieDBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create chain: %w", err)
	}

	gateway, err := core.New(ec, submitter, chain, cfg.Network.ChainID)
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway: %w", err)
	}

	var gwSync *network.Synchronizer
	switch cfg.Network.Protocol {
	case "fabric":
		gwSync, err = nfab.NewSynchronizer(chain, cfg.Network.Channel, cfg.Gateway.Committer.ToPeerConf(), gwSigner, logger, chain)
	case "fabric-x", "":
		gwSync, err = nfabx.NewSynchronizer(chain, cfg.Network.Channel, cfg.Gateway.Committer.ToPeerConf(), gwSigner, logger, chain)
	default:
		return nil, fmt.Errorf("unsupported protocol: %q", cfg.Network.Protocol)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway synchronizer: %w", err)
	}

	rpcServer, err := api.NewServer(gateway)
	if err != nil {
		return nil, err
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
		log.Printf("endorser %d synced", i)
	}

	// Create HTTP server before starting goroutine so Shutdown can safely read a.httpServer
	a.httpServer = api.NewHTTPServer(a.rpcServer, a.cfg.Server.Bind)
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

	// Signal → cancel → triggers shutdown goroutine
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case sig := <-sigCh:
			log.Printf("signal %v received, initiating graceful shutdown...", sig)
			cancel()
		case <-gctx.Done():
		}
	}()

	return g.Wait()
}

// Shutdown performs graceful shutdown of all application components.
func (a *App) Shutdown() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop accepting new HTTP requests
	if a.httpServer != nil {
		log.Println("shutting down HTTP server...")
		if err := a.httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		} else {
			log.Println("HTTP server stopped")
		}
	}

	// Close submitter
	log.Println("closing submitter...")
	if err := a.submitter.Close(); err != nil {
		log.Printf("submitter close error: %v", err)
	} else {
		log.Println("submitter closed")
	}

	// Close chain (trie + database)
	log.Println("closing chain...")
	if err := a.chain.Close(); err != nil {
		log.Printf("chain close error: %v", err)
	} else {
		log.Println("chain closed")
	}

	log.Println("graceful shutdown complete")
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
