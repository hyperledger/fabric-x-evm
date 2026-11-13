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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/hyperledger/fabric-protos-go-apiv2/ledger/rwset"
	"github.com/hyperledger/fabric-protos-go-apiv2/ledger/rwset/kvrwset"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/protoutil"
	fc "github.com/hyperledger/fabric-x-evm/common"
	econf "github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/gateway/storage/trie"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/protobuf/proto"
)

// want_very_slow is set when we want to run the tests that we typically skip because they are too slow
var want_very_slow = flag.Bool("very_slow", false, "Run the very slow tests that are otherwise blacklisted")

// loadSlow loads the test cases that are typically skipped because they are slow
func loadSlow(path string) (map[string]struct{}, error) {
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

	// The suite has known failures, which only the baseline check knows to expect
	// (make eth-tests), so a plain short run would fail on them.
	if testing.Short() {
		t.Skip("skipping in short mode; run make eth-tests")
	}

	testsDir := filepath.Join("..", "testdata", "execution-specs-tests", "fixtures", "state_tests")
	if _, err := os.Stat(testsDir); os.IsNotExist(err) {
		t.Skipf("execution-specs fixtures not found at %s; run `make fetch-execution-specs-tests`", testsDir)
	}

	slow, err := loadSlow(filepath.Join("..", "testdata", "eth_tests.slow"))
	if err != nil {
		t.Fatalf("Failed to load slow list: %v", err)
	}
	t.Logf("Loaded slow list with %d entries", len(slow))

	allFiles, err := findJSONFiles(testsDir)
	if err != nil {
		t.Fatalf("Failed to find test files: %v", err)
	}
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

		t.Run(key, func(t *testing.T) {
			runEthereumTestConfig(t, stateTest, subtest)
		})
	}
}

// runEthereumTestConfig executes a specific test configuration
func runEthereumTestConfig(t *testing.T, stateTest *StateTest, subtest StateSubtest) {
	// Get the post-state to extract the correct indices and expected results
	post := stateTest.json.Post[subtest.Fork][subtest.Index]
	dataIndex := post.Indexes.Data
	gasIndex := post.Indexes.Gas
	valueIndex := post.Indexes.Value

	config, block, context, prepareErr := stateTest.prepareTestEnvironment(subtest.Fork, subtest.Index)

	// Check if the error from prepareTestEnvironment is expected (e.g., blob count exceeded).
	// We must check this BEFORE trying to access block.Number() or block.Time().
	if prepareErr != nil {
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
		t.Fatalf("Failed to build transaction: %v", err)
	}

	// Create EVMConfig to pass to test harness
	evmConfig := execution.EVMConfig{
		ChainConfig: config,
	}

	// Create test harness with local backend and state priming, passing evmConfig and block context
	th, err := newEthereumTestHarness(t, evmConfig, stateTest.json.Pre, &context)
	if err != nil {
		t.Fatalf("Failed to create test harness: %v", err)
	}
	defer th.Stop()

	// run the tx through the pre-execution validation steps
	preExecErr := core.ValidateTx(tx, config, signerForTx(tx))

	// Execute transaction through gateway
	env, execErr := th.Gateways[0].ExecuteEthTx(t.Context(), tx)

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

	// What Fabric commits is the endorsement: its write set replayed onto the
	// primed pre-state must reach the fixture's root, and its logs must hash the same.
	txRWS, logs, err := endorsementResult(env)
	if err != nil {
		t.Fatalf("read endorsement: %v", err)
	}
	verifyWriteSetRoot(t, th.Primer.Writes(), txRWS, block.Number().Uint64(), common.Hash(post.Root), post.State)
	if got := rlpHash(logs); got != common.Hash(post.Logs) {
		t.Fatalf("logs hash mismatch: got %s, want %s (%d logs)", got.Hex(), common.Hash(post.Logs).Hex(), len(logs))
	}
}

