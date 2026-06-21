/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	mathrand "math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	fabriccommon "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/msp"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-common/api/msppb"
	"github.com/hyperledger/fabric-x-common/protoutil"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser"
	econf "github.com/hyperledger/fabric-x-evm/endorser/config"
	gwapi "github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	bfab "github.com/hyperledger/fabric-x-sdk/blocks/fabric"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	efab "github.com/hyperledger/fabric-x-sdk/endorsement/fabric"
	efabx "github.com/hyperledger/fabric-x-sdk/endorsement/fabricx"
	"github.com/hyperledger/fabric-x-sdk/fabrictest"
	"github.com/hyperledger/fabric-x-sdk/identity"
	"github.com/hyperledger/fabric-x-sdk/local"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfab "github.com/hyperledger/fabric-x-sdk/network/fabric"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
	"github.com/hyperledger/fabric-x-sdk/notification"
	"github.com/hyperledger/fabric-x-sdk/state"
)

// GetERC20BalanceSlot computes the storage slot for a balance in an ERC-20 mapping(address => uint256).
// This uses the Solidity storage layout: keccak256(abi.encodePacked(address, mappingPosition))
func GetERC20BalanceSlot(account ethcommon.Address, mappingPosition uint64) ethcommon.Hash {
	// Concatenate: address (32 bytes) + mapping position (32 bytes)
	data := append(
		ethcommon.LeftPadBytes(account.Bytes(), 32),
		ethcommon.LeftPadBytes(new(big.Int).SetUint64(mappingPosition).Bytes(), 32)...,
	)
	return crypto.Keccak256Hash(data)
}

type localSigner struct{}

func (localSigner) Sign(msg []byte) ([]byte, error) {
	return []byte("signature"), nil
}

func (localSigner) Serialize() ([]byte, error) {
	return []byte("serialised identity"), nil
}

// NewStatePrimer returns a reset StatePrimer ready for a new batch of state operations.
// Can be called at any time during tests.
//
// Example usage:
//
//	primer, err := th.NewStatePrimer()
//	err = primer.SetNonce(addr1, 5).SetCode(addr2, contractCode).Commit(ctx)
func (th *TestHarness) NewStatePrimer() (*StatePrimer, error) {
	return th.Primer.Reset()
}

// PrimeStateFromJSON builds a proposal that contains a RWSet derived from the contents of
// `jsonFilePath` as the chaincode results, creates a ProposalResponses signed by the given
// endorsers and submits them via the submitter. This causes Fabric peers to apply the state
// through normal commit flow.
//
// This is a convenience wrapper around NewStatePrimer().LoadFromJSON().Commit().
func (th *TestHarness) PrimeStateFromJSON(ctx context.Context, jsonFilePath string, wait bool) error {
	// bail if no file is given
	if jsonFilePath == "" {
		return nil
	}

	primer, err := th.NewStatePrimer()
	if err != nil {
		return err
	}
	primer, err = primer.LoadFromJSON(jsonFilePath)
	if err != nil {
		return err
	}
	return primer.Commit(ctx, wait)
}

// buildTestHarness is the shared implementation for all test harness constructors.
// It builds endorsers, a gateway, and primes state.
//
// The gateway signer and identity deserializer are derived from cfg:
//   - cfg.Gateway.SignerMSPDir set → MSP-based signer; empty → local mock
//   - cfg.Endorsers[0].MspDir set → FabricDeserializer; empty → local mock
//
// Sync goroutines are started in the background using ctx. The returned synchronizers
// can be used by callers that need to wait for the initial sync to complete.
//
// If useNotifications is true, uses NotificationDispatcher + MemoryStore instead of
// Synchronizer + Chain. This is intended for fabric-x performance testing.
func buildTestHarness(t *testing.T, logger sdk.Logger, cfg config.Config, evmConfig endorser.EVMConfig, primeDBPath string, bypass bool, ends []core.Endorser, dbs []endorser.KVS, builders []endorsement.Builder, txQueue core.TxQueueInterface, useNotifications bool) (*TestHarness, *network.Synchronizer, error) {
	return buildTestHarnessWithExtraHandler(t, logger, cfg, evmConfig, primeDBPath, bypass, ends, dbs, builders, txQueue, useNotifications, nil)
}

