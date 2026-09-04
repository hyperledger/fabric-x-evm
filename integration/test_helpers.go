/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"context"
	"errors"
	"fmt"
	"math/big"
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

	"github.com/hyperledger/fabric-protos-go-apiv2/msp"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/protoutil"
	"github.com/hyperledger/fabric-x-evm/common"
	eapi "github.com/hyperledger/fabric-x-evm/endorser/api"
	eapp "github.com/hyperledger/fabric-x-evm/endorser/app"
	econf "github.com/hyperledger/fabric-x-evm/endorser/config"
	ecore "github.com/hyperledger/fabric-x-evm/endorser/core"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-evm/endorser/storage"
	gwapi "github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/app"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/gateway/testimpl"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	bfab "github.com/hyperledger/fabric-x-sdk/blocks/fabric"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	"github.com/hyperledger/fabric-x-sdk/fabrictest"
	"github.com/hyperledger/fabric-x-sdk/identity"
	"github.com/hyperledger/fabric-x-sdk/local"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfab "github.com/hyperledger/fabric-x-sdk/network/fabric"
	"google.golang.org/protobuf/proto"
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
	return proto.Marshal(&msp.SerializedIdentity{Mspid: "test-msp", IdBytes: []byte("serialised identity")})
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

// HandlerChainFactory builds the gateway and the complete synchronizer handler
// chain for a test harness.  It receives the pre-built endorser services, gateway
// signer, submitters, and txQueue so it can call app.BuildGateway internally and
// assemble whatever handler list is appropriate.
//
// It returns:
//   - gw: the constructed gateway (also started by buildTestHarness after this call)
//   - handlers: the full, ordered []blocks.BlockHandler for the synchronizer
//   - heightReader: the network.BlockHeightReader the synchronizer uses for catch-up
//
// The factory is responsible for allocating any store it needs and registering
// teardown via t.Cleanup.  Passing nil to buildTestHarness selects
// defaultHandlerChain.
//
// Conventional handler order:
//
//	[endorser KVS…, chain store, gateway, …any extra observers]
type HandlerChainFactory func(
	t *testing.T,
	ctx context.Context,
	cfg config.Config,
	ends []eapi.Service,
	gwSigner sdk.Signer,
	submitters []core.Submitter,
	txQueue core.TxQueueInterface,
	dbs []storage.KVS,
) (gw *core.Gateway, handlers []blocks.BlockHandler, heightReader network.BlockHeightReader)

// defaultHandlerChain is the HandlerChainFactory used by every test that does
// not need a custom chain store.  It opens the SQLite-backed core.Chain,
// registers chain.Close on t, builds the gateway, and returns the standard
// handler order.
//
// Handler order matters: each handler is called in sequence for every committed block.
//
//  1. Endorser KVS (dbs): update the read/write-set store used by the endorser for
//     MVCC validation. Must run first so that by the time chain and gateway see the
//     block the endorser's state already reflects it — this gives read-your-writes
//     semantics for the test RPC's synchronous eth_sendRawTransaction.
//
//  2. Chain: persist the block and its Ethereum transactions to the SQLite store and
//     update the state-root trie. Must run before the gateway so that eth_getBlockBy*
//     and eth_getTransactionReceipt are answerable the moment the gateway marks a
//     transaction complete.
//
//  3. Gateway: call TxQueue.Handle to mark any pending Ethereum transactions whose
//     Fabric tx-ID appears in this block as complete, unblocking waiting callers.
//
//  4. Extra handlers (e.g. TxCompletionTracker in perf tests): any caller-supplied
//     handlers that observe committed blocks for their own purposes.
func defaultHandlerChain(t *testing.T, ctx context.Context, cfg config.Config, ends []eapi.Service, gwSigner sdk.Signer, submitters []core.Submitter, txQueue core.TxQueueInterface, dbs []storage.KVS) (*core.Gateway, []blocks.BlockHandler, network.BlockHeightReader) {
	chain, err := core.NewChain(cfg.Gateway.Database.ConnString, cfg.Gateway.Database.TriePath, false)
	if err != nil {
		t.Fatalf("open chain: %v", err)
	}
	t.Cleanup(func() { chain.Close() })

	txPerSec := 0
	if cfg.Network.Namespace == "synthetic" {
		txPerSec = 10000
	}
	// Tests prime and revert ledger state out of band; reconcile before each admit.
	gw, err := app.BuildGateway(ctx, ends, gwSigner, cfg.Network, chain, submitters, cfg.Gateway.SubmitterCount, cfg.Gateway.WorkerCount, txQueue, cfg.Gateway.EndorsementChanSize, txPerSec, core.WithNonceSequencer(testimpl.NewReconcilingGate))
	if err != nil {
		t.Fatalf("build gateway: %v", err)
	}

	handlers := make([]blocks.BlockHandler, 0, len(dbs)+2)
	for _, db := range dbs {
		handlers = append(handlers, db)
	}
	handlers = append(handlers, chain, gw)
	return gw, handlers, chain
}

