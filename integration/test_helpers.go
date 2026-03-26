/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"cmp"
	"context"
	"errors"
	"math/big"
	"os"
	"path"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"golang.org/x/sync/errgroup"

	"github.com/hyperledger/fabric-x-evm/endorser"
	"github.com/hyperledger/fabric-x-evm/endorser/api"
	"github.com/hyperledger/fabric-x-evm/endorser/config"
	gwapi "github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/utils"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	bfab "github.com/hyperledger/fabric-x-sdk/blocks/fabric"
	bfabx "github.com/hyperledger/fabric-x-sdk/blocks/fabricx"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	efab "github.com/hyperledger/fabric-x-sdk/endorsement/fabric"
	efabx "github.com/hyperledger/fabric-x-sdk/endorsement/fabricx"
	"github.com/hyperledger/fabric-x-sdk/identity"
	"github.com/hyperledger/fabric-x-sdk/local"
	"github.com/hyperledger/fabric-x-sdk/network"
	nfab "github.com/hyperledger/fabric-x-sdk/network/fabric"
	nfabx "github.com/hyperledger/fabric-x-sdk/network/fabricx"
	"github.com/hyperledger/fabric-x-sdk/state"
)

type localSigner struct{}

func (*localSigner) Sign(msg []byte) ([]byte, error) {
	return []byte("signature"), nil
}