// buildTestHarnessWithExtraHandler is like buildTestHarness but accepts an optional extra TxHandler
// that will be inserted into the notification handler chain right before the cleanup handler.
func buildTestHarnessWithExtraHandler(t *testing.T, logger sdk.Logger, cfg config.Config, evmConfig endorser.EVMConfig, primeDBPath string, bypass bool, ends []core.Endorser, dbs []endorser.KVS, builders []endorsement.Builder, txQueue core.TxQueueInterface, useNotifications bool, extraHandler core.TxHandler) (*TestHarness, *network.Synchronizer, error) {
	// Build gateway signer.
	var gwSigner sdk.Signer
	if cfg.Gateway.Identity.MSPDir != "" {
		var err error
		gwSigner, err = identity.SignerFromMSP(cfg.Gateway.Identity.MSPDir, cfg.Gateway.Identity.MspID)
		if err != nil {
			return nil, nil, err
		}
	} else {
		gwSigner = localSigner{}
	}

	ec, err := core.NewEndorsementClient(ends, gwSigner, cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion)
	if err != nil {
		return nil, nil, err
	}

	chain, err := core.NewChain(cfg.Gateway.Database.ConnString, cfg.Gateway.Database.TriePath, false)
	if err != nil {
		return nil, nil, err
	}
	if !useNotifications {
		t.Cleanup(func() { chain.Close() })
	}

	// Build submitters (one per worker for parallel submission)
	orderers := make([]network.OrdererConf, len(cfg.Gateway.Orderers))
	for i, o := range cfg.Gateway.Orderers {
		orderers[i] = o.ToOrdererConf()
	}

	submitterCount := cfg.Gateway.SubmitterCount
	if submitterCount <= 0 {
		submitterCount = core.DefaultNumWorkers
	}
	submitters := make([]core.Submitter, submitterCount)
	var sync *network.Synchronizer
	var err1 error

	if bypass {
		// Use local submitters for bypass mode (no network communication)
		for i := 0; i < submitterCount; i++ {
			submitters[i] = local.NewLocalSubmitter(dbs[0], cfg.Network.Channel, cfg.Network.Namespace, nfab.NewTxPackager(gwSigner), bfab.NewBlockParser(logger), false)
		}
	} else {
		// Create network submitters
		for i := 0; i < submitterCount; i++ {
			switch cfg.Network.Protocol {
			case "fabric":
				submitters[i], err1 = nfab.NewSubmitter(t.Context(), orderers, gwSigner, 0, logger)
			case "fabric-x", "":
				submitters[i], err1 = nfabx.NewSubmitter(t.Context(), orderers, gwSigner, 0, logger)
			default:
				return nil, nil, fmt.Errorf("unsupported protocol: %q", cfg.Network.Protocol)
			}
			if err1 != nil {
				return nil, nil, fmt.Errorf("failed to create submitter %d: %w", i, err1)
			}
		}
	}

	// Create BatchSubmitter infrastructure
	endorsementChan := make(chan sdk.Endorsement, 1000)
	batchSubmitter := core.NewBatchSubmitter(submitters, endorsementChan, cfg.Gateway.SubmitterCount)

	batchSubmitter.Start(t.Context())

	// Create gateway before synchronizer so we can register it as a handler
	// Gateway owns the BatchSubmitter and will handle its lifecycle
	gw, err := core.New(ec, batchSubmitter, chain, cfg.Network.ChainID, cfg.Gateway.WorkerCount, txQueue, endorsementChan)
	if err != nil {
		return nil, nil, err
	}

	// Create synchronizer with handlers (endorsers, chain, and gateway) - only for non-bypass mode
	if !bypass {
		handlers := make([]blocks.BlockHandler, 0, len(dbs)+2)
		for _, db := range dbs {
			handlers = append(handlers, db)
		}
		// Add chain before gateway to ensure blocks are persisted before marking transactions complete
		handlers = append(handlers, chain)
		if !useNotifications {
			handlers = append(handlers, gw)
		}

		switch cfg.Network.Protocol {
		case "fabric":
			sync, err = nfab.NewSynchronizer(chain, cfg.Network.Channel, cfg.Gateway.Committer.ToPeerConf(), gwSigner, logger, handlers...)
		case "fabric-x", "":
			sync, err = nfabx.NewSynchronizer(chain, cfg.Network.Channel, cfg.Gateway.Committer.ToPeerConf(), gwSigner, logger, handlers...)
		default:
			return nil, nil, fmt.Errorf("unsupported protocol: %q", cfg.Network.Protocol)
		}
		if err != nil {
			return nil, nil, err
		}

		if useNotifications {
			// HYBRID MODE: Use synchronizer to catch up, then switch to notifications
			syncCtx, syncCancel := context.WithCancel(t.Context())
			syncDone := make(chan struct{})
			go func() {
				defer close(syncDone)
				if err := sync.Start(syncCtx); err != nil && syncCtx.Err() == nil {
					logger.Errorf("synchronizer error during catchup: %v", err)
				}
			}()

			logger.Infof("Waiting for synchronizer to catch up...")
			waitUntilSynced(t, sync, 60*time.Second)
			logger.Infof("Synchronizer caught up - stopping and switching to notifications")

			syncCancel()
			<-syncDone
			chain.Close()
			logger.Infof("Synchronizer stopped cleanly")

			// Set up AllTxStreamer notification system
			txHandlers := make([]core.TxHandler, 0, len(dbs)+2)
			for _, db := range dbs {
				txHandlers = append(txHandlers, db.(core.TxHandler))
			}
			txHandlers = append(txHandlers, gw.TxQueue.(core.TxHandler))
			if extraHandler != nil {
				txHandlers = append(txHandlers, extraHandler)
			}

			dispatcher := core.NewAllTxBatchDispatcher(txHandlers...)

			if cfg.Network.Protocol == "fabric-x" || cfg.Network.Protocol == "" {
				peer, err := nfabx.NewPeer(cfg.Gateway.Committer.ToPeerConf(), cfg.Network.Channel, gwSigner)
				if err != nil {
					return nil, nil, fmt.Errorf("create notification peer: %w", err)
				}
				streamer := notification.NewAllTxStreamer(peer, []notification.AllTxHandler{dispatcher}, logger)
				go func() {
					req := &notification.StreamAllRequest{
						FilterNamespaces:     []string{cfg.Network.Namespace},
						IncludeReadWriteSets: true,
						IncludeMetadata:      true,
					}
					if err := streamer.Stream(t.Context(), req); err != nil && t.Context().Err() == nil {
						logger.Errorf("AllTxStreamer error: %v", err)
					}
				}()
				logger.Infof("AllTxStreamer active")
			}

			sync = nil
		} else {
			go func() error { return sync.Start(t.Context()) }()
		}
	}

	// Start gateway worker pool
	gw.Start(t.Context())
	t.Cleanup(func() { gw.Stop() })

	// Create state primer (use first submitter)
	primer, err := NewStatePrimer(gw, submitters[0], dbs[0], cfg.Network.Namespace, gwSigner, builders, cfg.Network.Channel, cfg.Network.NsVersion, cfg.Network.Protocol == "fabric-x")
	if err != nil {
		return nil, nil, err
	}

	// Convert orderer configs
	ordererConfigs := make([]network.OrdererConf, len(cfg.Gateway.Orderers))
	for i, o := range cfg.Gateway.Orderers {
		ordererConfigs[i] = o.ToOrdererConf()
	}

	th := &TestHarness{
		Gateways:       []*core.Gateway{gw},
		endorsers:      ends,
		ethChainConfig: evmConfig.ChainConfig,
		Primer:         primer,
		DBs:            dbs,
		OrdererConfigs: ordererConfigs,
	}

	if err := th.PrimeStateFromJSON(t.Context(), primeDBPath, !bypass); err != nil {
		return nil, nil, err
	}

	return th, sync, nil
}

