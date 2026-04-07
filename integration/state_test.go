/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/vm"
)

// TestSingleAdd11 runs only the add11.json test from LegacyTests/Constantinople/GeneralStateTests/stExample
// This test is ported from go-ethereum's tests/state_test.go
func TestSingleAdd11(t *testing.T) {
	// Adjust the path to point to the ethereum-tests directory in this repo
	// Original path in go-ethereum: ./testdata/LegacyTests/Constantinople/GeneralStateTests/stExample/add11.json
	// Path in this repo: ../testdata/ethereum-tests/LegacyTests/Constantinople/GeneralStateTests/stExample/add11.json
	testPath := filepath.Join("..", "testdata", "ethereum-tests", "GeneralStateTests", "stEIP1559", "lowGasLimit.json")

	// Load the test file
	file, err := os.Open(testPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	m := make(map[string]StateTest)
	if err := json.NewDecoder(file).Decode(&m); err != nil {
		t.Fatal(err)
	}

	// Run the test
	for key, test := range m {
		t.Run(key, func(t *testing.T) {
			execStateTest(t, &test)
		})
	}
}

// execStateTest executes a state test similar to go-ethereum's implementation
func execStateTest(t *testing.T, test *StateTest) {
	for _, subtest := range test.Subtests() {
		key := fmt.Sprintf("%s/%d", subtest.Fork, subtest.Index)

		// If -short flag is used, we don't execute all four permutations, only one.
		executionMask := 0xf
		if testing.Short() {
			executionMask = (1 << (rand.Int63() & 4))
		}

		t.Run(key+"/hash/trie", func(t *testing.T) {
			if executionMask&0x1 == 0 {
				t.Skip("test (randomly) skipped due to short-tag")
			}
			withTrace(t, test, subtest, func(vmconfig vm.Config) error {
				return test.Run(subtest, vmconfig, false, rawdb.HashScheme)
			})
		})

		t.Run(key+"/hash/snap", func(t *testing.T) {
			if executionMask&0x2 == 0 {
				t.Skip("test (randomly) skipped due to short-tag")
			}
			withTrace(t, test, subtest, func(vmconfig vm.Config) error {
				return test.Run(subtest, vmconfig, true, rawdb.HashScheme)
			})
		})

		t.Run(key+"/path/trie", func(t *testing.T) {
			if executionMask&0x4 == 0 {
				t.Skip("test (randomly) skipped due to short-tag")
			}
			withTrace(t, test, subtest, func(vmconfig vm.Config) error {
				return test.Run(subtest, vmconfig, false, rawdb.PathScheme)
			})
		})

		t.Run(key+"/path/snap", func(t *testing.T) {
			if executionMask&0x8 == 0 {
				t.Skip("test (randomly) skipped due to short-tag")
			}
			withTrace(t, test, subtest, func(vmconfig vm.Config) error {
				return test.Run(subtest, vmconfig, true, rawdb.PathScheme)
			})
		})
	}
}

// withTrace runs the test with optional tracing on failure
func withTrace(t *testing.T, test *StateTest, subtest StateSubtest, testFunc func(vm.Config) error) {
	// Use config from command line arguments.
	config := vm.Config{}
	err := testFunc(config)
	if err == nil {
		return
	}

	// Test failed
	t.Error(err)
}