// endorsementResult extracts the write set and the logs from the first
// ProposalResponse in an sdk.Endorsement. It reverses the encoding done by
// endorsement/fabric.EndorsementBuilder.
func endorsementResult(env sdk.Endorsement) (blocks.ReadWriteSet, []*types.Log, error) {
	if len(env.Responses) == 0 {
		return blocks.ReadWriteSet{}, nil, nil
	}

	prp, err := protoutil.UnmarshalProposalResponsePayload(env.Responses[0].Payload)
	if err != nil {
		return blocks.ReadWriteSet{}, nil, fmt.Errorf("unmarshal proposal response payload: %w", err)
	}

	ca, err := protoutil.UnmarshalChaincodeAction(prp.Extension)
	if err != nil {
		return blocks.ReadWriteSet{}, nil, fmt.Errorf("unmarshal chaincode action: %w", err)
	}

	txrws := &rwset.TxReadWriteSet{}
	if err := proto.Unmarshal(ca.Results, txrws); err != nil {
		return blocks.ReadWriteSet{}, nil, fmt.Errorf("unmarshal tx rws: %w", err)
	}

	var rws blocks.ReadWriteSet
	for _, ns := range txrws.NsRwset {
		kvRws := &kvrwset.KVRWSet{}
		if err := proto.Unmarshal(ns.Rwset, kvRws); err != nil {
			return blocks.ReadWriteSet{}, nil, fmt.Errorf("unmarshal kv rws for ns %s: %w", ns.Namespace, err)
		}
		for _, w := range kvRws.Writes {
			rws.Writes = append(rws.Writes, blocks.KVWrite{
				Key:      w.Key,
				IsDelete: w.IsDelete,
				Value:    w.Value,
			})
		}
	}

	var logs []*types.Log
	if len(ca.Events) > 0 {
		event := &peer.ChaincodeEvent{}
		if err := proto.Unmarshal(ca.Events, event); err != nil {
			return blocks.ReadWriteSet{}, nil, fmt.Errorf("unmarshal chaincode event: %w", err)
		}
		if len(event.Payload) > 0 {
			raw, err := fc.UnmarshalLogs(event.Payload)
			if err != nil {
				return blocks.ReadWriteSet{}, nil, fmt.Errorf("unmarshal logs: %w", err)
			}
			for _, l := range raw {
				topics := make([]common.Hash, len(l.Topics))
				for i, topic := range l.Topics {
					topics[i] = common.BytesToHash(topic)
				}
				logs = append(logs, &types.Log{Address: common.BytesToAddress(l.Address), Topics: topics, Data: l.Data})
			}
		}
	}

	return rws, logs, nil
}

// verifyWriteSetRoot replays the primed pre-state as genesis and the
// transaction's write set as the next block, and fails the test with a
// per-account diff against the fixture's post-state unless the root is want.
func verifyWriteSetRoot(t *testing.T, genesisRWS, txRWS blocks.ReadWriteSet, blockNum uint64, want common.Hash, post types.GenesisAlloc) {
	t.Helper()

	ts, err := trie.New("", types.EmptyRootHash)
	if err != nil {
		t.Fatalf("create trie store: %v", err)
	}
	defer ts.Close()

	if _, err := ts.CommitGenesis(genesisRWS.Writes); err != nil {
		t.Fatalf("trie commit pre-state: %v", err)
	}
	root, err := ts.Commit(t.Context(), blocks.Block{Number: blockNum, Transactions: []blocks.Transaction{{
		Status: blocks.StatusCommitted,
		NsRWS:  []blocks.NsReadWriteSet{{Namespace: "basic", RWS: txRWS}},
	}}})
	if err != nil {
		t.Fatalf("trie commit tx: %v", err)
	}
	if root == want {
		return
	}

	got, err := ts.State()
	if err != nil {
		t.Fatalf("write-set root mismatch: got %s, want %s (open state: %v)", root.Hex(), want.Hex(), err)
	}
	t.Fatalf("write-set root mismatch: got %s, want %s\n%s", root.Hex(), want.Hex(),
		diffState(got, post, append(genesisRWS.Writes, txRWS.Writes...)))
}

// maxDiffLines caps diffState's output; one wrong field usually explains the rest.
const maxDiffLines = 20