// applyConfigOverrides applies overrides from a map to a config struct using reflection.
// Keys use dot notation like "Gateway.WorkerCount" to specify nested fields.
func applyConfigOverrides(cfg *config.Config, overrides map[string]any) error {
	for key, value := range overrides {
		parts := strings.Split(key, ".")
		if len(parts) == 0 {
			return fmt.Errorf("invalid config key: %s", key)
		}

		v := reflect.ValueOf(cfg).Elem()
		for i, part := range parts {
			field := v.FieldByName(part)
			if !field.IsValid() {
				return fmt.Errorf("invalid config field: %s", key)
			}
			if i == len(parts)-1 {
				// Last part - set the value
				if !field.CanSet() {
					return fmt.Errorf("cannot set config field: %s", key)
				}
				val := reflect.ValueOf(value)
				if !val.Type().AssignableTo(field.Type()) {
					return fmt.Errorf("type mismatch for %s: expected %s, got %s", key, field.Type(), val.Type())
				}
				field.Set(val)
			} else {
				// Intermediate part - navigate deeper
				if field.Kind() != reflect.Struct {
					return fmt.Errorf("cannot navigate through non-struct field: %s", key)
				}
				v = field
			}
		}
	}
	return nil
}

// EndorserFactory is a function that creates an endorser along with its dependencies.
// It returns core.Endorser interface which both *endorser.Endorser and *testimpl.EndorserWrapper implement.
type EndorserFactory func(t *testing.T, ecfg econf.Endorser, channel, namespace string, evmConfig endorser.EVMConfig, protocol string) (endorser.KVS, endorsement.Builder, core.Endorser)

// buildEndorsers creates endorsers using the provided factory function.
func buildEndorsers(t *testing.T, cfg config.Config, evmConfig endorser.EVMConfig, factory EndorserFactory) ([]endorser.KVS, []endorsement.Builder, []core.Endorser) {
	dbs := make([]endorser.KVS, len(cfg.Endorsers))
	builders := make([]endorsement.Builder, len(cfg.Endorsers))
	ends := make([]core.Endorser, len(cfg.Endorsers))
	for i, ecfg := range cfg.Endorsers {
		dbs[i], builders[i], ends[i] = factory(t, ecfg, cfg.Network.Channel, cfg.Network.Namespace, evmConfig, cfg.Network.Protocol)
	}
	return dbs, builders, ends
}

// defaultEndorserFactory creates regular endorsers without wrapping.
func defaultEndorserFactory(t *testing.T, ecfg econf.Endorser, channel, namespace string, evmConfig endorser.EVMConfig, protocol string) (endorser.KVS, endorsement.Builder, core.Endorser) {
	db, builder, end := NewEndorser(t, ecfg, channel, namespace, evmConfig, protocol)
	return db, builder, end
}

// newLocalTestHarness commits updates directly to the DB, bypassing peers and orderers.
// Exported for use by eth-tests package.
func NewLocalTestHarness(t *testing.T, logger sdk.Logger, evmConfig endorser.EVMConfig, primeDbPath, networkType string, configOverrides map[string]any) (*TestHarness, error) {
	return NewLocalTestHarnessWithFactory(t, logger, evmConfig, primeDbPath, networkType, configOverrides, defaultEndorserFactory)
}

// NewLocalTestHarnessWithFactory is like NewLocalTestHarness but allows custom endorser creation.
// NewLocalTestHarnessWithFactory creates a test harness with a custom endorser factory.
// Uses the default TxQueue implementation.
func NewLocalTestHarnessWithFactory(t *testing.T, logger sdk.Logger, evmConfig endorser.EVMConfig, primeDbPath, networkType string, configOverrides map[string]any, factory EndorserFactory) (*TestHarness, error) {
	return NewLocalTestHarnessWithFactoryAndTxQueue(t, logger, evmConfig, primeDbPath, networkType, configOverrides, factory, nil)
}

