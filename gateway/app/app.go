/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package app

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"golang.org/x/sync/errgroup"
	_ "modernc.org/sqlite"

	"github.com/hyperledger/fabric-x-evm/endorser"
	eapi "github.com/hyperledger/fabric-x-evm/endorser/api"
	eapp "github.com/hyperledger/fabric-x-evm/endorser/app"
	econf "github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/gateway/storage"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/blocks/fabric"
	"github.com/hyperledger/fabric-x-sdk/identity"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfab "github.com/hyperledger/fabric-x-sdk/network/fabric"
	_ "modernc.org/sqlite"
)

// App represents the gateway application with all its components.
type App struct {
	cfg           config.Config
	endorserSyncs []*network.Synchronizer
	gwSync        *network.Synchronizer
	submitter     core.Submitter
	gwDB          *sql.DB
	rpcServer     *rpc.Server
	httpServer    *http.Server
}

// New creates a new gateway application from the provided configuration.
func New(cfg config.Config) (*App, error) {
	logger := sdk.NewStdLogger("gateway")

	// create endorsers and their synchronizers
	endorsers := make([]*endorser.Endorser, 0, len(cfg.Endorsers))
	endorserSyncs := make([]*network.Synchronizer, 0, len(cfg.Endorsers))

	for i, ecfg := range cfg.Endorsers {
		net := econf.Network{
			Channel:   cfg.Network.Channel,
			Namespace: cfg.Network.Namespace,
			NsVersion: cfg.Network.NsVersion,
			ChainID:   cfg.Network.ChainID,
		}

		end, sync, err := eapp.NewEndorser(ecfg, net, logger, false)
		if err != nil {
			return nil, fmt.Errorf("endorser %d (%s): %w", i, ecfg.Name, err)
		}
		endorsers = append(endorsers, end)
		endorserSyncs = append(endorserSyncs, sync)
	}

	// wrap endorsers in API for gateway use
	des, err := eapi.NewFabricDeserializer(cfg.Endorsers[0].MspDir, cfg.Endorsers[0].MspID)
	if err != nil {
		return nil, err
	}
	endorserAPIs := make([]core.Endorser, 0, len(endorsers))
	for _, end := range endorsers {
		endAPI := eapi.New(cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion, des, end, nil)
		endorserAPIs = append(endorserAPIs, endAPI)
	}

	gwSigner, err := identity.SignerFromMSP(cfg.Gateway.SignerMSPDir, cfg.Gateway.SignerMSPID)
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway signer: %w", err)
	}

	ec, err := core.NewEndorsementClient(endorserAPIs, gwSigner, cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create endorsement client: %w", err)
	}

	gwDB, err := newSqlite(cfg.Gateway.DbConnStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway database: %w", err)
	}

	orderers := make([]network.OrdererConf, len(cfg.Gateway.Orderers))
	for i, o := range cfg.Gateway.Orderers {
		orderers[i] = network.OrdererConf{
			Address: o.Address,
			TLSPath: o.TLSPath,
		}
	}

	submitter, err := network.NewSubmitter(orderers, nfab.NewTxPackager(gwSigner), cfg.Gateway.SubmitWaitTime)
	if err != nil {
		return nil, fmt.Errorf("failed to create submitter: %w", err)
	}

	blockStore := storage.NewStore(gwDB)
	if err := blockStore.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize block store: %w", err)
	}

	gateway, err := core.New(ec, submitter, blockStore, cfg.Network.ChainID)
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway: %w", err)
	}

	processor := blocks.NewProcessor(fabric.NewBlockParser(logger), []blocks.BlockHandler{core.NewEthBlockPersister(blockStore)})
	gwSync, err := network.NewSynchronizer(blockStore, cfg.Network.Channel, cfg.Gateway.SyncPeerAddr, cfg.Gateway.SyncPeerTLS, gwSigner, processor, logger)
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
		submitter:     submitter,
		gwDB:          gwDB,
		rpcServer:     rpcServer,
		httpServer:    nil, // Will be set when HTTP server starts
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
		if err := sync.WaitUntilSynced(gctx, a.cfg.Gateway.SyncTimeout); err != nil {
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
	// Create a separate timeout context for shutdown operations
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

	// Close database
	log.Println("closing database...")
	if err := a.gwDB.Close(); err != nil {
		log.Printf("database close error: %v", err)
	} else {
		log.Println("database closed")
	}

	log.Println("graceful shutdown complete")
	return nil
}

// newSqlite creates a new SQLite database connection with appropriate settings.
func newSqlite(connStr string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	return db, nil
}