func (*localSigner) Serialize() ([]byte, error) {
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

// NewStatePrimer creates a StatePrimer for programmatic state priming.
// This allows setting nonces, code, balances, and storage directly from variables
// without needing a JSON file. Can be called at any time during tests.
//
// Example usage:
//
//	err := th.NewStatePrimer().
//	    SetNonce(addr1, 5).
//	    SetCode(addr2, contractCode).
//	    SetStorage(addr2, storageMap).
//	    Commit(ctx)
func (th *TestHarness) NewStatePrimer() (*StatePrimer, error) {
	return NewStatePrimer(th.db, th.namespace, th.signer, th.builders, th.submitter, th.channel, th.nsVersion, th.monotonicVersions)
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

	// Use the new StatePrimer builder pattern
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

// newLocalXTestHarness is the internal version for integration tests.
func newLocalXTestHarness(ctx context.Context, logger sdk.Logger, ethChainConfig *params.ChainConfig, primeDbPath string) (*TestHarness, error) {
	return NewLocalXTestHarness(ctx, logger, ethChainConfig, primeDbPath)
}

// NewLocalXTestHarness commits updates directly to the DB, bypassing peers and orderers.
// Exported for use by eth-tests package.
func NewLocalXTestHarness(ctx context.Context, logger sdk.Logger, ethChainConfig *params.ChainConfig, primeDbPath string) (*TestHarness, error) {
	cfg := XTestCommitterConfig()
	if ethChainConfig != nil {
		cfg.Network.ChainID = ethChainConfig.ChainID.Int64()
	}

	// no certificates for local tests
	cfg.Endorsers[0].PeerTLS = ""
	cfg.Endorsers[0].MspDir = ""

	db, builder, end, sync := newEndorser(logger, cfg.Endorsers[0], cfg.Network.Channel, cfg.Network.Namespace, ethChainConfig, "fabric-x")
	syncG, syncGCtx := errgroup.WithContext(context.Background())
	syncG.Go(func() error { return sync.Start(syncGCtx) })

	signer := &localSigner{}
	endAPI := []core.Endorser{api.New(cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion, &localIdDeserialiser{}, end, ethChainConfig)}
	ec, err := core.NewEndorsementClient(endAPI, signer, cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion, ethChainConfig)
	if err != nil {
		return nil, err
	}

	sub := local.NewLocalSubmitter(db, cfg.Network.Channel, cfg.Network.Namespace, nfabx.NewTxPackager(signer), bfabx.NewBlockParser(&TestLogger{ID: "local"}), true)

	gw, err := core.New(ec, sub, nil, cfg.Network.ChainID)
	if err != nil {
		return nil, err
	}

	th := &TestHarness{
		gateways:          []*core.Gateway{gw},
		endorsers:         []*endorser.Endorser{end},
		synchronizers:     nil,
		ethChainConfig:    ethChainConfig,
		db:                db,
		submitter:         sub,
		signer:            signer,
		builders:          []endorsement.Builder{builder},
		channel:           cfg.Network.Channel,
		namespace:         cfg.Network.Namespace,
		nsVersion:         cfg.Network.NsVersion,
		monotonicVersions: true,
	}

	// Prime state from JSON (semantic priming)
	err = th.PrimeStateFromJSON(ctx, primeDbPath)
	if err != nil {
		return nil, err
	}

	logger.Infof("local-x test harness is ready!")

	return th, nil
}

// newLocalTestHarness is the internal version for integration tests.
func newLocalTestHarness(ctx context.Context, logger sdk.Logger, ethChainConfig *params.ChainConfig, primeDbPath string) (*TestHarness, error) {
	return NewLocalTestHarness(ctx, logger, ethChainConfig, primeDbPath)
}

// NewLocalTestHarness commits updates directly to the DB, bypassing peers and orderers.
// Exported for use by eth-tests package.
func NewLocalTestHarness(ctx context.Context, logger sdk.Logger, ethChainConfig *params.ChainConfig, primeDbPath string) (*TestHarness, error) {
	cwd, _ := os.Getwd()
	testdataDir := cmp.Or(os.Getenv("TESTDATA"), path.Join(cwd, "..", "testdata"))
	cfg := FabricSamplesConfig(testdataDir)
	if ethChainConfig != nil {
		cfg.Network.ChainID = ethChainConfig.ChainID.Int64()
	}

	// no certificates for local tests
	cfg.Endorsers[0].PeerTLS = ""
	cfg.Endorsers[0].MspDir = ""

	db, builder, end, sync := newEndorser(logger, cfg.Endorsers[0], cfg.Network.Channel, cfg.Network.Namespace, ethChainConfig, "fabric")
	syncG, syncGCtx := errgroup.WithContext(context.Background())
	syncG.Go(func() error { return sync.Start(syncGCtx) })

	signer := &localSigner{}
	endAPI := []core.Endorser{api.New(cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion, &localIdDeserialiser{}, end, ethChainConfig)}
	ec, err := core.NewEndorsementClient(endAPI, signer, cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion, ethChainConfig)
	if err != nil {
		return nil, err
	}

	sub := local.NewLocalSubmitter(db, cfg.Network.Channel, cfg.Network.Namespace, nfab.NewTxPackager(signer), bfab.NewBlockParser(&TestLogger{ID: "local"}), false)

	gw, err := core.New(ec, sub, nil, cfg.Network.ChainID)
	if err != nil {
		return nil, err
	}

	th := &TestHarness{
		gateways:          []*core.Gateway{gw},
		endorsers:         []*endorser.Endorser{end},
		synchronizers:     nil,
		ethChainConfig:    ethChainConfig,
		db:                db,
		submitter:         sub,
		signer:            signer,
		builders:          []endorsement.Builder{builder},
		channel:           cfg.Network.Channel,
		namespace:         cfg.Network.Namespace,
		nsVersion:         cfg.Network.NsVersion,
		monotonicVersions: false,
	}

	// Prime state from JSON (semantic priming)
	err = th.PrimeStateFromJSON(ctx, primeDbPath)
	if err != nil {
		return nil, err
	}

	logger.Infof("local test harness is ready!")

	return th, nil
}

// newFabricTestHarness is the internal version for integration tests.
func newFabricTestHarness(ctx context.Context, logger sdk.Logger, ethChainConfig *params.ChainConfig, primeDbPath string) (*TestHarness, error) {
	return NewFabricTestHarness(ctx, logger, ethChainConfig, primeDbPath)
}

// NewFabricTestHarness returns a client for integration testing with access to a peer, orderer and local committer.
// It follows the directory structure of a fabric samples test network.
// Exported for use by eth-tests package.
func NewFabricTestHarness(ctx context.Context, logger sdk.Logger, ethChainConfig *params.ChainConfig, primeDbPath string) (*TestHarness, error) {
	// Use TESTDATA environment variable if set, otherwise find project root
	var testdataDir string
	if envTestdata := os.Getenv("TESTDATA"); envTestdata != "" {
		testdataDir = envTestdata
	} else {
		// Find project root and construct path to testdata
		projectRoot, err := findProjectRoot()
		if err != nil {
			// Fallback to relative path (works from integration/ directory)
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

	db1, builder1, end1, sync1 := newEndorser(logger, cfg.Endorsers[0], cfg.Network.Channel, cfg.Network.Namespace, ethChainConfig, "fabric")
	_, builder2, end2, sync2 := newEndorser(logger, cfg.Endorsers[1], cfg.Network.Channel, cfg.Network.Namespace, ethChainConfig, "fabric")
	syncG, syncGCtx := errgroup.WithContext(ctx)
	syncG.Go(func() error { return sync1.Start(syncGCtx) })
	syncG.Go(func() error { return sync2.Start(syncGCtx) })

	// Endorsement API
	des, err := api.NewFabricDeserializer(cfg.Endorsers[0].MspDir, cfg.Endorsers[0].MspID)
	if err != nil {
		return nil, err
	}
	endAPI := []core.Endorser{
		api.New(cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion, des, end1, ethChainConfig),
		api.New(cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion, des, end2, ethChainConfig),
	}

	// Gateway
	gwSigner, err := identity.SignerFromMSP(cfg.Gateway.SignerMSPDir, cfg.Gateway.SignerMSPID)
	if err != nil {
		return nil, err
	}

	ec, err := core.NewEndorsementClient(endAPI, gwSigner, cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion, ethChainConfig)
	if err != nil {
		return nil, err
	}

	orderers := make([]network.OrdererConf, len(cfg.Gateway.Orderers))
	for i, o := range cfg.Gateway.Orderers {
		orderers[i] = network.OrdererConf{
			Address: o.Address,
			TLSPath: o.TLSPath,
		}
	}
	submitter, err := nfab.NewSubmitter(orderers, gwSigner, cfg.Gateway.SubmitWaitTime)
	if err != nil {
		return nil, err
	}

	gw, err := core.New(ec, submitter, nil, cfg.Network.ChainID)
	if err != nil {
		return nil, err
	}

	th := &TestHarness{
		gateways:          []*core.Gateway{gw},
		endorsers:         []*endorser.Endorser{end1, end2},
		synchronizers:     []*network.Synchronizer{sync1, sync2},
		ethChainConfig:    ethChainConfig,
		db:                db1,
		submitter:         submitter,
		signer:            gwSigner,
		builders:          []endorsement.Builder{builder1, builder2},
		channel:           cfg.Network.Channel,
		namespace:         cfg.Network.Namespace,
		nsVersion:         cfg.Network.NsVersion,
		monotonicVersions: false,
	}

	// Prime state from JSON (semantic priming)
	err = th.PrimeStateFromJSON(ctx, primeDbPath)
	if err != nil {
		return nil, err
	}

	if err := sync1.WaitUntilSynced(ctx, cfg.Gateway.SyncTimeout); err != nil {
		return nil, err
	}
	if err := sync2.WaitUntilSynced(ctx, cfg.Gateway.SyncTimeout); err != nil {
		return nil, err
	}

	logger.Infof("fabric test harness is ready!")

	return th, nil
}

// newFabricXTestHarness is the internal version for integration tests.
func newFabricXTestHarness(ctx context.Context, logger sdk.Logger, ethChainConfig *params.ChainConfig, primeDbPath string) (*TestHarness, error) {
	return NewFabricXTestHarness(ctx, logger, ethChainConfig, primeDbPath)
}

// NewFabricXTestHarness returns a client for integration testing with access to a peer, orderer and local committer.
// It follows the directory structure of a fabric samples test network.
// Exported for use by eth-tests package.
func NewFabricXTestHarness(ctx context.Context, logger sdk.Logger, ethChainConfig *params.ChainConfig, primeDbPath string) (*TestHarness, error) {
	cfg := XTestCommitterConfig()
	if ethChainConfig != nil {
		cfg.Network.ChainID = ethChainConfig.ChainID.Int64()
	}

	db1, builder1, end1, sync1 := newEndorser(logger, cfg.Endorsers[0], cfg.Network.Channel, cfg.Network.Namespace, ethChainConfig, "fabric-x")
	syncG, syncGCtx := errgroup.WithContext(ctx)
	syncG.Go(func() error { return sync1.Start(syncGCtx) })

	// Endorsement API
	des, err := api.NewFabricDeserializer(cfg.Endorsers[0].MspDir, cfg.Endorsers[0].MspID)
	if err != nil {
		return nil, err
	}
	endAPI := []core.Endorser{
		api.New(cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion, des, end1, ethChainConfig),
	}

	// Gateway
	gwSigner, err := identity.SignerFromMSP(cfg.Gateway.SignerMSPDir, cfg.Gateway.SignerMSPID)
	if err != nil {
		return nil, err
	}

	ec, err := core.NewEndorsementClient(endAPI, gwSigner, cfg.Network.Channel, cfg.Network.Namespace, cfg.Network.NsVersion, ethChainConfig)
	if err != nil {
		return nil, err
	}

	orderers := make([]network.OrdererConf, len(cfg.Gateway.Orderers))
	for i, o := range cfg.Gateway.Orderers {
		orderers[i] = network.OrdererConf{
			Address: o.Address,
			TLSPath: o.TLSPath,
		}
	}
	submitter, err := nfabx.NewSubmitter(orderers, gwSigner, 200*time.Millisecond)
	if err != nil {
		return nil, err
	}

	gw, err := core.New(ec, submitter, nil, cfg.Network.ChainID)
	if err != nil {
		return nil, err
	}

	th := &TestHarness{
		gateways:          []*core.Gateway{gw},
		endorsers:         []*endorser.Endorser{end1},
		synchronizers:     []*network.Synchronizer{sync1},
		ethChainConfig:    ethChainConfig,
		db:                db1,
		submitter:         submitter,
		signer:            gwSigner,
		builders:          []endorsement.Builder{builder1},
		channel:           cfg.Network.Channel,
		namespace:         cfg.Network.Namespace,
		nsVersion:         cfg.Network.NsVersion,
		monotonicVersions: true,
	}

	// Prime state from JSON (semantic priming)
	err = th.PrimeStateFromJSON(ctx, primeDbPath)
	if err != nil {
		return nil, err
	}

	time.Sleep(2 * time.Second) // wait until synced...

	logger.Infof("fabric-x test harness is ready!")

	return th, nil
}

func newEndorser(logger sdk.Logger, cfg config.Endorser, channel, namespace string, ethChainConfig *params.ChainConfig, typ string) (*state.VersionedDB, endorsement.Builder, *endorser.Endorser, *network.Synchronizer) {
	var err error

	// if mspDir is empty, we mock the signer
	var signer sdk.Signer
	if cfg.MspDir == "" {
		signer = &localSigner{}
		logger.Infof("using mock signer")
	} else {
		signer, err = identity.SignerFromMSP(cfg.MspDir, cfg.MspID)
		if err != nil {
			panic(err)
		}
	}

	writeDB, err := state.NewWriteDB(channel, cfg.DbConnStr)
	if err != nil {
		panic(err)
	}

	// the shape of endorsements and blocks differs per ledger.
	var builder endorsement.Builder
	var processor network.BlockProcessor
	var monotonicVersions bool
	switch typ {
	case "fabric":
		processor = blocks.NewProcessor(bfab.NewBlockParser(logger), []blocks.BlockHandler{writeDB})
		builder = efab.NewEndorsementBuilder(signer)
		monotonicVersions = false
	case "fabric-x":
		processor = blocks.NewProcessor(bfabx.NewBlockParser(logger), []blocks.BlockHandler{writeDB})
		builder = efabx.NewEndorsementBuilder(signer)
		monotonicVersions = true
	default:
		panic("typ must be fabric or fabric-x")
	}

	end, err := endorser.New(endorser.NewEVMEngine(namespace, writeDB, &endorser.EVMConfig{ChainConfig: ethChainConfig}, monotonicVersions), builder)
	if err != nil {
		panic(err)
	}

	readDB, err := state.NewReadDB(channel, cfg.DbConnStr)
	if err != nil {
		panic(err)
	}

	sync, err := network.NewSynchronizer(readDB, channel, cfg.PeerAddr, cfg.PeerTLS, signer, processor, logger)
	if err != nil {
		panic(err)
	}

	return writeDB, builder, end, sync
}

// TestHarness provides access to gateways and endorsers for testing.
// Exported for use by eth-tests package.
type TestHarness struct {
	gateways          []*core.Gateway
	endorsers         []*endorser.Endorser
	synchronizers     []*network.Synchronizer
	ethChainConfig    *params.ChainConfig
	db                *state.VersionedDB
	submitter         core.Submitter
	signer            sdk.Signer
	builders          []endorsement.Builder
	channel           string
	namespace         string
	nsVersion         string
	monotonicVersions bool
	primedEthStateDB  *ethstate.StateDB // Stores the ethStateDB from state priming for reuse
}

type AllocEntry struct {
	Balance string            `json:"balance"`
	Code    string            `json:"code"`
	Nonce   string            `json:"nonce"`
	Storage map[string]string `json:"storage"`
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