// NewLocalTestHarnessWithFactoryAndTxQueue creates a test harness with a custom endorser factory and TxQueue.
// If txQueue is nil, the default TxQueue will be used.
func NewLocalTestHarnessWithFactoryAndTxQueue(t *testing.T, logger sdk.Logger, evmConfig endorser.EVMConfig, primeDbPath, networkType string, configOverrides map[string]any, factory EndorserFactory, txQueue core.TxQueueInterface) (*TestHarness, error) {
	bypass := networkType == "bypass"

	orderer := &common.Endpoint{Host: "127.0.0.1", Port: 1337}
	peer := &common.Endpoint{Host: "127.0.0.1", Port: 1337}

	// bypass mode uses Fabric block format
	protocol := networkType
	if bypass {
		protocol = "fabric"
	}

	tname := strings.ReplaceAll(strings.ReplaceAll(t.Name(), "/", "_"), ".", "-")
	dir := t.TempDir()
	cfg := config.Config{
		Network: common.Network{
			Protocol:  protocol,
			Channel:   "mychannel",
			Namespace: "basic",
			NsVersion: "1.0",
			ChainID:   4011,
		},
		Gateway: config.Gateway{
			Database: config.DB{
				ConnString: filepath.Join(dir, tname+"gateway.db"),
				TriePath:   filepath.Join(dir, tname+"triedb.db"),
			},
			SyncTimeout: 2 * time.Second,
			Orderers: []common.ClientConfig{
				{Endpoint: orderer},
			},
			Committer: common.ClientConfig{
				Endpoint: peer,
			},
		},
		Endorsers: []econf.Endorser{
			{
				Committer: common.ClientConfig{Endpoint: peer},
				Name:      "endorser1",
				Database: econf.DB{
					ConnString: filepath.Join(dir, tname+"endorser1.db"),
				},
			},
		},
	}
	if err := applyConfigOverrides(&cfg, configOverrides); err != nil {
		return nil, err
	}

	// Derive ChainConfig from cfg.Network.ChainID when not explicitly provided.
	if evmConfig.ChainConfig == nil {
		evmConfig.ChainConfig = common.BuildChainConfig(cfg.Network.ChainID)
	}

	// Build all endorsers using the factory
	dbs, builders, ends := buildEndorsers(t, cfg, evmConfig, factory)

	if !bypass {
		nw, err := fabrictest.Start(t.Context(), cfg.Network.Namespace, networkType, fabrictest.Config{}, dbs[0])
		if err != nil {
			t.Fatalf("fabrictest.Start: %v", err)
		}
		// Don't register cleanup for nw.Stop - fabrictest.Start already registers its own cleanup internally
		orderer.Port = nw.OrdererPort
		peer.Port = nw.PeerPort
	}

	th, _, err := buildTestHarness(t, logger, cfg, evmConfig, primeDbPath, bypass, ends, dbs, builders, txQueue, false)
	if err != nil {
		return nil, err
	}

	return th, nil
}

// NewFabricTestHarness returns a client for integration testing with access to a peer, orderer and local committer.
// It follows the directory structure of a Fablo test network.
func NewFabricTestHarness(t *testing.T, logger sdk.Logger, evmConfig endorser.EVMConfig, primeDbPath string, configOverrides map[string]any) (*TestHarness, error) {
	return NewFabricTestHarnessWithFactory(t, logger, evmConfig, primeDbPath, configOverrides, defaultEndorserFactory)
}

func NewFabricTestHarnessWithFactory(t *testing.T, logger sdk.Logger, evmConfig endorser.EVMConfig, primeDbPath string, configOverrides map[string]any, factory EndorserFactory) (*TestHarness, error) {
	return NewFabricTestHarnessWithFactoryAndTxQueue(t, logger, evmConfig, primeDbPath, configOverrides, factory, nil)
}

func NewFabricTestHarnessWithFactoryAndTxQueue(t *testing.T, logger sdk.Logger, evmConfig endorser.EVMConfig, primeDbPath string, configOverrides map[string]any, factory EndorserFactory, txQueue core.TxQueueInterface) (*TestHarness, error) {
	// cwd, _ := os.Getwd()
	// defer os.Chdir(cwd)
	// _ = os.Chdir("../")

	cfg, err := config.Load("fablo.yaml")
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	x := mathrand.Int64()
	for i := range cfg.Endorsers {
		cfg.Endorsers[i].Database.ConnString = fmt.Sprintf("file:endorser%d-%d.db?mode=memory&cache=shared", i, x)
	}

	if err := applyConfigOverrides(&cfg, configOverrides); err != nil {
		return nil, err
	}

	// Derive ChainConfig from cfg.Network.ChainID when not explicitly provided.
	if evmConfig.ChainConfig == nil {
		evmConfig.ChainConfig = common.BuildChainConfig(cfg.Network.ChainID)
	}

	// Build all endorsers
	dbs, builders, ends := buildEndorsers(t, cfg, evmConfig, factory)

	th, sync, err := buildTestHarness(t, logger, cfg, evmConfig, primeDbPath, false, ends, dbs, builders, txQueue, false)
	if err != nil {
		return nil, err
	}

	waitUntilSynced(t, sync, 10*time.Second)

	return th, nil
}