// buildTestHarness is the shared implementation for all test harness constructors.
// It builds endorsers, a gateway, and primes state.
//
// The gateway signer and identity deserializer are derived from cfg:
//   - cfg.Gateway.SignerMSPDir set → MSP-based signer; empty → local mock
//   - cfg.Endorsers[0].MspDir set → FabricDeserializer; empty → local mock
//
// Sync goroutines are started in the background using ctx. The returned synchronizer
// can be used by callers that need to wait for the initial sync to complete.
//
// chainFactory controls which chain store and handler chain are wired into the
// synchronizer. Pass nil to use defaultHandlerChain.
func buildTestHarness(t *testing.T, logger sdk.Logger, cfg config.Config, evmConfig execution.EVMConfig, primeDBPath string, bypass bool, endorsers []EndorserComponents, txQueue core.TxQueueInterface, chainFactory HandlerChainFactory) (*TestHarness, app.Synchronizer, error) {
	dbs := make([]storage.KVS, len(endorsers))
	builders := make([]endorsement.Builder, len(endorsers))
	ends := make([]eapi.Service, len(endorsers))
	for i, e := range endorsers {
		dbs[i], builders[i], ends[i] = e.KVS, e.Builder, e.Service
	}

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

	// Build submitters (one per worker for parallel submission).
	ordererConfs := make([]network.OrdererConf, len(cfg.Gateway.Orderers))
	for i, o := range cfg.Gateway.Orderers {
		ordererConfs[i] = o.ToOrdererConf()
	}

	submitterCount := cfg.Gateway.SubmitterCount
	if submitterCount <= 0 {
		submitterCount = core.DefaultNumWorkers
	}

	var (
		submitters []core.Submitter
		sync       app.Synchronizer
		err        error
	)

	if bypass {
		// Use local submitters for bypass mode (no network communication)
		submitters = make([]core.Submitter, submitterCount)
		for i := 0; i < submitterCount; i++ {
			submitters[i] = local.NewLocalSubmitter(dbs[0], cfg.Network.Channel, cfg.Network.Namespace, nfab.NewTxPackager(gwSigner), bfab.NewBlockParser(logger), false)
		}
	} else {
		// Create network submitters
		submitters, err = app.NewNetworkSubmitters(t.Context(), cfg.Network.Protocol, ordererConfs, gwSigner, submitterCount, logger)
		if err != nil {
			return nil, nil, err
		}
	}

	if chainFactory == nil {
		chainFactory = defaultHandlerChain
	}
	gw, handlers, heightReader := chainFactory(t, t.Context(), cfg, ends, gwSigner, submitters, txQueue, dbs)

	// Create synchronizer with handlers — only for non-bypass mode (bypass uses a local
	// in-process submitter and has no network peer to synchronize with).
	//
	// Handler order matters: each handler is called in sequence for every committed block.
	// The ordering is the responsibility of the chainFactory; see defaultHandlerChain for
	// the conventional ordering (endorser KVS…, chain store, gateway, extra observers…).
	if !bypass {
		sync, err = app.NewSynchronizer(cfg.Network.Protocol, heightReader, cfg.Network.Channel, cfg.Network.Namespace, cfg.Committer.ToPeerConf(), gwSigner, logger, handlers...)
		if err != nil {
			return nil, nil, err
		}

		go func() { _ = sync.Start(t.Context()) }()
	}

	// Start gateway worker pool
	gw.Start(t.Context())
	t.Cleanup(func() { gw.Stop() })

	// Create state primer (use first submitter)
	primer, err := NewStatePrimer(gw, submitters[0], dbs[0], cfg.Network.Namespace, gwSigner, builders, cfg.Network.Channel, cfg.Network.NsVersion, cfg.Network.Protocol == "fabric-x")
	if err != nil {
		return nil, nil, err
	}

	th := &TestHarness{
		Gateways:       []*core.Gateway{gw},
		endorsers:      ends,
		ethChainConfig: evmConfig.ChainConfig,
		Primer:         primer,
		DBs:            dbs,
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

// EndorserComponents bundles the pieces produced when constructing a single test-harness
// endorser: its KVS (for handler registration and priming), its endorsement builder (for
// state priming), and the eapi.Service used for endorsement.
type EndorserComponents struct {
	KVS     storage.KVS
	Builder endorsement.Builder
	Service eapi.Service
}

// EndorserFactory is a function that creates an endorser along with its dependencies.
// Service is the eapi.Service interface which both *ecore.Endorser and *testimpl.EndorserWrapper implement.
type EndorserFactory func(t *testing.T, ecfg econf.Endorser, channel, namespace string, evmConfig execution.EVMConfig, protocol string) EndorserComponents

// buildEndorsers creates the embedded endorser using the provided factory
// function. Split-deployment configs have no embedded endorser and yield none.
func buildEndorsers(t *testing.T, cfg config.Config, evmConfig execution.EVMConfig, factory EndorserFactory) []EndorserComponents {
	if cfg.Endorser == nil {
		return nil
	}
	ecfg := *cfg.Endorser
	// LightKVS needs at least one history slot to record the pre-write snapshot on
	// every Update; config files (fablo.yaml, fabx.yaml) don't set history_size.
	if ecfg.Database.HistorySize == 0 {
		ecfg.Database.HistorySize = 1
	}
	return []EndorserComponents{
		factory(t, ecfg, cfg.Network.Channel, cfg.Network.Namespace, evmConfig, cfg.Network.Protocol),
	}
}

// defaultEndorserFactory creates regular endorsers without wrapping.
func defaultEndorserFactory(t *testing.T, ecfg econf.Endorser, channel, namespace string, evmConfig execution.EVMConfig, protocol string) EndorserComponents {
	db, builder, end := NewEndorser(t, ecfg, channel, namespace, evmConfig, protocol)
	return EndorserComponents{KVS: db, Builder: builder, Service: end}
}

// prepareHarnessConfig applies configOverrides to cfg, derives evmConfig.ChainConfig from
// cfg.Network.ChainID when not already set, and builds all endorsers via factory. Shared
// tail of every harness constructor below.
func prepareHarnessConfig(t *testing.T, cfg *config.Config, evmConfig *execution.EVMConfig, configOverrides map[string]any, factory EndorserFactory) ([]EndorserComponents, error) {
	if err := applyConfigOverrides(cfg, configOverrides); err != nil {
		return nil, err
	}

	if evmConfig.ChainConfig == nil {
		evmConfig.ChainConfig = common.BuildChainConfig(cfg.Network.ChainID)
	}

	return buildEndorsers(t, *cfg, *evmConfig, factory), nil
}

// NewLocalTestHarness commits updates directly to the DB, bypassing peers and orderers.
func NewLocalTestHarness(t *testing.T, logger sdk.Logger, evmConfig execution.EVMConfig, primeDbPath, networkType string, configOverrides map[string]any) (*TestHarness, error) {
	return NewLocalTestHarnessWithFactory(t, logger, evmConfig, primeDbPath, networkType, configOverrides, defaultEndorserFactory)
}

// NewLocalTestHarnessWithFactory is like NewLocalTestHarness but allows a custom endorser factory.
func NewLocalTestHarnessWithFactory(t *testing.T, logger sdk.Logger, evmConfig execution.EVMConfig, primeDbPath, networkType string, configOverrides map[string]any, factory EndorserFactory) (*TestHarness, error) {
	bypass := networkType == "bypass"

	orderer := &common.Endpoint{Host: "127.0.0.1", Port: 1337}
	peer := &common.Endpoint{Host: "127.0.0.1", Port: 1337}

	// bypass mode uses Fabric block format
	protocol := networkType
	if bypass {
		protocol = "fabric"
	}

	dir := t.TempDir()
	cfg := config.Config{
		Network: common.Network{
			Protocol:  protocol,
			Channel:   "mychannel",
			Namespace: "basic",
			NsVersion: "1.0",
			ChainID:   4011,
		},
		Committer:    common.ClientConfig{Endpoint: peer},
		Synchronizer: config.Synchronizer{Timeout: 2 * time.Second},
		Gateway: config.Gateway{
			Database: config.DB{
				ConnString: filepath.Join(dir, "gateway.db"),
				TriePath:   filepath.Join(dir, "triedb.db"),
			},
			Orderers: []common.ClientConfig{
				{Endpoint: orderer},
			},
		},
		Endorser: &econf.Endorser{
			Name: "endorser1",
			Database: econf.DB{
				Database:    "memory",
				ConnString:  filepath.Join(dir, "endorser1.db"),
				HistorySize: 1,
			},
		},
	}
	endorsers, err := prepareHarnessConfig(t, &cfg, &evmConfig, configOverrides, factory)
	if err != nil {
		return nil, err
	}

	if !bypass {
		nw, err := fabrictest.Start(t.Context(), cfg.Network.Namespace, networkType, fabrictest.Config{}, endorsers[0].KVS)
		if err != nil {
			t.Fatalf("fabrictest.Start: %v", err)
		}
		// Don't register cleanup for nw.Stop - fabrictest.Start already registers its own cleanup internally
		orderer.Port = nw.OrdererPort
		peer.Port = nw.PeerPort
	}

	th, _, err := buildTestHarness(t, logger, cfg, evmConfig, primeDbPath, bypass, endorsers, nil, nil)
	if err != nil {
		return nil, err
	}

	return th, nil
}

// newFileConfigHarness loads configFile (e.g. "fablo.yaml" for Fablo, "fabx.yaml" for
// fabric-x — both connect to a real, already-running network), builds a harness against it,
// and waits for the gateway synchronizer to catch up before returning.
func newFileConfigHarness(t *testing.T, logger sdk.Logger, evmConfig execution.EVMConfig, primeDbPath, configFile string, configOverrides map[string]any) (*TestHarness, error) {
	cfg, err := config.Load(configFile)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	endorsers, err := prepareHarnessConfig(t, &cfg, &evmConfig, configOverrides, defaultEndorserFactory)
	if err != nil {
		return nil, err
	}

	th, sync, err := buildTestHarness(t, logger, cfg, evmConfig, primeDbPath, false, endorsers, nil, nil)
	if err != nil {
		return nil, err
	}

	if err := app.WaitUntilSynced(t.Context(), sync, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	return th, nil
}

// newSplitFileConfigHarness is newFileConfigHarness for a split-deployment
// gateway config: each endorser is built from its own config file, served over
// mTLS gRPC, and reached by the gateway through a gRPC client rather than
// directly.
//
// The endorsers still run in this process, so the harness keeps their KVS and
// builder: the KVS is registered as a block handler like any embedded endorser
// (which is what keeps their state current), and the builder is what the state
// primer signs with. Only the gateway's own path to them is remote, which is
// the part these tests exist to exercise.
func newSplitFileConfigHarness(t *testing.T, logger sdk.Logger, evmConfig execution.EVMConfig, primeDbPath, gatewayConfigFile string, endorserConfigFiles []string, trustedCAs []string, configOverrides map[string]any) (*TestHarness, error) {
	cfg, err := config.Load(gatewayConfigFile)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	if err := applyConfigOverrides(&cfg, configOverrides); err != nil {
		return nil, err
	}
	if evmConfig.ChainConfig == nil {
		evmConfig.ChainConfig = common.BuildChainConfig(cfg.Network.ChainID)
	}
	if len(cfg.Gateway.Endorsers) != len(endorserConfigFiles) {
		return nil, fmt.Errorf("%s: %d gateway.endorsers entries but %d endorser config files",
			gatewayConfigFile, len(cfg.Gateway.Endorsers), len(endorserConfigFiles))
	}

	endorsers := make([]EndorserComponents, len(endorserConfigFiles))
	for i, ecfgFile := range endorserConfigFiles {
		db, builder, client := startServedEndorser(t, ecfgFile, evmConfig, trustedCAs, &cfg.Gateway.Endorsers[i])
		endorsers[i] = EndorserComponents{KVS: db, Builder: builder, Service: client}
	}

	th, sync, err := buildTestHarness(t, logger, cfg, evmConfig, primeDbPath, false, endorsers, nil, nil)
	if err != nil {
		return nil, err
	}

	if err := app.WaitUntilSynced(t.Context(), sync, 10*time.Second); err != nil {
		t.Fatal(err)
	}

	return th, nil
}

// NewFabricXTestHarnessWithNotifications creates a fabric-x test harness backed by
// the hybrid synchronizer (delivery catch-up + notification live feed).
//
// It blocks until the synchronizer has fully caught up with the network before
// returning, so callers can rely on the node being in sync from the first call.
//
// chainFactory controls the chain store and handler chain wired into the
// synchronizer. Pass nil to use defaultHandlerChain (SQLite-backed core.Chain).
// Perf tests supply their own factory to use a lightweight in-memory height
// tracker and attach a TxCompletionTracker as a tail handler.
func NewFabricXTestHarnessWithNotifications(t *testing.T, logger sdk.Logger, evmConfig execution.EVMConfig, primeDbPath string, configOverrides map[string]any, factory EndorserFactory, txQueue core.TxQueueInterface, chainFactory HandlerChainFactory, confFile string) (*TestHarness, error) {
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

	endorsers, err := prepareHarnessConfig(t, &cfg, &evmConfig, configOverrides, factory)
	if err != nil {
		return nil, err
	}

	th, sync, err := buildTestHarness(t, logger, cfg, evmConfig, primeDbPath, false, endorsers, txQueue, chainFactory)
	if err != nil {
		return nil, err
	}

	// Wait for the hybrid synchronizer to finish delivery catch-up.
	if err := app.WaitUntilSynced(t.Context(), sync, 60*time.Second); err != nil {
		return nil, fmt.Errorf("timed out waiting for sync: %w", err)
	}

	return th, nil
}

// NewEndorser creates a sync-less endorser with its dependencies, for use under the
// harness's single gateway-level synchronizer topology (see buildTestHarnessWithExtraHandler).
// Exported for use by custom endorser factories.
func NewEndorser(t *testing.T, cfg econf.Endorser, channel, namespace string, evmConfig execution.EVMConfig, protocol string) (storage.KVS, endorsement.Builder, *ecore.Endorser) {
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

	end, db, builder, err := eapp.NewEndorserCore(cfg.Database, channel, namespace, protocol, signer, evmConfig, false, econf.Endorser{})
	if err != nil {
		t.Fatalf("NewEndorserCore: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return db, builder, end
}

// TestHarness provides access to gateways and endorsers for testing.
type TestHarness struct {
	DBs            []storage.KVS
	Gateways       []*core.Gateway
	endorsers      []eapi.Service
	ethChainConfig *params.ChainConfig
	Primer         *StatePrimer
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
		if err := gw.SubmitFabricTx(t.Context(), tx.Hash(), env); err != nil {
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

	// Extract the Ethereum transaction from the proposal
	tx, err := extractEthTxFromProposal(end.Proposal)
	if err != nil {
		t.Error(err)
	}

	if err := gw.SubmitFabricTx(t.Context(), tx.Hash(), end); err != nil {
		t.Error(err)
	}

	ec, err := NewNativeEthClient(gw)
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
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var err error

	backoff := time.Duration(0)
	iter := 0
	step := 100

	for pending := true; pending; {
		_, pending, err = ec.TransactionByHash(ctx, tx.Hash())
		if err != nil {
			if !strings.Contains(err.Error(), "not found") {
				return fmt.Errorf("waiting for tx %s to commit: %w", tx.Hash(), err)
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