// diffState lists the accounts and storage slots where got differs from want.
// It checks every account in want, plus every account and slot the writes touched.
func diffState(got *state.StateDB, want types.GenesisAlloc, writes []blocks.KVWrite) string {
	slots := map[common.Address]map[common.Hash]struct{}{}
	for addr, acc := range want {
		slots[addr] = map[common.Hash]struct{}{}
		for slot := range acc.Storage {
			slots[addr][slot] = struct{}{}
		}
	}
	for _, w := range writes {
		key, ok := fc.ParseStateKey(w.Key)
		if !ok {
			continue
		}
		if slots[key.Address] == nil {
			slots[key.Address] = map[common.Hash]struct{}{}
		}
		if key.Storage {
			slots[key.Address][key.Slot] = struct{}{}
		}
	}
	addrs := make([]common.Address, 0, len(slots))
	for addr := range slots {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool { return bytes.Compare(addrs[i][:], addrs[j][:]) < 0 })

	var lines []string
	for _, addr := range addrs {
		acc, ok := want[addr]
		if !ok {
			if got.Exist(addr) {
				lines = append(lines, fmt.Sprintf("%s: account exists, want none", addr.Hex()))
			}
			continue
		}
		if !got.Exist(addr) {
			lines = append(lines, fmt.Sprintf("%s: account missing", addr.Hex()))
			continue
		}
		wantBal := new(big.Int)
		if acc.Balance != nil {
			wantBal = acc.Balance
		}
		if gotBal := got.GetBalance(addr).ToBig(); gotBal.Cmp(wantBal) != 0 {
			lines = append(lines, fmt.Sprintf("%s: balance %s, want %s", addr.Hex(), gotBal, wantBal))
		}
		if gotNonce := got.GetNonce(addr); gotNonce != acc.Nonce {
			lines = append(lines, fmt.Sprintf("%s: nonce %d, want %d", addr.Hex(), gotNonce, acc.Nonce))
		}
		if gotCode := got.GetCode(addr); !bytes.Equal(gotCode, acc.Code) {
			lines = append(lines, fmt.Sprintf("%s: code %d bytes, want %d", addr.Hex(), len(gotCode), len(acc.Code)))
		}
		for slot := range slots[addr] {
			if gotVal, wantVal := got.GetState(addr, slot), acc.Storage[slot]; gotVal != wantVal {
				lines = append(lines, fmt.Sprintf("%s: slot %s = %s, want %s", addr.Hex(), slot.Hex(), gotVal.Hex(), wantVal.Hex()))
			}
		}
	}
	if len(lines) > maxDiffLines {
		lines = append(lines[:maxDiffLines], fmt.Sprintf("... and %d more", len(lines)-maxDiffLines))
	}
	if len(lines) == 0 {
		return "no account or slot differs; the difference is outside what the fixture lists"
	}
	return strings.Join(lines, "\n")
}

// ethereumTestHarness is a TestHarness whose endorsers run a fixture's block context.
type ethereumTestHarness struct {
	*TestHarness
}

// fixtureEndorserFactory creates production endorsers whose engine runs every
// transaction in blockCtx, the fixture's block context (fork rules, coinbase,
// base fee, ...), and charges gas like Ethereum does instead of executing for free.
func fixtureEndorserFactory(blockCtx *vm.BlockContext) EndorserFactory {
	return func(t *testing.T, ecfg econf.Endorser, channel, namespace string, evmConfig execution.EVMConfig, protocol string) EndorserComponents {
		db, builder, end := NewEndorser(t, ecfg, channel, namespace, evmConfig, protocol)
		end.Engine.(*execution.EVMEngine).SetExecutorHook(func(ex *execution.Executor) {
			ex.BlockCtx = *blockCtx
			ex.FreeGas = false
		})
		return EndorserComponents{KVS: db, Builder: builder, Service: end}
	}
}

// newEthereumTestHarness creates a test harness with pre-state primed from ethereum test format.
// blockCtx is the fixture's block context the endorsers execute in.
func newEthereumTestHarness(t *testing.T, evmConfig execution.EVMConfig, pre types.GenesisAlloc, blockCtx *vm.BlockContext) (*ethereumTestHarness, error) {
	t.Helper()

	th, err := NewLocalTestHarnessWithFactory(t, TestLogger{T: t}, evmConfig, "", "bypass", nil, fixtureEndorserFactory(blockCtx))
	if err != nil {
		return nil, err
	}

	eth := &ethereumTestHarness{th}
	if err := eth.primeGenesisAlloc(t.Context(), pre); err != nil {
		th.Stop()
		return nil, err
	}
	return eth, nil
}

// primeGenesisAlloc commits the fixture's pre-state to the ledger.
func (eth *ethereumTestHarness) primeGenesisAlloc(ctx context.Context, pre types.GenesisAlloc) error {
	primer, err := eth.NewStatePrimer()
	if err != nil {
		return err
	}

	// Sort addresses to ensure deterministic account creation order
	addresses := make([]common.Address, 0, len(pre))
	for addr := range pre {
		addresses = append(addresses, addr)
	}
	sort.Slice(addresses, func(i, j int) bool {
		return bytes.Compare(addresses[i].Bytes(), addresses[j].Bytes()) < 0
	})

	for _, addr := range addresses {
		account := pre[addr]
		nonce := account.Nonce
		var code []byte
		if len(account.Code) > 0 {
			code = account.Code
		}
		var storage map[common.Hash]common.Hash
		if len(account.Storage) > 0 {
			storage = account.Storage
		}
		primer.SetAccount(addr, &nonce, code, account.Balance, storage)
	}

	return primer.Commit(ctx, false)
}
