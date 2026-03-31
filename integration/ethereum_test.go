/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/hyperledger/fabric-x-evm/endorser"
)

// loadBlacklist loads the blacklist file and returns a map of blacklisted file paths
func loadBlacklist(path string) (map[string]struct{}, error) {
	blacklist := make(map[string]struct{})

	file, err := os.Open(path)
	if err != nil {
		// If blacklist doesn't exist, return empty map
		if os.IsNotExist(err) {
			return blacklist, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			blacklist[line] = struct{}{}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return blacklist, nil
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

// filterBlacklistedFiles removes blacklisted files from the list
func filterBlacklistedFiles(files []string, blacklist map[string]struct{}) []string {
	var filtered []string

	for _, file := range files {
		// Check if the file is in the blacklist
		if _, isBlacklisted := blacklist[file]; !isBlacklisted {
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
	// Load blacklist
	blacklist, err := loadBlacklist(filepath.Join("..", "testdata", "eth_tests.blacklist"))
	if err != nil {
		t.Fatalf("Failed to load blacklist: %v", err)
	}
	t.Logf("Loaded blacklist with %d entries", len(blacklist))

	// Find all JSON files recursively
	testsDir := filepath.Join("..", "testdata", "ethereum-tests", "LegacyTests", "Constantinople", "GeneralStateTests")
	allFiles, err := findJSONFiles(testsDir)
	if err != nil {
		t.Fatalf("Failed to find test files: %v", err)
	}
	t.Logf("Found %d total test files", len(allFiles))

	// Filter out blacklisted files
	testFiles := filterBlacklistedFiles(allFiles, blacklist)
	t.Logf("Running %d test files after filtering blacklist", len(testFiles))

	for _, testPath := range testFiles {
		file, err := GetTestPath(testPath)
		if err != nil {
			t.Logf("Skipping %s: %v", testPath, err)
			continue
		}

		t.Run(filepath.Base(file), func(t *testing.T) {
			runEthereumTestFile(t, file)
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
			runSingleEthereumTest(t, test)
		})
	}
}

// runSingleEthereumTest executes one ethereum test case with all configurations
func runSingleEthereumTest(t *testing.T, stateTest *StateTest) {
	// Iterate through all subtests (fork/index combinations)
	for _, subtest := range stateTest.Subtests() {
		key := fmt.Sprintf("%s/%d", subtest.Fork, subtest.Index)

		if testing.Short() && rand.Intn(2) == 0 {
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

	// Build and sign transaction using the indices from post-state
	tx, err := buildTransaction(&stateTest.json.Tx, dataIndex, gasIndex, valueIndex)
	if err != nil {
		t.Fatalf("Failed to build transaction: %v", err)
	}

	// Call prepareTestEnvironment to get context, config, block, and msg
	vmConfig := vm.Config{} // Empty VM config for now
	_, config, block, msg, context, err := stateTest.prepareTestEnvironment(subtest.Fork, subtest.Index, vmConfig, snapshotter, scheme)
	if err != nil {
		t.Fatalf("Failed to prepare test environment: %v", err)
	}

	// Create EVMConfig to pass to test harness
	evmConfig := &endorser.EVMConfig{
		BlockContext: &context,
		ChainConfig:  config,
		VMConfig:     &vmConfig,
	}

	t.Logf("Creating test harness with EVM config: fork=%s, block=%d, msg.From=%s, snapshotter=%v, scheme=%s",
		subtest.Fork, block.NumberU64(), msg.From.Hex(), snapshotter, scheme)

	// Create test harness with local backend and state priming, passing evmConfig
	th, err := newEthereumTestHarness(t, evmConfig, stateTest.json.Pre)
	if err != nil {
		t.Fatalf("Failed to create test harness: %v", err)
	}
	defer th.Stop()

	// Execute transaction through gateway
	_, execErr := th.gateways[0].ExecuteEthTx(t.Context(), tx, blockInfo)

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
				t.Logf("Failed to commit ethereum state: %v", err)
			} else {
				t.Logf("Committed ethereum state with root: %s", root.Hex())
			}

			actualRoot = root
		}
	}

	// Check for expected errors
	if post.ExpectException != "" {
		if execErr == nil {
			t.Fatalf("expected error %q, got no error", post.ExpectException)
		}
		t.Logf("Got expected error: %v", execErr)
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

	t.Logf("Test completed successfully")
}

// newEthereumTestHarness creates a test harness with pre-state primed from ethereum test format
func newEthereumTestHarness(t *testing.T, evmConfig *endorser.EVMConfig, pre types.GenesisAlloc) (*TestHarness, error) {
	t.Helper()

	th, err := newLocalTestHarness(t, TestLogger{T: t}, evmConfig, "", "fabric")
	if err != nil {
		return nil, err
	}

	if err := th.PrimeGenesisAlloc(t.Context(), pre); err != nil {
		th.Stop()
		return nil, err
	}

	return th, nil
}
