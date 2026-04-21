/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/hyperledger/fabric-protos-go-apiv2/ledger/rwset"
	"github.com/hyperledger/fabric-protos-go-apiv2/ledger/rwset/kvrwset"
	"github.com/hyperledger/fabric-x-common/protoutil"
	"github.com/hyperledger/fabric-x-evm/endorser"
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

// want_legacy is set when we want to run the legacy tests, which we typically skip
var want_legacy = flag.Bool("legacy", false, "Run the legacy tests that are otherwise blacklisted")

// loadSlow loads the test cases that are typically skipped because they are slow
func loadSlow(path string) (map[string]struct{}, error) {
	slow := make(map[string]struct{})

	file, err := os.Open(path)
	if err != nil {
		// If file doesn't exist, return empty map
		if os.IsNotExist(err) {
			return slow, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			slow[line] = struct{}{}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return slow, nil
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

// TestEthereumTests runs official ethereum/tests from the git submodule
//
// The ethereum/tests repository is included as a git submodule at testdata/ethereum-tests/
// To initialize: git submodule update --init --recursive
//
// This follows the same approach as Besu, Geth, and other Ethereum clients.
func TestEthereumTests(t *testing.T) {
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr)) // disable grpc logging

	// Load slow
	slow, err := loadSlow(filepath.Join("..", "testdata", "eth_tests.slow"))
	if err != nil {
		t.Fatalf("Failed to load blacklist: %v", err)
	}
	t.Logf("Loaded blacklist with %d entries", len(slow))

	// Find all JSON files recursively

	// 1) GeneralStateTests
	testsDir := filepath.Join("..", "testdata", "ethereum-tests", "GeneralStateTests")
	allFiles, err := findJSONFiles(testsDir)
	if err != nil {
		t.Fatalf("Failed to find test files: %v", err)
	}
	t.Logf("Found %d total test files", len(allFiles))

	if *want_legacy {
		// 2) LegacyTests
		testsDir = filepath.Join("..", "testdata", "ethereum-tests", "LegacyTests", "Constantinople", "GeneralStateTests")
		allFiles, err = findJSONFiles(testsDir)
		if err != nil {
			t.Fatalf("Failed to find test files: %v", err)
		}
		t.Logf("Found %d total test files", len(allFiles))
	}

	// Filter out slow files unless we explicitly want them
	testFiles := filterSlowTests(allFiles, slow, *want_very_slow)
	t.Logf("Running %d test files after filtering", len(testFiles))

	// testFiles = []string{
	// 	"../testdata/ethereum-tests/GeneralStateTests/stEIP1559/lowGasLimit.json",
	// }

	for _, testPath := range testFiles {
		t.Run(filepath.Base(testPath), func(t *testing.T) {
			runEthereumTestFile(t, testPath)
		})
	}
}

// runEthereumTestFile parses a test file and runs all tests within it
func runEthereumTestFile(t *testing.T, path string) {
	tests, err := ParseTestFile(path)
	if err != nil {
		t.Fatalf("Failed to parse test file: %v", err)
	}

	// Run each StateTest with all configurations
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel() // Run test files in parallel
			runSingleEthereumTest(t, test)
		})
	}
}

// runSingleEthereumTest executes one ethereum test case with all configurations
func runSingleEthereumTest(t *testing.T, stateTest *StateTest) {
	// Iterate through all subtests (fork/index combinations)
	for _, subtest := range stateTest.Subtests() {
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

// runEthereumTestConfig executes a specific test configuration
func runEthereumTestConfig(t *testing.T, stateTest *StateTest, subtest StateSubtest, snapshotter bool, scheme string) {
	// Build block info from test environment
	blockInfo, err := buildBlockInfo(&stateTest.json.Env)
	if err != nil {
		t.Fatalf("Failed to build block info: %v", err)
	}

	// Get the post-state to extract the correct indices and expected results
	post := stateTest.json.Post[subtest.Fork][subtest.Index]
	dataIndex := post.Indexes.Data
	gasIndex := post.Indexes.Gas
	valueIndex := post.Indexes.Value

	// Call prepareTestEnvironment to get context, config, block, and msg.
	// The returned StateTestState holds a TrieDB and optional Snapshots that must
	// be closed to stop the background snapshot-generator goroutine.
	vmConfig := vm.Config{} // Empty VM config for now
	st, config, block, _, context, prepareErr := stateTest.prepareTestEnvironment(subtest.Fork, subtest.Index, vmConfig, snapshotter, scheme)

	// Check if the error from prepareTestEnvironment is expected (e.g., blob count exceeded)
	// This matches the behavior of TestSingleAdd11 which uses checkError
	// We must check this BEFORE trying to access block.Number() or block.Time()
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
	evmConfig := &endorser.EVMConfig{
		BlockContext: &context,
		ChainConfig:  config,
		VMConfig:     &vmConfig,
	}

	// Create test harness with local backend and state priming, passing evmConfig
	th, err := newEthereumTestHarness(t, evmConfig, stateTest.json.Pre)
	if err != nil {
		t.Fatalf("Failed to create test harness: %v", err)
	}
	defer th.Stop()

	// Execute transaction through gateway
	env, execErr := th.gateways[0].ExecuteEthTx(t.Context(), tx, blockInfo)

	// Get expected root from post-state
	expectedRoot := common.Hash(post.Root)

	var actualRoot common.Hash
	// After execution, extract the ethStateDB and commit the ethereum state
	if len(th.endorsers) > 0 {
		ethStateDB := th.endorsers[0].GetEthStateDB()
		if ethStateDB != nil {
			// Commit the ethereum state
			root, err := ethStateDB.Commit(blockInfo.BlockNumber.Uint64(),
				config.IsEIP158(blockInfo.BlockNumber),
				config.IsCancun(blockInfo.BlockNumber, blockInfo.BlockTime))
			if err != nil {
				t.Fatalf("Failed to commit ethereum state: %v", err)
			}

			actualRoot = root
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
		verifyTrieRoot(t, th.primer.Writes(), txRWS, blockInfo.BlockNumber.Uint64(), expectedRoot)
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

// newEthereumTestHarness creates a test harness with pre-state primed from ethereum test format
func newEthereumTestHarness(t *testing.T, evmConfig *endorser.EVMConfig, pre types.GenesisAlloc) (*TestHarness, error) {
	t.Helper()

	th, err := newLocalTestHarness(t, TestLogger{T: t}, evmConfig, "", "bypass", nil)
	if err != nil {
		return nil, err
	}

	if err := th.PrimeGenesisAlloc(t.Context(), pre, false); err != nil {
		th.Stop()
		return nil, err
	}

	return th, nil
}
