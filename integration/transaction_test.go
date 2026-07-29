/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/tests"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	"google.golang.org/grpc/grpclog"
)

// transactionTestFile is the execution-specs transaction_test fixture shape:
//
//	{ "<name>": { "txbytes": "0x..", "result": { "<Fork>": { "exception": "..", "intrinsicGas": ".." } } } }
//
// A non-empty "exception" for a fork means the raw transaction must be rejected
// under that fork's rules; its absence means the transaction is valid.
type transactionTestFile map[string]struct {
	TxBytes hexutil.Bytes `json:"txbytes"`
	Result  map[string]struct {
		Exception string `json:"exception"`
	} `json:"result"`
}

// txNonceReader is a stub nonce source for the admission check: transaction_tests
// carry no pre-state, and every fixture that reaches the nonce check is rejected
// earlier on tx-type grounds, so a constant zero nonce is sufficient.
type txNonceReader struct{}

func (txNonceReader) NonceAt(context.Context, common.Address, *big.Int) (uint64, error) {
	return 0, nil
}

// TestTransactionTests runs the execution-specs transaction_tests through the gateway's
// admission path (UnmarshalBinary + core.ValidateTx), limited to executionSpecForks.
//
// These fixtures are all negative cases: each ships a raw transaction that must be
// rejected, so we assert on the reject/accept verdict rather than the exact reason
// string (the fixture names a fine-grained EEST exception; our gateway may reject at
// a coarser level, e.g. an unsupported tx type). This mirrors go-ethereum's own
// transaction_test.go, which also checks validity rather than the exception text.
func TestTransactionTests(t *testing.T) {
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr)) // disable grpc logging

	testsDir := filepath.Join("..", "testdata", "execution-specs-tests", "fixtures", "transaction_tests")
	if _, err := os.Stat(testsDir); os.IsNotExist(err) {
		t.Skipf("execution-specs fixtures not found at %s; run `make fetch-execution-specs-tests`", testsDir)
	}

	skip, err := loadSkip(filepath.Join("..", "testdata", "transaction_tests.skip"))
	if err != nil {
		t.Fatalf("Failed to load skip list: %v", err)
	}
	t.Logf("Loaded skip list with %d entries", len(skip))

	allFiles, err := findJSONFiles(testsDir)
	if err != nil {
		t.Fatalf("Failed to find test files: %v", err)
	}
	testFiles := filterSkippedTests(allFiles, skip)
	t.Logf("Running %d transaction_tests files after filtering", len(testFiles))

	for _, testPath := range testFiles {
		t.Run(filepath.Base(testPath), func(t *testing.T) {
			runTransactionTestFile(t, testPath)
		})
	}
}

// runTransactionTestFile runs one fixtures file's transaction tests against the fork allowlist.
func runTransactionTestFile(t *testing.T, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}
	var tf transactionTestFile
	if err := json.Unmarshal(data, &tf); err != nil {
		t.Fatalf("Failed to parse test file: %v", err)
	}

	for name, tc := range tf {
		for fork, res := range tc.Result {
			// EEST records one verdict per fork; only assert the forks we support.
			if _, ok := executionSpecForks[fork]; !ok {
				continue
			}
			expectRejected := res.Exception != ""
			t.Run(fmt.Sprintf("%s/%s", name, fork), func(t *testing.T) {
				t.Parallel()
				rejected := admissionRejects(t, tc.TxBytes, fork)
				if rejected != expectRejected {
					t.Fatalf("fork %s: expected rejected=%v, got rejected=%v (fixture exception=%q)",
						fork, expectRejected, rejected, res.Exception)
				}
			})
		}
	}
}

// admissionRejects mirrors the gateway's raw-transaction admission path
// (gateway/api.SendRawTransaction -> UnmarshalBinary, then core.ValidateTx) and
// reports whether the transaction is rejected at either step.
func admissionRejects(t *testing.T, raw []byte, fork string) bool {
	var tx types.Transaction
	if err := tx.UnmarshalBinary(raw); err != nil {
		return true // malformed bytes are rejected before validation
	}

	config, _, err := tests.GetChainConfig(fork)
	if err != nil {
		t.Skipf("unsupported fork %s: %v", fork, err)
	}

	signer := types.LatestSignerForChainID(config.ChainID)
	return core.ValidateTx(t.Context(), &tx, config, signer, txNonceReader{}) != nil
}