func NewFabricXTestHarness(t *testing.T, logger sdk.Logger, evmConfig endorser.EVMConfig, primeDbPath string, configOverrides map[string]any) (*TestHarness, error) {
	return NewFabricXTestHarnessWithFactory(t, logger, evmConfig, primeDbPath, configOverrides, defaultEndorserFactory)
}

// NewFabricXTestHarness returns a client for integration testing with access to a peer, orderer and local committer.
// It follows the directory structure of a fabric samples test network.
// Exported for use by eth-tests package.
func NewFabricXTestHarnessWithFactory(t *testing.T, logger sdk.Logger, evmConfig endorser.EVMConfig, primeDbPath string, configOverrides map[string]any, factory EndorserFactory) (*TestHarness, error) {
	return NewFabricXTestHarnessWithFactoryAndTxQueue(t, logger, evmConfig, primeDbPath, configOverrides, factory, nil)
}

func NewFabricXTestHarnessWithFactoryAndTxQueue(t *testing.T, logger sdk.Logger, evmConfig endorser.EVMConfig, primeDbPath string, configOverrides map[string]any, factory EndorserFactory, txQueue core.TxQueueInterface) (*TestHarness, error) {
	cfg, err := config.Load("fabx.yaml")
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if err := applyConfigOverrides(&cfg, configOverrides); err != nil {
		return nil, err
	}

	// Derive ChainConfig from cfg.Network.ChainID when not explicitly provided.
	if evmConfig.ChainConfig == nil {
		evmConfig.ChainConfig = common.BuildChainConfig(cfg.Network.ChainID)
	}

	// Build all endorsers
	dbs, builders, ends := buildEndorsers(t, cfg, evmConfig, factory)

	th, _, err := buildTestHarness(t, logger, cfg, evmConfig, primeDbPath, false, ends, dbs, builders, txQueue, false)
	if err != nil {
		return nil, err
	}

	return th, nil
}

// NewFabricXTestHarnessWithNotifications creates a fabric-x test harness with notification-based
// transaction completion tracking instead of block-based synchronization.
// Uses MemoryStore and NotificationDispatcher for better performance in replay scenarios.
// If extraHandler is non-nil, it will be inserted into the handler chain right before the cleanup handler.
func NewFabricXTestHarnessWithNotifications(t *testing.T, logger sdk.Logger, evmConfig endorser.EVMConfig, primeDbPath string, configOverrides map[string]any, factory EndorserFactory, txQueue core.TxQueueInterface, extraHandler core.TxHandler, confFile string) (*TestHarness, error) {
	if primeDbPath != "" && !filepath.IsAbs(primeDbPath) {
		if abs, err := filepath.Abs(primeDbPath); err == nil {
			primeDbPath = abs
		}
	}
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	_ = os.Chdir("../")

	cfg, err := config.Load(confFile)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if err := applyConfigOverrides(&cfg, configOverrides); err != nil {
		return nil, err
	}

	// Derive ChainConfig from cfg.Network.ChainID when not explicitly provided.
	if evmConfig.ChainConfig == nil {
		evmConfig.ChainConfig = common.BuildChainConfig(cfg.Network.ChainID)
	}

	// Build all endorsers
	dbs, builders, ends := buildEndorsers(t, cfg, evmConfig, factory)

	// Use buildTestHarness with useNotifications=true and extraHandler
	th, _, err := buildTestHarnessWithExtraHandler(t, logger, cfg, evmConfig, primeDbPath, false, ends, dbs, builders, txQueue, true, extraHandler)
	if err != nil {
		return nil, err
	}

	return th, nil
}

// NewEndorser creates an endorser with its dependencies.
// Exported for use by custom endorser factories.
func NewEndorser(t *testing.T, cfg econf.Endorser, channel, namespace string, evmConfig endorser.EVMConfig, protocol string) (endorser.KVS, endorsement.Builder, *endorser.Endorser) {
	t.Helper()

	var signer sdk.Signer
	if cfg.Identity.MSPDir == "" {
		signer = &localSigner{}
	} else {
		var err error
		signer, err = identity.SignerFromMSP(cfg.Identity.MSPDir, cfg.Identity.MspID)
		if err != nil {
			t.Fatalf("SignerFromMSP: %v", err)
		}
	}

	var db endorser.KVS
	switch cfg.Database.Database {
	case "sqlite":
		writeDB, err := state.NewWriteDB(channel, cfg.Database.ConnString)
		if err != nil {
			t.Fatalf("NewWriteDB: %v", err)
		}
		db = endorser.NewVersionedDBWrapper(writeDB)
	default:
		db = endorser.NewLightKVS(1)
	}
	t.Cleanup(func() { db.Close() })

	// the shape of endorsements and blocks differs per protocol.
	var builder endorsement.Builder
	var monotonicVersions bool
	switch protocol {
	case "fabric", "":
		builder = efab.NewEndorsementBuilder(signer)
	case "fabric-x":
		builder = efabx.NewEndorsementBuilder(signer)
		monotonicVersions = true
	default:
		t.Fatalf("unsupported protocol: %q", protocol)
	}

	end, err := endorser.New(
		endorser.NewEVMEngine(namespace, db, evmConfig, monotonicVersions),
		builder,
		evmConfig.ChainConfig.ChainID.Int64(),
	)
	if err != nil {
		t.Fatalf("endorser.New: %v", err)
	}

	return db, builder, end
}

