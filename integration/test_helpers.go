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
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/protoutil"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser"
	econf "github.com/hyperledger/fabric-x-evm/endorser/config"
	gwapi "github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/config"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/utils"
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

// PrimeGenesisAlloc primes ledger state from an Ethereum genesis allocation and
// injects the resulting ethStateDB into all endorsers for state reuse.
func (th *TestHarness) PrimeGenesisAlloc(ctx context.Context, pre types.GenesisAlloc, wait bool) error {
	if len(pre) == 0 {
		return nil
	}

	primer, err := th.NewStatePrimer()
	if err != nil {
		return err
	}

	// Sort addresses to ensure deterministic account creation order
	// (Go map iteration order is random, which affects the state trie structure)
	var addresses []ethcommon.Address
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
		var storage map[ethcommon.Hash]ethcommon.Hash
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
	if err := primer.Commit(ctx, wait); err != nil {
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
func buildTestHarness(t *testing.T, logger sdk.Logger, cfg config.Config, evmConfig endorser.EVMConfig, primeDBPath string, bypass bool) (*TestHarness, *network.Synchronizer, error) {
	t.Helper()

	// Derive ChainConfig from cfg.Network.ChainID when not explicitly provided.
	if evmConfig.ChainConfig == nil {
		evmConfig.ChainConfig = common.BuildChainConfig(cfg.Network.ChainID)
	}

	// Build all endorsers.
	dbs := make([]*state.VersionedDB, len(cfg.Endorsers))
	builders := make([]endorsement.Builder, len(cfg.Endorsers))
	ends := make([]*endorser.Endorser, len(cfg.Endorsers))
	for i, ecfg := range cfg.Endorsers {
		dbs[i], builders[i], ends[i] = newEndorser(t, logger, ecfg, cfg.Network.Channel, cfg.Network.Namespace, evmConfig, cfg.Network.Protocol)
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

	ec, err := core.NewEndorsementClient(ends, gwSigner, cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion)
	if err != nil {
		return nil, nil, err
	}

	chain, err := core.NewChain(cfg.Gateway.DbConnStr, cfg.Gateway.TrieDBPath)
	if err != nil {
		return nil, nil, err
	}
	t.Cleanup(func() { chain.Close() })

	// Build submitter.
	orderers := make([]network.OrdererConf, len(cfg.Gateway.Orderers))
	for i, o := range cfg.Gateway.Orderers {
		orderers[i] = o.ToOrdererConf()
	}

	var submitter core.Submitter
	var sync *network.Synchronizer
	var err1 error
	if bypass {
		submitter = local.NewLocalSubmitter(dbs[0], cfg.Network.Channel, cfg.Network.Namespace, nfab.NewTxPackager(gwSigner), bfab.NewBlockParser(logger), false)
	} else {
		handlers := make([]blocks.BlockHandler, 0, len(dbs)+1)
		for _, db := range dbs {
			handlers = append(handlers, db)
		}
		// by adding `chain` as last handler, we're sure that by the time the
		// gateway sees the transaction as committed, all endorsers will have
		// updated their ledger state
		handlers = append(handlers, chain)

		switch cfg.Network.Protocol {
		case "fabric":
			sync, err = nfab.NewSynchronizer(chain, cfg.Network.Channel, cfg.Gateway.Committer.ToPeerConf(), gwSigner, logger, handlers...)
			submitter, err1 = nfab.NewSubmitter(orderers, gwSigner, 0, logger)
		case "fabric-x", "":
			sync, err = nfabx.NewSynchronizer(chain, cfg.Network.Channel, cfg.Gateway.Committer.ToPeerConf(), gwSigner, logger, handlers...)
			submitter, err1 = nfabx.NewSubmitter(orderers, gwSigner, 0, logger)
		default:
			return nil, nil, fmt.Errorf("unsupported protocol: %q", cfg.Network.Protocol)
		}
		if err := errors.Join(err, err1); err != nil {
			return nil, nil, err
		}
	}

	gw, err := core.New(ec, submitter, chain, cfg.Network.ChainID, cfg.Gateway.WorkerCount)
	if err != nil {
		return nil, nil, err
	}

	if !bypass {
		go func() error { return sync.Start(t.Context()) }()
	}

	// Start gateway worker pool for tests
	gw.Start(t.Context())
	t.Cleanup(func() { gw.Stop() })

	primer, err := NewStatePrimer(gw, dbs[0], cfg.Network.Namespace, gwSigner, builders, cfg.Network.Channel, cfg.Network.NsVersion, cfg.Network.Protocol == "fabric-x")
	if err != nil {
		return nil, nil, err
	}

	th := &TestHarness{
		Gateways:       []*core.Gateway{gw},
		endorsers:      ends,
		ethChainConfig: evmConfig.ChainConfig,
		Primer:         primer,
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

// newLocalTestHarness commits updates directly to the DB, bypassing peers and orderers.
// Exported for use by eth-tests package.
func NewLocalTestHarness(t *testing.T, logger sdk.Logger, evmConfig endorser.EVMConfig, primeDbPath, networkType string, configOverrides map[string]any) (*TestHarness, error) {
	bypass := networkType == "bypass"

	orderer := &common.Endpoint{Host: "127.0.0.1", Port: 1337}
	peer := &common.Endpoint{Host: "127.0.0.1", Port: 1337}

	if !bypass {
		nw, err := fabrictest.Start("basic", networkType, fabrictest.Config{})
		if err != nil {
			t.Fatalf("fabrictest.Start: %v", err)
		}
		t.Cleanup(nw.Stop)
		orderer.Port = nw.OrdererPort
		peer.Port = nw.PeerPort
	}

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
			DbConnStr:   filepath.Join(dir, tname+"gateway.db"),
			TrieDBPath:  filepath.Join(dir, tname+"triedb.db"),
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
				DbConnStr: filepath.Join(dir, tname+"endorser1.db"),
			},
		},
	}
	if err := applyConfigOverrides(&cfg, configOverrides); err != nil {
		return nil, err
	}

	th, _, err := buildTestHarness(t, logger, cfg, evmConfig, primeDbPath, bypass)
	if err != nil {
		return nil, err
	}

	return th, nil
}

// newFabricTestHarness returns a client for integration testing with access to a peer, orderer and local committer.
// It follows the directory structure of a fabric samples test network.
// Exported for use by eth-tests package.
func newFabricTestHarness(t *testing.T, logger sdk.Logger, evmConfig endorser.EVMConfig, primeDbPath string, configOverrides map[string]any) (*TestHarness, error) {
	// Use TESTDATA environment variable if set, otherwise find project root
	var testdataDir string
	if envTestdata := os.Getenv("TESTDATA"); envTestdata != "" {
		testdataDir = path.Join(envTestdata, "fablo")
	} else {
		projectRoot, err := findProjectRoot()
		if err != nil {
			cwd, _ := os.Getwd()
			testdataDir = path.Join(cwd, "..", "testdata", "fablo")
		} else {
			testdataDir = path.Join(projectRoot, "testdata", "fablo")
		}
	}

	cfg := FabloConfig(testdataDir)

	if err := applyConfigOverrides(&cfg, configOverrides); err != nil {
		return nil, err
	}

	th, sync, err := buildTestHarness(t, logger, cfg, evmConfig, primeDbPath, false)
	if err != nil {
		return nil, err
	}

	waitUntilSynced(t, sync, 10*time.Second)

	return th, nil
}

// NewFabricXTestHarness returns a client for integration testing with access to a peer, orderer and local committer.
// It follows the directory structure of a fabric samples test network.
// Exported for use by eth-tests package.
func NewFabricXTestHarness(t *testing.T, logger sdk.Logger, evmConfig endorser.EVMConfig, primeDbPath string, configOverrides map[string]any) (*TestHarness, error) {
	cfg := XTestCommitterConfig()

	if err := applyConfigOverrides(&cfg, configOverrides); err != nil {
		return nil, err
	}

	th, _, err := buildTestHarness(t, logger, cfg, evmConfig, primeDbPath, false)
	if err != nil {
		return nil, err
	}

	return th, nil
}

func newEndorser(t *testing.T, logger sdk.Logger, cfg econf.Endorser, channel, namespace string, evmConfig endorser.EVMConfig, protocol string) (*state.VersionedDB, endorsement.Builder, *endorser.Endorser) {
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

	writeDB, err := state.NewWriteDB(channel, cfg.DbConnStr)
	if err != nil {
		t.Fatalf("NewWriteDB: %v", err)
	}
	t.Cleanup(func() { writeDB.Close() })

	readDB, err := state.NewReadDB(channel, cfg.DbConnStr)
	if err != nil {
		t.Fatalf("NewReadDB: %v", err)
	}
	t.Cleanup(func() { readDB.Close() })

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
	if err != nil {
		t.Fatalf("NewSynchronizer: %v", err)
	}

	end, err := endorser.New(
		endorser.NewEVMEngine(namespace, writeDB, evmConfig, monotonicVersions),
		builder,
		evmConfig.ChainConfig.ChainID.Int64(),
	)
	if err != nil {
		t.Fatalf("endorser.New: %v", err)
	}

	return writeDB, builder, end
}

// TestHarness provides access to gateways and endorsers for testing.
// Exported for use by eth-tests package.
type TestHarness struct {
	Gateways       []*core.Gateway
	endorsers      []*endorser.Endorser
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

		ec, err := NewNativeEthClient(gw)
		if err != nil {
			t.Fatal(err)
		}

		waitForCommitT(t, ec, tx)
	}

	return env
}

func getEndorsedTxForSmartContractCall(t *testing.T, client *EthClient, addr ethcommon.Address, gw *core.Gateway, method string, blockInfo *utils.BlockInfo, args ...any) sdk.Endorsement {
	t.Helper()
	tx, err := client.TxForCall(t.Context(), gw, &addr, method, blockInfo, args...)
	if err != nil {
		t.Fatal(err)
	}

	return processCommon(t, gw, false, tx, blockInfo)
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

	tx, addr, err := client.txForDeploy(t.Context(), gw, nil, args...)
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

func callSmartContract(t *testing.T, client *EthClient, addr ethcommon.Address, gw *core.Gateway, method string, blockInfo *utils.BlockInfo, args ...any) {
	t.Helper()

	ec, err := NewNativeEthClient(gw)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := client.TxForCall(t.Context(), gw, &addr, method, blockInfo, args...)
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
