/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/hyperledger/fabric-protos-go-apiv2/ledger/rwset"
	"github.com/hyperledger/fabric-protos-go-apiv2/ledger/rwset/kvrwset"
	"github.com/hyperledger/fabric-x-common/protoutil"
	econf "github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-evm/endorser/testimpl"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/gateway/storage/trie"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/protobuf/proto"
)

// verify_root is false by default, because many tests are konwn to fail.
// Set it to true to fix the tests one by one.
var verify_root = flag.Bool("verify_root", false, "Verify trie root computed by committer")

// want_very_slow is set when we want to run the tests that we typically skip because they are too slow
var want_very_slow = flag.Bool("very_slow", false, "Run the very slow tests that are otherwise blacklisted")

// loadSlow loads the test cases that are typically skipped because they are slow
func loadSlow(path string) (map[string]struct{}, error) {
	return loadList(path)
}

// loadSkip loads the test cases that are unsupported and should never run.
func loadSkip(path string) (map[string]struct{}, error) {
	return loadList(path)
}

// loadList reads a newline-delimited list of paths into a set, ignoring blank
// lines and lines starting with '#'. A missing file is treated as an empty list.
func loadList(path string) (map[string]struct{}, error) {
	set := make(map[string]struct{})

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return set, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			set[line] = struct{}{}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return set, nil
}

// findJSONFiles recursively finds all .json files in the given directory
func findJSONFiles(root string) ([]string, error) {
	var files []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(path, ".json") {
			files = append(files, path)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}

// filterSlowTests removes blacklisted files from the list
func filterSlowTests(files []string, slow map[string]struct{}, want_very_slow bool) []string {
	var filtered []string

	for _, file := range files {
		// Check if we want this test case
		if _, isSlow := slow[file]; isSlow == want_very_slow {
			filtered = append(filtered, file)
		}
	}

	return filtered
}

// filterSkippedTests removes always-skip files from the list.
func filterSkippedTests(files []string, skip map[string]struct{}) []string {
	var filtered []string

	for _, file := range files {
		if _, isSkip := skip[file]; !isSkip {
			filtered = append(filtered, file)
		}
	}

	return filtered
}

// executionSpecForks limits the suite to Osaka-forward, plus Prague/Cancun as cheap regression.
var executionSpecForks = map[string]struct{}{
	"Osaka":  {},
	"BPO1":   {},
	"BPO2":   {},
	"Prague": {},
	"Cancun": {},
}

// TestEthereumTests runs the execution-specs state_tests through the shared harness, limited to executionSpecForks.
//
// The fixtures are fetched on demand into testdata/execution-specs-tests/ (see `make fetch-execution-specs-tests`);
// this is the same corpus geth and other clients validate against.
func TestEthereumTests(t *testing.T) {
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr)) // disable grpc logging

	testsDir := filepath.Join("..", "testdata", "execution-specs-tests", "fixtures", "state_tests")
	if _, err := os.Stat(testsDir); os.IsNotExist(err) {
		t.Skipf("execution-specs fixtures not found at %s; run `make fetch-execution-specs-tests`", testsDir)
	}

	skip, err := loadSkip(filepath.Join("..", "testdata", "eth_tests.skip"))
	if err != nil {
		t.Fatalf("Failed to load skip list: %v", err)
	}
	t.Logf("Loaded skip list with %d entries", len(skip))

	slow, err := loadSlow(filepath.Join("..", "testdata", "eth_tests.slow"))
	if err != nil {
		t.Fatalf("Failed to load slow list: %v", err)
	}
	t.Logf("Loaded slow list with %d entries", len(slow))

	allFiles, err := findJSONFiles(testsDir)
	if err != nil {
		t.Fatalf("Failed to find test files: %v", err)
	}
	allFiles = filterSkippedTests(allFiles, skip)
	testFiles := filterSlowTests(allFiles, slow, *want_very_slow)
	t.Logf("Running %d state_tests files after filtering", len(testFiles))

	for _, testPath := range testFiles {
		t.Run(filepath.Base(testPath), func(t *testing.T) {
			runEthereumTestFile(t, testPath)
		})
	}
}

// runEthereumTestFile runs one fixtures file's state tests against the fork allowlist.
func runEthereumTestFile(t *testing.T, path string) {
	tests, err := ParseTestFile(path)
	if err != nil {
		t.Fatalf("Failed to parse test file: %v", err)
	}
	for name, test := range tests {
		// EEST bakes the fork into the test name, so skip a test with no allowlisted fork entirely.
		if !hasAllowlistedFork(test, executionSpecForks) {
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runStateTestSubtests(t, test, executionSpecForks)
		})
	}
}