// TestHarness provides access to gateways and endorsers for testing.
// Exported for use by eth-tests package.
type TestHarness struct {
	DBs            []endorser.KVS
	Gateways       []*core.Gateway
	endorsers      []core.Endorser
	ethChainConfig *params.ChainConfig
	Primer         *StatePrimer
	OrdererConfigs []network.OrdererConf
}

// BuildMSPMemberPolicy creates a SignaturePolicyEnvelope for "MSPID.member" policy.
// This policy requires a signature from any member of the specified MSP.
func BuildMSPMemberPolicy(mspID string) ([]byte, error) {
	// Create MSP principal for "member" role
	mspRole := &msp.MSPRole{
		Role:          msp.MSPRole_MEMBER,
		MspIdentifier: mspID,
	}

	mspRoleBytes, err := proto.Marshal(mspRole)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal MSP role: %w", err)
	}

	principal := &msp.MSPPrincipal{
		PrincipalClassification: msp.MSPPrincipal_ROLE,
		Principal:               mspRoleBytes,
	}

	// Create signature policy: SignedBy(0) - requires signature from principal at index 0
	policy := &fabriccommon.SignaturePolicy{}
	policy.Type = &fabriccommon.SignaturePolicy_SignedBy{
		SignedBy: 0,
	}

	// Wrap in envelope
	policyEnvelope := &fabriccommon.SignaturePolicyEnvelope{
		Version:    0,
		Rule:       policy,
		Identities: []*msp.MSPPrincipal{principal},
	}

	// Marshal the SignaturePolicyEnvelope
	mspRuleBytes, err := proto.Marshal(policyEnvelope)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal signature policy envelope: %w", err)
	}

	// Wrap in NamespacePolicy with msp_rule
	namespacePolicy := &applicationpb.NamespacePolicy{
		Rule: &applicationpb.NamespacePolicy_MspRule{
			MspRule: mspRuleBytes,
		},
	}

	return proto.Marshal(namespacePolicy)
}

// CreateNamespaces creates multiple namespaces in a single transaction by writing to the meta-namespace.
// It bypasses the proposal/endorsement layer and creates the transaction envelope directly.
// All namespaces will use the same endorsementPolicy.
func CreateNamespaces(ctx context.Context, t *testing.T, ordererConfigs []network.OrdererConf, adminMSPDir, adminMSPID, channel string, namespaceIDs []string, endorsementPolicy []byte) error {
	t.Helper()

	if len(namespaceIDs) == 0 {
		return nil
	}

	// Resolve adminMSPDir to absolute path before changing directories
	absAdminMSPDir, err := filepath.Abs(adminMSPDir)
	if err != nil {
		return fmt.Errorf("failed to resolve admin MSP directory: %w", err)
	}

	// Change to integration directory for correct relative paths in config
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	defer os.Chdir(cwd)

	// If we're in integration/perf, go up one level to integration
	if filepath.Base(cwd) == "perf" {
		if err := os.Chdir("../"); err != nil {
			return fmt.Errorf("failed to change to integration directory: %w", err)
		}
	}

	// Create orderer connections
	orderers := make([]*network.Orderer, len(ordererConfigs))
	for i, cfg := range ordererConfigs {
		orderer, err := network.NewOrderer(ctx, cfg)
		if err != nil {
			return fmt.Errorf("failed to create orderer %d: %w", i, err)
		}
		defer orderer.Close()
		orderers[i] = orderer
	}

	// Load admin signer from MSP directory (use absolute path)
	adminSigner, err := identity.SignerFromMSP(absAdminMSPDir, adminMSPID)
	if err != nil {
		return fmt.Errorf("failed to load admin signer: %w", err)
	}

	creator, err := adminSigner.Serialize()
	if err != nil {
		return fmt.Errorf("failed to serialize admin identity: %w", err)
	}

	// Generate nonce and compute TxID
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("failed to generate nonce: %w", err)
	}
	txID := protoutil.ComputeTxID(nonce, creator)

	// Build write set for all namespaces in the meta-namespace
	var readWrites []*applicationpb.ReadWrite
	for _, nsID := range namespaceIDs {
		readWrites = append(readWrites, &applicationpb.ReadWrite{
			Key:   []byte(nsID),
			Value: endorsementPolicy,
		})
	}

	// Create Fabric-X transaction with writes to meta-namespace (without endorsements first)
	txNamespace := &applicationpb.TxNamespace{
		NsId:        "_meta",
		NsVersion:   0,
		ReadsOnly:   nil,
		ReadWrites:  readWrites,
		BlindWrites: nil,
	}

	// Use ASN1Marshal to create the digest for signing (same as endorser does)
	// No metadata needed for meta-namespace writes
	digest, err := txNamespace.ASN1Marshal(txID, nil)
	if err != nil {
		return fmt.Errorf("failed to ASN1 marshal tx namespace: %w", err)
	}

	// Sign the digest with admin signer
	endorsementSig, err := adminSigner.Sign(digest)
	if err != nil {
		return fmt.Errorf("failed to sign endorsement: %w", err)
	}

	// Deserialize the creator to extract certificate bytes (same as packager.go does)
	var si msp.SerializedIdentity
	if err := proto.Unmarshal(creator, &si); err != nil {
		return fmt.Errorf("failed to unmarshal creator identity: %w", err)
	}

	// Create admin identity for endorsement using certificate bytes
	adminIdentity := msppb.NewIdentity(si.Mspid, si.IdBytes)

	// Create endorsement with identity (Fabric-X format)
	endorsements := &applicationpb.Endorsements{
		EndorsementsWithIdentity: []*applicationpb.EndorsementWithIdentity{{
			Endorsement: endorsementSig,
			Identity:    adminIdentity,
		}},
	}

	// Create complete transaction with endorsement
	tx := &applicationpb.Tx{
		Metadata:     nil,
		Namespaces:   []*applicationpb.TxNamespace{txNamespace},
		Endorsements: []*applicationpb.Endorsements{endorsements},
	}

	txBytes, err := proto.Marshal(tx)
	if err != nil {
		return fmt.Errorf("failed to marshal tx: %w", err)
	}

	// Create channel header with MESSAGE type (not ENDORSER_TRANSACTION)
	channelHeader := &fabriccommon.ChannelHeader{
		Type:      int32(fabriccommon.HeaderType_MESSAGE),
		TxId:      txID,
		Timestamp: timestamppb.Now(),
		ChannelId: channel,
		Epoch:     0,
	}
	channelHeaderBytes, err := proto.Marshal(channelHeader)
	if err != nil {
		return fmt.Errorf("failed to marshal channel header: %w", err)
	}

	// Create signature header
	signatureHeaderBytes, err := proto.Marshal(&fabriccommon.SignatureHeader{
		Creator: creator,
		Nonce:   nonce,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal signature header: %w", err)
	}

	// Create payload
	payload := &fabriccommon.Payload{
		Header: &fabriccommon.Header{
			ChannelHeader:   channelHeaderBytes,
			SignatureHeader: signatureHeaderBytes,
		},
		Data: txBytes,
	}
	payloadBytes, err := proto.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Sign the payload
	signature, err := adminSigner.Sign(payloadBytes)
	if err != nil {
		return fmt.Errorf("failed to sign payload: %w", err)
	}

	// Create envelope
	envelope := &fabriccommon.Envelope{
		Payload:   payloadBytes,
		Signature: signature,
	}

	// Broadcast to all orderers
	var errs int
	for _, orderer := range orderers {
		if err := orderer.Broadcast(ctx, envelope); err != nil {
			t.Logf("Failed to broadcast to orderer: %v", err)
			errs++
		}
	}

	if errs*2 > len(orderers) {
		return fmt.Errorf("failed to broadcast to majority of orderers")
	}

	t.Logf("Created %d namespaces successfully: %v", len(namespaceIDs), namespaceIDs)
	return nil
}

