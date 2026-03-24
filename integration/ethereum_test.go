/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"context"
	"fmt"
	"math/big"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
)

// TestEthereumTests runs official ethereum/tests from the git submodule
//
// The ethereum/tests repository is included as a git submodule at testdata/ethereum-tests/
// To initialize: git submodule update --init --recursive
//
// This follows the same approach as Besu, Geth, and other Ethereum clients.
func TestEthereumTests(t *testing.T) {
	testFiles := []string{
		// Tests from the ethereum-tests submodule
		"../testdata/ethereum-tests/LegacyTests/Constantinople/GeneralStateTests/stExample/add11.json",
		"../testdata/ethereum-tests/LegacyTests/Constantinople/GeneralStateTests/stArgsZeroOneBalance/addNonConst.json",
		"../testdata/ethereum-tests/LegacyTests/Constantinople/GeneralStateTests/stArgsZeroOneBalance/addmodNonConst.json",

		// Add more tests as needed
	}

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

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			runSingleEthereumTest(t, test)
		})
	}
}

// runSingleEthereumTest executes one ethereum test case
func runSingleEthereumTest(t *testing.T, test EthereumTest) {
	// Use AllEthashProtocolChanges for now (supports most opcodes)
	chainConfig := params.AllEthashProtocolChanges

	// Create test harness with local backend and state priming
	th, err := newEthereumTestHarness(t, chainConfig, test.Pre)
	if err != nil {
		t.Fatalf("Failed to create test harness: %v", err)
	}
	defer th.Stop()

	// Build block info from test environment
	blockInfo, err := buildBlockInfo(test.Env)
	if err != nil {
		t.Fatalf("Failed to build block info: %v", err)
	}

	// Execute each transaction variant (tests can have multiple data/gas/value combinations)
	for dataIdx := range test.Transaction.Data {
		t.Logf("Executing transaction variant %d/%d", dataIdx+1, len(test.Transaction.Data))

		// Build and sign transaction
		tx, err := buildTransaction(test.Transaction, dataIdx)
		if err != nil {
			t.Fatalf("Failed to build transaction: %v", err)
		}

		// Execute transaction through gateway
		_, execErr := th.gateways[0].ExecuteEthTx(t.Context(), tx, blockInfo)

		// Log execution result
		if execErr != nil {
			t.Logf("Transaction execution error: %v", execErr)
			// TODO: Check if error was expected based on test.Post
		} else {
			t.Logf("Transaction executed successfully")
			// TODO: Validate state against test.Post
		}
	}

	t.Logf("Test completed successfully")
}

// newEthereumTestHarness creates a test harness with pre-state primed from ethereum test format
func newEthereumTestHarness(t *testing.T, chainConfig *params.ChainConfig, pre map[string]TestAccount) (*TestHarness, error) {
	t.Helper()

	// Create a temporary test harness to get access to the database
	th, err := newLocalTestHarness(t.Context(), TestLogger{T: t}, chainConfig, "")
	if err != nil {
		return nil, err
	}

	// Prime the state using the existing infrastructure
	if len(pre) > 0 {
		t.Logf("Priming state with %d accounts", len(pre))

		// We need to access the database and prime it
		// The test harness has endorsers which have access to the txSim (VersionedDB)
		// We'll use a similar approach to PrimeStateFromJSON but with our test format

		if err := primeEthereumTestState(t.Context(), th, pre); err != nil {
			th.Stop()
			return nil, err
		}
	}

	return th, nil
}

// primeEthereumTestState primes the state database with ethereum test pre-state
func primeEthereumTestState(ctx context.Context, th *TestHarness, pre map[string]TestAccount) error {
	if len(pre) == 0 {
		return nil
	}

	// Create a StatePrimer to handle state initialization
	primer, err := th.NewStatePrimer()
	if err != nil {
		return err
	}

	// Convert each test account to StatePrimer operations
	for addrStr, account := range pre {
		addr := common.HexToAddress(addrStr)

		// Parse and set nonce
		var nonce *uint64
		if account.Nonce != "" {
			n, err := hexToUint64(account.Nonce)
			if err != nil {
				return fmt.Errorf("invalid nonce for %s: %w", addrStr, err)
			}
			nonce = &n
		}

		// Parse and set balance
		var balance *big.Int
		if account.Balance != "" {
			bal, err := hexToBigInt(account.Balance)
			if err != nil {
				return fmt.Errorf("invalid balance for %s: %w", addrStr, err)
			}
			balance = bal
		}

		// Parse and set code
		var code []byte
		if account.Code != "" && account.Code != "0x" {
			c, err := hexToBytes(account.Code)
			if err != nil {
				return fmt.Errorf("invalid code for %s: %w", addrStr, err)
			}
			code = c
		}

		// Parse and set storage
		var storage map[common.Hash]common.Hash
		if len(account.Storage) > 0 {
			storage = make(map[common.Hash]common.Hash)
			for keyStr, valStr := range account.Storage {
				key := common.HexToHash(keyStr)
				val := common.HexToHash(valStr)
				storage[key] = val
			}
		}

		// Apply all account properties
		primer.SetAccount(addr, nonce, code, balance, storage)
	}

	// Commit all state changes to the ledger
	return primer.Commit(ctx)
}