// hasAllowlistedFork reports whether any subtest targets a fork in the allowlist.
func hasAllowlistedFork(test *StateTest, forkAllowlist map[string]struct{}) bool {
	for _, subtest := range test.Subtests() {
		if _, ok := forkAllowlist[subtest.Fork]; ok {
			return true
		}
	}
	return false
}

// runStateTestSubtests runs each fork/index subtest; a non-nil forkAllowlist restricts to those forks.
func runStateTestSubtests(t *testing.T, stateTest *StateTest, forkAllowlist map[string]struct{}) {
	for _, subtest := range stateTest.Subtests() {
		if forkAllowlist != nil {
			if _, ok := forkAllowlist[subtest.Fork]; !ok {
				continue
			}
		}
		key := fmt.Sprintf("%s/%d", subtest.Fork, subtest.Index)

		if testing.Short() && rand.Intn(2) > 0 {
			t.Skip("skipping in short mode")
		}

		// Test with hash scheme and trie (no snapshotter)
		t.Run(key, func(t *testing.T) {
			runEthereumTestConfig(t, stateTest, subtest, false, rawdb.HashScheme)
		})
	}
}

type nonceReader struct {
	db *state.StateDB
}

func (n *nonceReader) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	nonce := n.db.GetNonce(account)
	return nonce, nil
}

// runEthereumTestConfig executes a specific test configuration
func runEthereumTestConfig(t *testing.T, stateTest *StateTest, subtest StateSubtest, snapshotter bool, scheme string) {
	// Get the post-state to extract the correct indices and expected results
	post := stateTest.json.Post[subtest.Fork][subtest.Index]
	dataIndex := post.Indexes.Data
	gasIndex := post.Indexes.Gas
	valueIndex := post.Indexes.Value

	// Call prepareTestEnvironment to get context, config, block, and msg.
	// The returned StateTestState holds a TrieDB and optional Snapshots that must
	// be closed to stop the background snapshot-generator goroutine.
	st, config, block, _, context, prepareErr := stateTest.prepareTestEnvironment(subtest.Fork, subtest.Index, vm.Config{}, snapshotter, scheme)

	// Check if the error from prepareTestEnvironment is expected (e.g., blob count exceeded).
	// We must check this BEFORE trying to access block.Number() or block.Time().
	if prepareErr != nil {
		// Close the state before returning
		st.Close()
		if post.ExpectException != "" {
			// Error was expected, test passes
			t.Logf("WANTED: %s\n   GOT: %s\n", post.ExpectException, prepareErr.Error())
			return
		}
		// Error was not expected, test fails
		t.Fatalf("Failed to prepare test environment: %v", prepareErr)
	}

	// Build and sign transaction using the indices from post-state and the chain config
	// This must come AFTER the prepareErr check because block may be nil on error
	tx, err := buildTransaction(&stateTest.json.Tx, dataIndex, gasIndex, valueIndex, config, block.Number(), block.Time())
	if err != nil {
		st.Close()
		t.Fatalf("Failed to build transaction: %v", err)
	}

	// Close immediately: config/block/msg/context are plain values that don't reference the
	// StateDB/TrieDB/Snapshots, so we can stop the snapshot-generator goroutine right here
	// rather than relying on a defer that won't run if this goroutine later gets stuck.
	st.Close()

	// Create EVMConfig to pass to test harness
	evmConfig := execution.EVMConfig{
		ChainConfig: config,
		DebugLogs:   true,
	}

	// Create test harness with local backend and state priming, passing evmConfig and block context
	th, err := newEthereumTestHarness(t, evmConfig, stateTest.json.Pre, &context)
	if err != nil {
		t.Fatalf("Failed to create test harness: %v", err)
	}
	defer th.Stop()

	// run the tx through the pre-execution validation steps
	preExecErr := core.ValidateTx(t.Context(), tx, config, signerForTx(tx), &nonceReader{th.endorsers[0].(*testimpl.EndorserWrapper).GetEthStateDB()})

	// Execute transaction through gateway
	env, execErr := th.Gateways[0].ExecuteEthTx(t.Context(), tx)

	// Get expected root from post-state
	expectedRoot := common.Hash(post.Root)

	var actualRoot common.Hash
	// After execution, extract the ethStateDB and commit the ethereum state
	if len(th.endorsers) > 0 {
		ethStateDB := th.endorsers[0].(*testimpl.EndorserWrapper).GetEthStateDB()
		if ethStateDB != nil {
			// Commit the ethereum state
			root, err := ethStateDB.Commit(block.Number().Uint64(),
				config.IsEIP158(block.Number()),
				config.IsCancun(block.Number(), block.Time()))
			if err != nil {
				t.Fatalf("Failed to commit ethereum state: %v", err)
			}

			actualRoot = root
		}
	}

	if tx.Type() != types.BlobTxType && // our pre-execution validation doesn't support blob txes yet
		tx.Type() != types.SetCodeTxType && // nor set-code (EIP-7702) txes yet
		tx.Protected() { // our pre-execution validation only support protected transactions

		// err out if we got a pre-validation error which we wouldn't get again when endorsing.
		// ErrOversizedData is a mempool-only size limit, not a consensus rule, so don't fail on it.
		if preExecErr != nil && execErr == nil && !errors.Is(preExecErr, txpool.ErrOversizedData) {
			t.Fatalf("unexpected pre-validation error %q", preExecErr)
		}
	}

	// Check for expected errors
	if post.ExpectException != "" {
		if execErr == nil {
			t.Fatalf("expected error %q, got no error", post.ExpectException)
		}
		t.Logf("WANTED: %s\n   GOT: %s\n", post.ExpectException, execErr.Error())
		return
	}

	// Log execution result
	if execErr != nil {
		t.Fatalf("unexpected transaction execution error: %v", execErr)
	}

	// Verify root hash
	if expectedRoot != actualRoot {
		t.Fatalf("post state root mismatch: got %s, want %s", actualRoot.Hex(), expectedRoot.Hex())
	}
	if *verify_root {
		// Also verify via trie.Store (Chain path)
		txRWS, err := endorsementToRWS(env)
		if err != nil {
			t.Fatalf("extract tx RWS from endorsement: %v", err)
		}
		verifyTrieRoot(t, th.Primer.Writes(), txRWS, block.Number().Uint64(), expectedRoot)
	}
}

