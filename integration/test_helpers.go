/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/hyperledger/fabric-x-evm/endorser"
	"github.com/hyperledger/fabric-x-evm/endorser/api"
	econf "github.com/hyperledger/fabric-x-evm/endorser/config"
	gwapi "github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/utils"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	bfab "github.com/hyperledger/fabric-x-sdk/blocks/fabric"
	bfabx "github.com/hyperledger/fabric-x-sdk/blocks/fabricx"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	efab "github.com/hyperledger/fabric-x-sdk/endorsement/fabric"
	efabx "github.com/hyperledger/fabric-x-sdk/endorsement/fabricx"
	"github.com/hyperledger/fabric-x-sdk/fabrictest"
	"github.com/hyperledger/fabric-x-sdk/identity"
	"github.com/hyperledger/fabric-x-sdk/local"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfab "github.com/hyperledger/fabric-x-sdk/network/fabric"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
	"github.com/hyperledger/fabric-x-sdk/state"
)

type localSigner struct{}

func (localSigner) Sign(msg []byte) ([]byte, error) {
	return []byte("signature"), nil
}

func (localSigner) Serialize() ([]byte, error) {
	return []byte("serialised identity"), nil
}

type localIdentity struct{}

func (*localIdentity) Validate() error {
	return nil
}

func (*localIdentity) Verify(msg []byte, sig []byte) error {
	return nil
}

type localIdDeserialiser struct{}

func (*localIdDeserialiser) DeserializeIdentity(serializedIdentity []byte) (api.Identity, error) {
	return &localIdentity{}, nil
}

// NewStatePrimer returns a reset StatePrimer ready for a new batch of state operations.
// Can be called at any time during tests.
//
// Example usage:
//
//	primer, err := th.NewStatePrimer()
//	err = primer.SetNonce(addr1, 5).SetCode(addr2, contractCode).Commit(ctx)
func (th *TestHarness) NewStatePrimer() (*StatePrimer, error) {
	return th.primer.Reset()
}

// PrimeGenesisAlloc primes ledger state from an Ethereum genesis allocation and
// injects the resulting ethStateDB into all endorsers for state reuse.
func (th *TestHarness) PrimeGenesisAlloc(ctx context.Context, pre types.GenesisAlloc) error {
	if len(pre) == 0 {
		return nil
	}

	primer, err := th.NewStatePrimer()
	if err != nil {
		return err
	}

	// Sort addresses to ensure deterministic account creation order
	// (Go map iteration order is random, which affects the state trie structure)
	var addresses []common.Address
	for addr := range pre {
		addresses = append(addresses, addr)
	}
	sort.Slice(addresses, func(i, j int) bool {
		return bytes.Compare(addresses[i].Bytes(), addresses[j].Bytes()) < 0
	})

	// Convert each test account to StatePrimer operations in sorted order
	for _, addr := range addresses {
		account := pre[addr]
		// Set nonce (always set it, even if 0, to ensure consistent state tracking)
		n := account.Nonce
		nonce := &n

		// Set balance
		var balance *big.Int
		if account.Balance != nil {
			balance = account.Balance
		}

		// Set code
		var code []byte
		if len(account.Code) > 0 {
			code = account.Code
		}

		// Set storage
		var storage map[common.Hash]common.Hash
		if len(account.Storage) > 0 {
			storage = account.Storage
		}

		// Apply all account properties
		primer.SetAccount(addr, nonce, code, balance, storage)
	}

	// Extract the ethStateDB before committing
	ethStateDB := primer.GetEthStateDB()

	// Commit the ethStateDB to finalize the primed state
	// This is critical: we commit with deleteEmptyObjects=false to preserve all primed accounts
	root, err := ethStateDB.Commit(0, false, false)
	if err != nil {
		return fmt.Errorf("failed to commit primed ethStateDB: %w", err)
	}

	// Create a new ethStateDB from the committed root
	// This ensures we start transaction execution with a clean, committed state
	stateDB := ethStateDB.Database()
	ethStateDB, err = ethstate.New(root, stateDB)
	if err != nil {
		return fmt.Errorf("failed to create new ethStateDB from committed root: %w", err)
	}

	// Commit all state changes to the Fabric ledger
	if err := primer.Commit(ctx); err != nil {
		return err
	}

	for _, end := range th.endorsers {
		end.SetEthStateDB(ethStateDB)
	}

	return nil
}