func (th *TestHarness) Stop() error {
	errs := []error{}
	for _, n := range th.Gateways {
		if err := n.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func processCommon(t *testing.T, gw *core.Gateway, commit bool, tx *types.Transaction) sdk.Endorsement {
	t.Helper()

	env, err := gw.ExecuteEthTx(t.Context(), tx)
	if err != nil {
		t.Fatal(err)
	}

	if commit {
		if err := gw.SubmitFabricTx(t.Context(), env); err != nil {
			t.Fatal(err)
		}

		ec, err := NewNativeEthClient(gw)
		if err != nil {
			t.Fatal(err)
		}

		waitForCommitT(t, ec, tx)
	}

	return env
}

func getEndorsedTxForSmartContractCall(t *testing.T, client *EthClient, addr ethcommon.Address, gw *core.Gateway, method string, args ...any) sdk.Endorsement {
	t.Helper()
	tx, err := client.TxForCall(t.Context(), gw, &addr, method, args...)
	if err != nil {
		t.Fatal(err)
	}

	return processCommon(t, gw, false, tx)
}

func NewNativeEthClient(gw *core.Gateway) (*ethclient.Client, error) {
	// Create production RPC server (no test accounts needed for integration tests)
	rpcServer, err := gwapi.NewServer(gw)
	if err != nil {
		return nil, err
	}

	client := rpc.DialInProc(rpcServer)
	return ethclient.NewClient(client), nil
}

func deploySmartContract(t *testing.T, gw *core.Gateway, client *EthClient, args ...any) ethcommon.Address {
	t.Helper()

	ec, err := NewNativeEthClient(gw)
	if err != nil {
		t.Fatal(err)
	}

	tx, addr, err := client.txForDeploy(t.Context(), gw, args...)
	if err != nil {
		t.Fatal(err)
	}

	err = ec.SendTransaction(t.Context(), tx)
	if err != nil {
		t.Fatal(err)
	}

	waitForCommitT(t, ec, tx)

	return addr
}

func callSmartContract(t *testing.T, client *EthClient, addr ethcommon.Address, gw *core.Gateway, method string, args ...any) {
	t.Helper()

	ec, err := NewNativeEthClient(gw)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := client.TxForCall(t.Context(), gw, &addr, method, args...)
	if err != nil {
		t.Fatal(err)
	}

	err = ec.SendTransaction(t.Context(), tx)
	if err != nil {
		t.Fatal(err)
	}

	waitForCommitT(t, ec, tx)
}

func querySmartContract(t *testing.T, gw *core.Gateway, client *EthClient, addr ethcommon.Address, method string, params ...any) []any {
	t.Helper()

	ec, err := NewNativeEthClient(gw)
	if err != nil {
		t.Fatal(err)
	}

	args, err := client.argsForCall(&addr, method, params...)
	if err != nil {
		t.Fatal(err)
	}

	output, err := ec.CallContract(t.Context(), *args, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(output) == 0 {
		return []any{}
	}

	res, err := client.getResult(method, output)
	if err != nil {
		t.Fatal(err)
	}

	return res
}

// querySmartContractExpect queries all gateways in the test harness and expects the same result
func querySmartContractExpect(t *testing.T, client *EthClient, addr ethcommon.Address, th *TestHarness, expected any, method string, params ...any) {
	for _, gw := range th.Gateways {
		res := querySmartContract(t, gw, client, addr, method, params...)
		if len(res) == 0 {
			t.Errorf("expected %v, got empty result", expected)
			return
		}

		rBig, rOK := res[0].(*big.Int)
		eBig, eOK := expected.(*big.Int)
		if rOK && eOK {
			if rBig.Cmp(eBig) != 0 {
				t.Errorf("expected %v, got %v", eBig, rBig)
			}
			return
		}

		if !reflect.DeepEqual(res[0], expected) {
			t.Errorf("expected %+v, got %+v", expected, res[0])
		}
	}
}

func submit(t *testing.T, gw *core.Gateway, end sdk.Endorsement) {
	t.Helper()

	if err := gw.SubmitFabricTx(t.Context(), end); err != nil {
		t.Error(err)
	}

	ec, err := NewNativeEthClient(gw)
	if err != nil {
		t.Error(err)
	}

	// Extract the Ethereum transaction from the proposal
	tx, err := extractEthTxFromProposal(end.Proposal)
	if err != nil {
		t.Error(err)
	}

	waitForCommitT(t, ec, tx)
}

// extractEthTxFromProposal extracts the Ethereum transaction from a peer.Proposal
func extractEthTxFromProposal(proposal *peer.Proposal) (*types.Transaction, error) {
	// Unmarshal the proposal payload to get the ChaincodeProposalPayload
	payload, err := protoutil.UnmarshalChaincodeProposalPayload(proposal.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal proposal payload: %w", err)
	}

	// Unmarshal the ChaincodeInvocationSpec from the input
	cis, err := protoutil.UnmarshalChaincodeInvocationSpec(payload.Input)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal chaincode invocation spec: %w", err)
	}

	// Get the args - args[0] is the proposal type, args[1] is the serialized eth tx
	args := cis.ChaincodeSpec.Input.Args
	if len(args) < 2 {
		return nil, fmt.Errorf("expected at least 2 args, got %d", len(args))
	}

	// Check that this is an EVM transaction proposal
	if len(args[0]) != 1 || args[0][0] != byte(common.ProposalTypeEVMTx) {
		return nil, fmt.Errorf("not an EVM transaction proposal")
	}

	// Unmarshal the Ethereum transaction
	var tx types.Transaction
	if err := tx.UnmarshalBinary(args[1]); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ethereum transaction: %w", err)
	}

	return &tx, nil
}

func waitForCommitT(t *testing.T, ec *ethclient.Client, tx *types.Transaction) {
	err := waitForCommit(t.Context(), ec, tx)
	if err != nil {
		t.Fatal(err)
	}
}

func waitForCommit(ctx context.Context, ec *ethclient.Client, tx *types.Transaction) error {
	var err error

	backoff := time.Duration(0)
	iter := 0
	step := 100

	for pending := true; pending; {
		_, pending, err = ec.TransactionByHash(ctx, tx.Hash())
		if err != nil {
			if !strings.Contains(err.Error(), "not found") {
				return err
			}
			pending = true
		}

		if pending {
			if backoff == 0 {
				runtime.Gosched()
			} else {
				time.Sleep(backoff)
			}

			iter++
			if iter%step == 0 {
				if backoff == 0 {
					backoff = time.Millisecond
				} else {
					backoff *= 2
				}
			}
		}
	}

	return nil
}

// decodeRawTransactionT decodes a raw Ethereum transaction and
// reports errors via t.Errorf instead of returning them.
func decodeRawTransactionT(t *testing.T, raw []byte) *types.Transaction {
	t.Helper()

	if len(raw) == 0 {
		t.Errorf("DecodeRawTransaction: empty raw transaction")
		return nil
	}

	var tx types.Transaction
	if err := rlp.DecodeBytes(raw, &tx); err != nil {
		t.Errorf("DecodeRawTransaction: failed to decode raw transaction: %v", err)
		return nil
	}

	return &tx
}

// TestLogger is a logger that logs to a testing.T.
// Exported for use by eth-tests package.
type TestLogger struct {
	ID string
	T  *testing.T
}

func (tl TestLogger) Debugf(format string, v ...any) {
	tl.T.Helper()
	tl.T.Logf(tl.ID+" > [DEBUG] "+format, v...)
}

func (tl TestLogger) Infof(format string, v ...any) {
	tl.T.Helper()
	tl.T.Logf(tl.ID+" > [INFO] "+format, v...)
}

func (tl TestLogger) Warnf(format string, v ...any) {
	tl.T.Helper()
	tl.T.Logf(tl.ID+" > [WARN] "+format, v...)
}

func (tl TestLogger) Errorf(format string, v ...any) {
	tl.T.Helper()
	tl.T.Logf(tl.ID+" > [ERROR] "+format, v...)
}

func waitUntilSynced(t *testing.T, sync *network.Synchronizer, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()

	for {
		if err := sync.Ready(); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("timeout waiting for sync")
		case <-time.After(100 * time.Millisecond):
		}
	}
}