// endorsementToRWS extracts the blocks.ReadWriteSet from the first ProposalResponse
// in an sdk.Endorsement. It reverses the encoding done by endorsement/fabric.EndorsementBuilder.
func endorsementToRWS(env sdk.Endorsement) (blocks.ReadWriteSet, error) {
	if len(env.Responses) == 0 {
		return blocks.ReadWriteSet{}, nil
	}

	prp, err := protoutil.UnmarshalProposalResponsePayload(env.Responses[0].Payload)
	if err != nil {
		return blocks.ReadWriteSet{}, fmt.Errorf("unmarshal proposal response payload: %w", err)
	}

	ca, err := protoutil.UnmarshalChaincodeAction(prp.Extension)
	if err != nil {
		return blocks.ReadWriteSet{}, fmt.Errorf("unmarshal chaincode action: %w", err)
	}

	txrws := &rwset.TxReadWriteSet{}
	if err := proto.Unmarshal(ca.Results, txrws); err != nil {
		return blocks.ReadWriteSet{}, fmt.Errorf("unmarshal tx rws: %w", err)
	}

	var rws blocks.ReadWriteSet
	for _, ns := range txrws.NsRwset {
		kvRws := &kvrwset.KVRWSet{}
		if err := proto.Unmarshal(ns.Rwset, kvRws); err != nil {
			return blocks.ReadWriteSet{}, fmt.Errorf("unmarshal kv rws for ns %s: %w", ns.Namespace, err)
		}
		for _, w := range kvRws.Writes {
			rws.Writes = append(rws.Writes, blocks.KVWrite{
				Key:      w.Key,
				IsDelete: w.IsDelete,
				Value:    w.Value,
			})
		}
	}

	return rws, nil
}