// PrimeStateFromJSON builds a proposal that contains a RWSet derived from the contents of
// `jsonFilePath` as the chaincode results, creates a ProposalResponses signed by the given
// endorsers and submits them via the submitter. This causes Fabric peers to apply the state
// through normal commit flow.
//
// This is a convenience wrapper around NewStatePrimer().LoadFromJSON().Commit().
func (th *TestHarness) PrimeStateFromJSON(ctx context.Context, jsonFilePath string) error {
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
	return primer.Commit(ctx)
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
func buildTestHarness(t *testing.T, logger sdk.Logger, cfg config.Config, evmConfig *endorser.EVMConfig, primeDBPath, networkType string) (*TestHarness, []*network.Synchronizer, error) {
	t.Helper()

	var ethChainConfig *params.ChainConfig
	if evmConfig != nil {
		ethChainConfig = evmConfig.ChainConfig
	}

	// Build all endorsers.
	dbs := make([]*state.VersionedDB, len(cfg.Endorsers))
	builders := make([]endorsement.Builder, len(cfg.Endorsers))
	ends := make([]*endorser.Endorser, len(cfg.Endorsers))
	syncs := make([]*network.Synchronizer, len(cfg.Endorsers))
	for i, ecfg := range cfg.Endorsers {
		dbs[i], builders[i], ends[i], syncs[i] = newEndorser(t, logger, ecfg, cfg.Network.Channel, cfg.Network.Namespace, evmConfig, networkType)
	}

	// Start sync goroutines; they run until the test context is cancelled.
	// Register a cleanup to wait for each goroutine to exit before the test
	// is considered done, preventing goroutine accumulation across subtests.
	if networkType != "bypass" {
		for _, s := range syncs {
			done := make(chan struct{})
			go func() {
				defer close(done)
				_ = s.Start(t.Context())
			}()
			t.Cleanup(func() {
				<-done
				_ = s.Close()
			})
		}
	}

	// Build identity deserializer.
	var des api.IdentityDeserializer
	if cfg.Endorsers[0].MspDir != "" {
		var err error
		des, err = api.NewFabricDeserializer(cfg.Endorsers[0].MspDir, cfg.Endorsers[0].MspID)
		if err != nil {
			return nil, nil, err
		}
	} else {
		des = &localIdDeserialiser{}
	}

	// Build endorsement API.
	endAPI := make([]core.Endorser, len(ends))
	for i, end := range ends {
		endAPI[i] = api.New(cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion, des, end, ethChainConfig)
	}

	// Build gateway signer.
	var gwSigner sdk.Signer
	if cfg.Gateway.SignerMSPDir != "" {
		var err error
		gwSigner, err = identity.SignerFromMSP(cfg.Gateway.SignerMSPDir, cfg.Gateway.SignerMSPID)
		if err != nil {
			return nil, nil, err
		}
	} else {
		gwSigner = localSigner{}
	}

	ec, err := core.NewEndorsementClient(endAPI, gwSigner, cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion, ethChainConfig)
	if err != nil {
		return nil, nil, err
	}

	// Build submitter.
	orderers := make([]network.OrdererConf, len(cfg.Gateway.Orderers))
	for i, o := range cfg.Gateway.Orderers {
		orderers[i] = network.OrdererConf{Address: o.Address, TLSPath: o.TLSPath}
	}
	var submitter core.Submitter
	switch networkType {
	case "fabric":
		submitter, err = nfab.NewSubmitter(orderers, gwSigner, cfg.Gateway.SubmitWaitTime, logger)
	case "fabric-x":
		submitter, err = nfabx.NewSubmitter(orderers, gwSigner, cfg.Gateway.SubmitWaitTime, logger)
	case "bypass":
		submitter = local.NewLocalSubmitter(dbs[0], cfg.Network.Channel, cfg.Network.Namespace, nfab.NewTxPackager(gwSigner), bfab.NewBlockParser(logger), false)
	default:
		return nil, nil, fmt.Errorf("unsupported network type: %s", networkType)
	}
	if err != nil {
		return nil, nil, err
	}

	gw, err := core.New(ec, submitter, nil, cfg.Network.ChainID)
	if err != nil {
		return nil, nil, err
	}

	primer, err := NewStatePrimer(dbs[0], cfg.Network.Namespace, gwSigner, builders, submitter, cfg.Network.Channel, cfg.Network.NsVersion, networkType == "fabric-x")
	if err != nil {
		return nil, nil, err
	}

	th := &TestHarness{
		gateways:       []*core.Gateway{gw},
		endorsers:      ends,
		ethChainConfig: ethChainConfig,
		primer:         primer,
	}

	if err := th.PrimeStateFromJSON(t.Context(), primeDBPath); err != nil {
		return nil, nil, err
	}

	return th, syncs, nil
}

// newLocalTestHarness commits updates directly to the DB, bypassing peers and orderers.
// Exported for use by eth-tests package.
func newLocalTestHarness(t *testing.T, logger sdk.Logger, evmConfig *endorser.EVMConfig, primeDbPath, networkType string) (*TestHarness, error) {
	var orderer, peer string

	if networkType == "bypass" {
		orderer = "127.0.0.1:1337"
		peer = "127.0.0.1:1337"
	} else {
		nw, err := fabrictest.Start("basic", networkType, fabrictest.BatchingConfig{})
		if err != nil {
			t.Fatalf("fabrictest.Start: %v", err)
		}
		t.Cleanup(nw.Stop)
		orderer = nw.OrdererAddr
		peer = nw.PeerAddr
	}

	tname := strings.ReplaceAll(strings.ReplaceAll(t.Name(), "/", "_"), ".", "-")
	dir := t.TempDir()
	cfg := config.Config{
		Network: config.Network{
			Channel:   "mychannel",
			Namespace: "basic",
			NsVersion: "1.0",
			ChainID:   31337,
		},
		Gateway: config.Gateway{
			DbConnStr:      filepath.Join(dir, tname+"gateway.db"),
			SubmitWaitTime: 10 * time.Millisecond,
			SyncTimeout:    2 * time.Second,
			Orderers:       []config.Orderer{{Address: orderer}},
		},
		Endorsers: []econf.Endorser{
			{
				PeerAddr:  peer,
				Name:      "endorser1",
				DbConnStr: filepath.Join(dir, tname+"endorser1.db"),
			},
		},
	}
	if evmConfig != nil && evmConfig.ChainConfig != nil {
		cfg.Network.ChainID = evmConfig.ChainConfig.ChainID.Int64()
	}

	th, _, err := buildTestHarness(t, logger, cfg, evmConfig, primeDbPath, networkType)
	if err != nil {
		return nil, err
	}

	return th, nil
}

// newFabricTestHarness returns a client for integration testing with access to a peer, orderer and local committer.
// It follows the directory structure of a fabric samples test network.
// Exported for use by eth-tests package.
func newFabricTestHarness(t *testing.T, logger sdk.Logger, ethChainConfig *params.ChainConfig, primeDbPath string) (*TestHarness, error) {
	// Use TESTDATA environment variable if set, otherwise find project root
	var testdataDir string
	if envTestdata := os.Getenv("TESTDATA"); envTestdata != "" {
		testdataDir = envTestdata
	} else {
		projectRoot, err := findProjectRoot()
		if err != nil {
			cwd, _ := os.Getwd()
			testdataDir = path.Join(cwd, "..", "testdata")
		} else {
			testdataDir = path.Join(projectRoot, "testdata")
		}
	}

	cfg := FabricSamplesConfig(testdataDir)
	if ethChainConfig != nil {
		cfg.Network.ChainID = ethChainConfig.ChainID.Int64()
	}

	th, syncs, err := buildTestHarness(t, logger, cfg, &endorser.EVMConfig{ChainConfig: ethChainConfig}, primeDbPath, "fabric")
	if err != nil {
		return nil, err
	}

	for _, s := range syncs {
		if err := s.WaitUntilSynced(t.Context(), cfg.Gateway.SyncTimeout); err != nil {
			return nil, err
		}
	}

	return th, nil
}

// newFabricXTestHarness returns a client for integration testing with access to a peer, orderer and local committer.
// It follows the directory structure of a fabric samples test network.
// Exported for use by eth-tests package.
func newFabricXTestHarness(t *testing.T, logger sdk.Logger, ethChainConfig *params.ChainConfig, primeDbPath string) (*TestHarness, error) {
	cfg := XTestCommitterConfig()
	if ethChainConfig != nil {
		cfg.Network.ChainID = ethChainConfig.ChainID.Int64()
	}

	th, _, err := buildTestHarness(t, logger, cfg, &endorser.EVMConfig{ChainConfig: ethChainConfig}, primeDbPath, "fabric-x")
	if err != nil {
		return nil, err
	}

	time.Sleep(2 * time.Second) // wait until synced...

	return th, nil
}

func newEndorser(t *testing.T, logger sdk.Logger, cfg econf.Endorser, channel, namespace string, evmConfig *endorser.EVMConfig, typ string) (*state.VersionedDB, endorsement.Builder, *endorser.Endorser, *network.Synchronizer) {
	t.Helper()

	var signer sdk.Signer
	if cfg.MspDir == "" {
		signer = &localSigner{}
	} else {
		var err error
		signer, err = identity.SignerFromMSP(cfg.MspDir, cfg.MspID)
		if err != nil {
			t.Fatalf("SignerFromMSP: %v", err)
		}
	}

	writeDB, err := state.NewWriteDB(channel, cfg.DbConnStr)
	if err != nil {
		t.Fatalf("NewWriteDB: %v", err)
	}
	t.Cleanup(func() { writeDB.Close() })

	// the shape of endorsements and blocks differs per ledger.
	var builder endorsement.Builder
	var processor network.BlockProcessor
	var monotonicVersions bool
	switch typ {
	case "bypass":
		fallthrough // with the bypass network type, we encode blocks like fabric
	case "fabric":
		processor = blocks.NewProcessor(bfab.NewBlockParser(logger), []blocks.BlockHandler{writeDB})
		builder = efab.NewEndorsementBuilder(signer)
	case "fabric-x":
		processor = blocks.NewProcessor(bfabx.NewBlockParser(logger), []blocks.BlockHandler{writeDB})
		builder = efabx.NewEndorsementBuilder(signer)
		monotonicVersions = true
	default:
		t.Fatalf("networkType must be fabric or fabric-x, got %q", typ)
	}

	if evmConfig == nil {
		evmConfig = &endorser.EVMConfig{}
	}

	end, err := endorser.New(endorser.NewEVMEngine(namespace, writeDB, evmConfig, monotonicVersions), builder)
	if err != nil {
		t.Fatalf("endorser.New: %v", err)
	}

	readDB, err := state.NewReadDB(channel, cfg.DbConnStr)
	if err != nil {
		t.Fatalf("NewReadDB: %v", err)
	}
	t.Cleanup(func() { readDB.Close() })

	sync, err := network.NewSynchronizer(readDB, channel, cfg.PeerAddr, cfg.PeerTLS, signer, processor, logger)
	if err != nil {
		t.Fatalf("NewSynchronizer: %v", err)
	}

	return writeDB, builder, end, sync
}

// TestHarness provides access to gateways and endorsers for testing.
// Exported for use by eth-tests package.
type TestHarness struct {
	gateways       []*core.Gateway
	endorsers      []*endorser.Endorser
	ethChainConfig *params.ChainConfig
	primer         *StatePrimer
}

func (th *TestHarness) Stop() error {
	errs := []error{}
	for _, n := range th.gateways {
		if err := n.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func processCommon(t *testing.T, gw *core.Gateway, commit bool, tx *types.Transaction, blockInfo *utils.BlockInfo) sdk.Endorsement {
	t.Helper()
	env, err := gw.ExecuteEthTx(t.Context(), tx, blockInfo)
	if err != nil {
		t.Fatal(err)
	}
	if commit {
		if err := gw.SubmitFabricTx(t.Context(), env); err != nil {
			t.Fatal(err)
		}
	}

	return env
}

func getEndorsedTxForSmartContractCall(t *testing.T, client *EthClient, addr common.Address, gw *core.Gateway, method string, blockInfo *utils.BlockInfo, args ...any) sdk.Endorsement {
	t.Helper()
	tx, err := client.txForCall(t.Context(), gw, &addr, method, blockInfo, args...)
	if err != nil {
		t.Fatal(err)
	}

	return processCommon(t, gw, false, tx, blockInfo)
}

func newNativeEthClient(gw *core.Gateway) (*ethclient.Client, error) {
	rpcServer, err := gwapi.NewServer(gw)
	if err != nil {
		return nil, err
	}

	client := rpc.DialInProc(rpcServer)
	return ethclient.NewClient(client), nil
}

func deploySmartContract(t *testing.T, gw *core.Gateway, client *EthClient, args ...any) common.Address {
	t.Helper()

	ec, err := newNativeEthClient(gw)
	if err != nil {
		t.Fatal(err)
	}

	tx, addr, err := client.txForDeploy(t.Context(), gw, nil, args...)
	if err != nil {
		t.Fatal(err)
	}

	err = ec.SendTransaction(t.Context(), tx)
	if err != nil {
		t.Fatal(err)
	}

	return addr
}

func callSmartContract(t *testing.T, client *EthClient, addr common.Address, gw *core.Gateway, method string, blockInfo *utils.BlockInfo, args ...any) {
	t.Helper()

	ec, err := newNativeEthClient(gw)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := client.txForCall(t.Context(), gw, &addr, method, blockInfo, args...)
	if err != nil {
		t.Fatal(err)
	}

	err = ec.SendTransaction(t.Context(), tx)
	if err != nil {
		t.Fatal(err)
	}
}

func querySmartContract(t *testing.T, gw *core.Gateway, client *EthClient, addr common.Address, method string, params ...any) []any {
	t.Helper()

	ec, err := newNativeEthClient(gw)
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
func querySmartContractExpect(t *testing.T, client *EthClient, addr common.Address, th *TestHarness, expected any, method string, params ...any) {
	for _, gw := range th.gateways {
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