// verifyTrieRoot validates that trie.Store produces the same state root as the
// ethStateDB path. Genesis and tx writes are combined into one block at blockNum
// to mirror the single ethStateDB.Commit call (preserving EIP-158 semantics).
func verifyTrieRoot(t *testing.T, genesisRWS, txRWS blocks.ReadWriteSet, blockNum uint64, expectedRoot common.Hash) {
	t.Helper()

	txns := make([]blocks.Transaction, 0, 2)
	if len(genesisRWS.Writes) > 0 {
		txns = append(txns, blocks.Transaction{
			Valid: true,
			NsRWS: []blocks.NsReadWriteSet{{Namespace: "basic", RWS: genesisRWS}},
		})
	}
	txns = append(txns, blocks.Transaction{
		Valid: true,
		NsRWS: []blocks.NsReadWriteSet{{Namespace: "basic", RWS: txRWS}},
	})

	ts, err := trie.New("", types.EmptyRootHash)
	if err != nil {
		t.Fatalf("create trie store: %v", err)
	}
	defer ts.Close()

	trieRoot, err := ts.Commit(t.Context(), blocks.Block{Number: blockNum, Transactions: txns})
	if err != nil {
		t.Fatalf("trie commit: %v", err)
	}

	if trieRoot != expectedRoot {
		t.Fatalf("trie root mismatch: got %s, want %s", trieRoot.Hex(), expectedRoot.Hex())
	}
	t.Logf("trie root verified: %s", trieRoot.Hex())
}

// ethereumTestHarness wraps TestHarness with endorser wrappers for ethereum state tracking
type ethereumTestHarness struct {
	*TestHarness
}

// wrappedEndorserFactory creates endorsers wrapped with testimpl wrappers for ethStateDB tracking.
// blockCtx injects the test-specific EVM block context (fork rules, coinbase, difficulty, etc.).
func wrappedEndorserFactory(blockCtx *vm.BlockContext) EndorserFactory {
	return func(t *testing.T, ecfg econf.Endorser, channel, namespace string, evmConfig execution.EVMConfig, protocol string) EndorserComponents {
		db, builder, end := NewEndorser(t, ecfg, channel, namespace, evmConfig, protocol)

		engine := execution.NewEVMEngine(namespace, db, evmConfig, protocol == "fabric-x")
		engineWrapper := testimpl.NewEVMEngineWrapper(namespace, db, evmConfig, protocol == "fabric-x", engine)
		engineWrapper.SetBlockContext(blockCtx)

		wrapper := testimpl.NewEndorserWrapper(end, engineWrapper)
		return EndorserComponents{KVS: db, Builder: builder, Service: wrapper}
	}
}

// newEthereumTestHarness creates a test harness with pre-state primed from ethereum test format.
// blockCtx provides the test-specific EVM block context injected into the engine wrapper.
func newEthereumTestHarness(t *testing.T, evmConfig execution.EVMConfig, pre types.GenesisAlloc, blockCtx *vm.BlockContext) (*ethereumTestHarness, error) {
	t.Helper()

	th, err := NewLocalTestHarnessWithFactory(t, TestLogger{T: t}, evmConfig, "", "bypass", nil, wrappedEndorserFactory(blockCtx))
	if err != nil {
		return nil, err
	}

	eth := &ethereumTestHarness{th}

	// Prime the state using our custom method that works with wrappers
	if err := eth.primeGenesisAlloc(t.Context(), pre, false); err != nil {
		th.Stop()
		return nil, err
	}

	return eth, nil
}

// primeGenesisAlloc primes the state from ethereum genesis format and sets up ethStateDB for wrappers
func (eth *ethereumTestHarness) primeGenesisAlloc(ctx context.Context, pre types.GenesisAlloc, wait bool) error {
	if len(pre) == 0 {
		return nil
	}

	primer, err := eth.NewStatePrimer()
	if err != nil {
		return err
	}

	// Sort addresses to ensure deterministic account creation order
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
		n := account.Nonce
		nonce := &n

		var balance *big.Int
		if account.Balance != nil {
			balance = account.Balance
		}

		var code []byte
		if len(account.Code) > 0 {
			code = account.Code
		}

		var storage map[common.Hash]common.Hash
		if len(account.Storage) > 0 {
			storage = account.Storage
		}

		primer.SetAccount(addr, nonce, code, balance, storage)
	}

	// Extract the ethStateDB before committing
	ethStateDB := primer.GetEthStateDB()

	// Commit the ethStateDB to finalize the primed state
	root, err := ethStateDB.Commit(0, false, false)
	if err != nil {
		return fmt.Errorf("failed to commit primed ethStateDB: %w", err)
	}

	// Create a new ethStateDB from the committed root
	stateDB := ethStateDB.Database()
	ethStateDB, err = state.New(root, stateDB)
	if err != nil {
		return fmt.Errorf("failed to create new ethStateDB from committed root: %w", err)
	}

	// Commit all state changes to the Fabric ledger
	if err := primer.Commit(ctx, wait); err != nil {
		return err
	}

	// Set the ethStateDB on all wrappers
	for _, end := range eth.endorsers {
		end.(*testimpl.EndorserWrapper).SetEthStateDB(ethStateDB)
	}

	return nil
}
