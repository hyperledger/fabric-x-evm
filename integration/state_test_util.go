/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later

This file contains code adapted from go-ethereum (https://github.com/ethereum/go-ethereum)
which is licensed under the GNU Lesser General Public License v3.0.
*/

package integration

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/state/snapshot"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/tests"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/hashdb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
	"github.com/holiman/uint256"
	"github.com/hyperledger/fabric-x-evm/endorser"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	fabricstate "github.com/hyperledger/fabric-x-sdk/state"
	"golang.org/x/crypto/sha3"
)

// StateTest checks transaction processing without block context.
type StateTest struct {
	json stJSON
}

// StateSubtest selects a specific configuration of a General State Test.
type StateSubtest struct {
	Fork  string
	Index int
}

func (t *StateTest) UnmarshalJSON(in []byte) error {
	return json.Unmarshal(in, &t.json)
}

type stJSON struct {
	Env  stEnv                    `json:"env"`
	Pre  types.GenesisAlloc       `json:"pre"`
	Tx   stTransaction            `json:"transaction"`
	Out  hexutil.Bytes            `json:"out"`
	Post map[string][]stPostState `json:"post"`
}

type stPostState struct {
	Root            common.UnprefixedHash `json:"hash"`
	Logs            common.UnprefixedHash `json:"logs"`
	TxBytes         hexutil.Bytes         `json:"txbytes"`
	ExpectException string                `json:"expectException"`
	Indexes         struct {
		Data  int `json:"data"`
		Gas   int `json:"gas"`
		Value int `json:"value"`
	}
}

type stEnv struct {
	Coinbase      common.Address `json:"currentCoinbase"`
	Difficulty    *big.Int       `json:"currentDifficulty"`
	Random        *big.Int       `json:"currentRandom"`
	GasLimit      uint64         `json:"currentGasLimit"`
	Number        uint64         `json:"currentNumber"`
	Timestamp     uint64         `json:"currentTimestamp"`
	BaseFee       *big.Int       `json:"currentBaseFee"`
	ExcessBlobGas *uint64        `json:"currentExcessBlobGas"`
}

func (s *stEnv) UnmarshalJSON(input []byte) error {
	type stEnv struct {
		Coinbase      *common.UnprefixedAddress `json:"currentCoinbase"`
		Difficulty    *math.HexOrDecimal256     `json:"currentDifficulty"`
		Random        *math.HexOrDecimal256     `json:"currentRandom"`
		GasLimit      *math.HexOrDecimal64      `json:"currentGasLimit"`
		Number        *math.HexOrDecimal64      `json:"currentNumber"`
		Timestamp     *math.HexOrDecimal64      `json:"currentTimestamp"`
		BaseFee       *math.HexOrDecimal256     `json:"currentBaseFee"`
		ExcessBlobGas *math.HexOrDecimal64      `json:"currentExcessBlobGas"`
	}
	var dec stEnv
	if err := json.Unmarshal(input, &dec); err != nil {
		return err
	}
	if dec.Coinbase != nil {
		s.Coinbase = common.Address(*dec.Coinbase)
	}
	if dec.Difficulty != nil {
		s.Difficulty = (*big.Int)(dec.Difficulty)
	}
	if dec.Random != nil {
		s.Random = (*big.Int)(dec.Random)
	}
	if dec.GasLimit != nil {
		s.GasLimit = uint64(*dec.GasLimit)
	}
	if dec.Number != nil {
		s.Number = uint64(*dec.Number)
	}
	if dec.Timestamp != nil {
		s.Timestamp = uint64(*dec.Timestamp)
	}
	if dec.BaseFee != nil {
		s.BaseFee = (*big.Int)(dec.BaseFee)
	}
	if dec.ExcessBlobGas != nil {
		val := uint64(*dec.ExcessBlobGas)
		s.ExcessBlobGas = &val
	}
	return nil
}

type stTransaction struct {
	GasPrice             *big.Int            `json:"gasPrice"`
	MaxFeePerGas         *big.Int            `json:"maxFeePerGas"`
	MaxPriorityFeePerGas *big.Int            `json:"maxPriorityFeePerGas"`
	Nonce                uint64              `json:"nonce"`
	To                   string              `json:"to"`
	Data                 []string            `json:"data"`
	AccessLists          []*types.AccessList `json:"accessLists,omitempty"`
	GasLimit             []uint64            `json:"gasLimit"`
	Value                []string            `json:"value"`
	PrivateKey           []byte              `json:"secretKey"`
	Sender               *common.Address     `json:"sender"`
	BlobVersionedHashes  []common.Hash       `json:"blobVersionedHashes,omitempty"`
	BlobGasFeeCap        *big.Int            `json:"maxFeePerBlobGas,omitempty"`
	AuthorizationList    []*stAuthorization  `json:"authorizationList,omitempty"`
}

type stAuthorization struct {
	ChainID *big.Int       `json:"chainId"`
	Address common.Address `json:"address"`
	Nonce   uint64         `json:"nonce"`
	V       uint8          `json:"v"`
	R       *big.Int       `json:"r"`
	S       *big.Int       `json:"s"`
}

func (tx *stTransaction) UnmarshalJSON(data []byte) error {
	type stTransactionMarshaling struct {
		GasPrice             *math.HexOrDecimal256
		MaxFeePerGas         *math.HexOrDecimal256
		MaxPriorityFeePerGas *math.HexOrDecimal256
		Nonce                math.HexOrDecimal64
		To                   string
		Data                 []string
		AccessLists          []*types.AccessList
		GasLimit             []math.HexOrDecimal64
		Value                []string
		PrivateKey           hexutil.Bytes `json:"secretKey"`
		Sender               *common.Address
		BlobVersionedHashes  []common.Hash
		BlobGasFeeCap        *math.HexOrDecimal256
	}
	var dec stTransactionMarshaling
	if err := json.Unmarshal(data, &dec); err != nil {
		return err
	}
	if dec.GasPrice != nil {
		tx.GasPrice = (*big.Int)(dec.GasPrice)
	}
	if dec.MaxFeePerGas != nil {
		tx.MaxFeePerGas = (*big.Int)(dec.MaxFeePerGas)
	}
	if dec.MaxPriorityFeePerGas != nil {
		tx.MaxPriorityFeePerGas = (*big.Int)(dec.MaxPriorityFeePerGas)
	}
	tx.Nonce = uint64(dec.Nonce)
	tx.To = dec.To
	tx.Data = dec.Data
	tx.AccessLists = dec.AccessLists
	tx.GasLimit = make([]uint64, len(dec.GasLimit))
	for i, g := range dec.GasLimit {
		tx.GasLimit[i] = uint64(g)
	}
	tx.Value = dec.Value
	tx.PrivateKey = dec.PrivateKey
	tx.Sender = dec.Sender
	tx.BlobVersionedHashes = dec.BlobVersionedHashes
	if dec.BlobGasFeeCap != nil {
		tx.BlobGasFeeCap = (*big.Int)(dec.BlobGasFeeCap)
	}
	return nil
}

// Subtests returns all valid subtests of the test.
func (t *StateTest) Subtests() []StateSubtest {
	var sub []StateSubtest
	for fork, pss := range t.json.Post {
		for i := range pss {
			sub = append(sub, StateSubtest{fork, i})
		}
	}
	return sub
}

// checkError checks if the error returned by the state transition matches any expected error.
func (t *StateTest) checkError(subtest StateSubtest, err error) error {
	expectedError := t.json.Post[subtest.Fork][subtest.Index].ExpectException
	if err == nil && expectedError == "" {
		return nil
	}
	if err == nil && expectedError != "" {
		return fmt.Errorf("expected error %q, got no error", expectedError)
	}
	if err != nil && expectedError == "" {
		return fmt.Errorf("unexpected error: %w", err)
	}
	if err != nil && expectedError != "" {
		// Ignore expected errors
		return nil
	}
	return nil
}

// Run executes a specific subtest and verifies the post-state and logs
func (t *StateTest) Run(subtest StateSubtest, vmconfig vm.Config, snapshotter bool, scheme string) error {
	st, root, _, err := t.RunNoVerify(subtest, vmconfig, snapshotter, scheme)
	//lint:ignore SA5001 st is guaranteed non-nil
	defer st.Close()

	checkedErr := t.checkError(subtest, err)
	if checkedErr != nil {
		return checkedErr
	}
	// The error has been checked; if it was unexpected, it's already returned.
	if err != nil {
		// Here, an error exists but it was expected.
		// We do not check the post state or logs.
		return nil
	}
	post := t.json.Post[subtest.Fork][subtest.Index]
	// N.B: We need to do this in a two-step process, because the first Commit takes care
	// of self-destructs, and we need to touch the coinbase _after_ it has potentially self-destructed.
	if root != common.Hash(post.Root) {
		return fmt.Errorf("post state root mismatch: got %x, want %x", root, post.Root)
	}
	// Get logs - need to access the underlying ethStateDB for both types
	var logs common.Hash
	if dualDB, ok := st.StateDB.(*endorser.DualStateDB); ok {
		logs = rlpHash(dualDB.EthStateDB().Logs())
	} else if ethDB, ok := st.StateDB.(*state.StateDB); ok {
		logs = rlpHash(ethDB.Logs())
	} else if loggerDB, ok := st.StateDB.(*endorser.EthStateDBLogger); ok {
		logs = rlpHash(loggerDB.Logs())
	}
	if logs != common.Hash(post.Logs) {
		return fmt.Errorf("post state logs hash mismatch: got %x, want %x", logs, post.Logs)
	}
	return nil
}

// StateTestState groups all the state database objects together for use in tests.
// StateDB can be either *state.StateDB or *endorser.DualStateDB
type StateTestState struct {
	StateDB   vm.StateDB
	TrieDB    *triedb.Database
	Snapshots *snapshot.Tree
}

// Close should be called when the state is no longer needed, ie. after running the test.
func (st *StateTestState) Close() {
	if st.TrieDB != nil {
		st.TrieDB.Close()
		st.TrieDB = nil
	}
	if st.Snapshots != nil {
		st.Snapshots.Disable()
		st.Snapshots.Release()
		st.Snapshots = nil
	}
}

// prepareTestEnvironment prepares the test environment including config, state, message, and EVM context.
//
// Parameters:
//   - fork: The Ethereum fork name (e.g., "London", "Cancun") that determines which chain configuration
//     and consensus rules to use for the test. This is used to look up the appropriate ChainConfig.
//   - postStateIndex: The index into the test's post-state array, selecting which specific transaction
//     configuration to test (tests can have multiple post-states with different gas/data/value combinations).
//   - vmconfig: The VM configuration including tracer settings and extra EIPs to enable.
//   - snapshotter: Whether to enable state snapshots for the test.
//   - scheme: The state trie scheme to use ("hash" or "path").
//
// Returns:
//   - st: The initialized state test state containing the StateDB and associated resources.
//   - config: The chain configuration for the specified fork.
//   - block: The genesis block for the test.
//   - msg: The transaction message to execute, derived from the test transaction and post-state.
//   - context: The EVM block context with all necessary block-level parameters.
//   - err: Any error encountered during preparation.
func (t *StateTest) prepareTestEnvironment(fork string, postStateIndex int, vmconfig vm.Config, snapshotter bool, scheme string) (
	st StateTestState,
	config *params.ChainConfig,
	block *types.Block,
	msg *core.Message,
	context vm.BlockContext,
	err error,
) {
	config, eips, err := tests.GetChainConfig(fork)
	if err != nil {
		return st, nil, nil, nil, vm.BlockContext{}, tests.UnsupportedForkError{Name: fork}
	}
	vmconfig.ExtraEips = eips

	block = t.genesis(config).ToBlock()
	st = makePreState(rawdb.NewMemoryDatabase(), t.json.Pre, snapshotter, scheme)

	var baseFee *big.Int
	if config.IsLondon(new(big.Int)) {
		baseFee = t.json.Env.BaseFee
		if baseFee == nil {
			baseFee = big.NewInt(0x0a)
		}
	}
	post := t.json.Post[fork][postStateIndex]
	msg, err = t.json.Tx.toMessage(post, baseFee)
	if err != nil {
		return st, nil, nil, nil, vm.BlockContext{}, err
	}

	if config.IsCancun(new(big.Int), block.Time()) {
		if len(msg.BlobHashes) > eip4844.MaxBlobsPerBlock(config, block.Time()) {
			return st, nil, nil, nil, vm.BlockContext{}, errors.New("blob gas exceeds maximum")
		}
	}

	if len(post.TxBytes) != 0 {
		var ttx types.Transaction
		err := ttx.UnmarshalBinary(post.TxBytes)
		if err != nil {
			return st, nil, nil, nil, vm.BlockContext{}, err
		}
		if _, err := types.Sender(types.LatestSigner(config), &ttx); err != nil {
			return st, nil, nil, nil, vm.BlockContext{}, err
		}
	}

	context = core.NewEVMBlockContext(block.Header(), &dummyChain{config: config}, &t.json.Env.Coinbase)
	context.GetHash = vmTestBlockHash
	context.BaseFee = baseFee
	context.Random = nil
	if t.json.Env.Difficulty != nil {
		context.Difficulty = new(big.Int).Set(t.json.Env.Difficulty)
	}
	if config.IsLondon(new(big.Int)) && t.json.Env.Random != nil {
		rnd := common.BigToHash(t.json.Env.Random)
		context.Random = &rnd
		context.Difficulty = big.NewInt(0)
	}
	if config.IsCancun(new(big.Int), block.Time()) && t.json.Env.ExcessBlobGas != nil {
		header := &types.Header{
			Time:          block.Time(),
			ExcessBlobGas: t.json.Env.ExcessBlobGas,
		}
		context.BlobBaseFee = eip4844.CalcBlobFee(config, header)
	}

	return st, config, block, msg, context, nil
}

// RunNoVerify runs a specific subtest and returns the statedb and post-state root.
func (t *StateTest) RunNoVerify(subtest StateSubtest, vmconfig vm.Config, snapshotter bool, scheme string) (st StateTestState, root common.Hash, gasUsed uint64, err error) {
	st, config, block, msg, context, err := t.prepareTestEnvironment(subtest.Fork, subtest.Index, vmconfig, snapshotter, scheme)
	if err != nil {
		return st, common.Hash{}, 0, err
	}

	evm := vm.NewEVM(context, st.StateDB, config, vmconfig)

	if tracer := vmconfig.Tracer; tracer != nil && tracer.OnTxStart != nil {
		tracer.OnTxStart(evm.GetVMContext(), nil, msg.From)
	}
	snapshot := st.StateDB.Snapshot()
	gaspool := new(core.GasPool)
	gaspool.AddGas(block.GasLimit())
	vmRet, err := core.ApplyMessage(evm, msg, gaspool)
	if err != nil {
		st.StateDB.RevertToSnapshot(snapshot)
		if tracer := evm.Config.Tracer; tracer != nil && tracer.OnTxEnd != nil {
			evm.Config.Tracer.OnTxEnd(nil, err)
		}
		return st, common.Hash{}, 0, err
	}
	st.StateDB.AddBalance(block.Coinbase(), new(uint256.Int), tracing.BalanceChangeUnspecified)

	// Commit the state - need to access the underlying ethStateDB
	if dualDB, ok := st.StateDB.(*endorser.DualStateDB); ok {
		root, err = dualDB.EthStateDB().Commit(block.NumberU64(), config.IsEIP158(block.Number()), config.IsCancun(block.Number(), block.Time()))
		if err != nil {
			return st, common.Hash{}, 0, fmt.Errorf("commit failed: %w", err)
		}
	} else if ethDB, ok := st.StateDB.(*state.StateDB); ok {
		root, err = ethDB.Commit(block.NumberU64(), config.IsEIP158(block.Number()), config.IsCancun(block.Number(), block.Time()))
		if err != nil {
			return st, common.Hash{}, 0, fmt.Errorf("commit failed: %w", err)
		}
	} else if loggerDB, ok := st.StateDB.(*endorser.EthStateDBLogger); ok {
		root, err = loggerDB.Commit(block.NumberU64(), config.IsEIP158(block.Number()), config.IsCancun(block.Number(), block.Time()))
		if err != nil {
			return st, common.Hash{}, 0, fmt.Errorf("commit failed: %w", err)
		}
	} else {
		return st, common.Hash{}, 0, fmt.Errorf("unknown StateDB type: %T", st.StateDB)
	}

	if tracer := evm.Config.Tracer; tracer != nil && tracer.OnTxEnd != nil {
		receipt := &types.Receipt{GasUsed: vmRet.UsedGas}
		tracer.OnTxEnd(receipt, nil)
	}
	return st, root, vmRet.UsedGas, nil
}

func (t *StateTest) genesis(config *params.ChainConfig) *core.Genesis {
	genesis := &core.Genesis{
		Config:     config,
		Coinbase:   t.json.Env.Coinbase,
		Difficulty: t.json.Env.Difficulty,
		GasLimit:   t.json.Env.GasLimit,
		Number:     t.json.Env.Number,
		Timestamp:  t.json.Env.Timestamp,
		Alloc:      t.json.Pre,
	}
	if t.json.Env.Random != nil {
		// Post-Merge
		genesis.Mixhash = common.BigToHash(t.json.Env.Random)
		genesis.Difficulty = big.NewInt(0)
	}
	return genesis
}

func (tx *stTransaction) toMessage(ps stPostState, baseFee *big.Int) (*core.Message, error) {
	var from common.Address
	if tx.Sender != nil {
		from = *tx.Sender
	} else if len(tx.PrivateKey) > 0 {
		key, err := crypto.ToECDSA(tx.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("invalid private key: %v", err)
		}
		from = crypto.PubkeyToAddress(key.PublicKey)
	}
	var to *common.Address
	if tx.To != "" {
		to = new(common.Address)
		if err := to.UnmarshalText([]byte(tx.To)); err != nil {
			return nil, fmt.Errorf("invalid to address: %v", err)
		}
	}

	if ps.Indexes.Data > len(tx.Data) {
		return nil, fmt.Errorf("tx data index %d out of bounds", ps.Indexes.Data)
	}
	if ps.Indexes.Value > len(tx.Value) {
		return nil, fmt.Errorf("tx value index %d out of bounds", ps.Indexes.Value)
	}
	if ps.Indexes.Gas > len(tx.GasLimit) {
		return nil, fmt.Errorf("tx gas limit index %d out of bounds", ps.Indexes.Gas)
	}
	dataHex := tx.Data[ps.Indexes.Data]
	valueHex := tx.Value[ps.Indexes.Value]
	gasLimit := tx.GasLimit[ps.Indexes.Gas]
	value := new(big.Int)
	if valueHex != "0x" {
		v, ok := math.ParseBig256(valueHex)
		if !ok {
			return nil, fmt.Errorf("invalid tx value %q", valueHex)
		}
		value = v
	}
	data, err := hex.DecodeString(strings.TrimPrefix(dataHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("invalid tx data %q", dataHex)
	}
	var accessList types.AccessList
	if tx.AccessLists != nil && tx.AccessLists[ps.Indexes.Data] != nil {
		accessList = *tx.AccessLists[ps.Indexes.Data]
	}
	gasPrice := tx.GasPrice
	if baseFee != nil {
		if tx.MaxFeePerGas == nil {
			tx.MaxFeePerGas = gasPrice
		}
		if tx.MaxFeePerGas == nil {
			tx.MaxFeePerGas = new(big.Int)
		}
		if tx.MaxPriorityFeePerGas == nil {
			tx.MaxPriorityFeePerGas = tx.MaxFeePerGas
		}
		gasPrice = new(big.Int).Add(tx.MaxPriorityFeePerGas, baseFee)
		if gasPrice.Cmp(tx.MaxFeePerGas) > 0 {
			gasPrice = tx.MaxFeePerGas
		}
	}
	if gasPrice == nil {
		return nil, errors.New("no gas price provided")
	}
	var authList []types.SetCodeAuthorization
	if tx.AuthorizationList != nil {
		authList = make([]types.SetCodeAuthorization, len(tx.AuthorizationList))
		for i, auth := range tx.AuthorizationList {
			authList[i] = types.SetCodeAuthorization{
				ChainID: *uint256.MustFromBig(auth.ChainID),
				Address: auth.Address,
				Nonce:   auth.Nonce,
				V:       auth.V,
				R:       *uint256.MustFromBig(auth.R),
				S:       *uint256.MustFromBig(auth.S),
			}
		}
	}

	msg := &core.Message{
		From:                  from,
		To:                    to,
		Nonce:                 tx.Nonce,
		Value:                 value,
		GasLimit:              gasLimit,
		GasPrice:              gasPrice,
		GasFeeCap:             tx.MaxFeePerGas,
		GasTipCap:             tx.MaxPriorityFeePerGas,
		Data:                  data,
		AccessList:            accessList,
		BlobHashes:            tx.BlobVersionedHashes,
		BlobGasFeeCap:         tx.BlobGasFeeCap,
		SetCodeAuthorizations: authList,
	}
	return msg, nil
}

//lint:ignore U1000 kept for future tests / debugging
func makePreState(db ethdb.Database, accounts types.GenesisAlloc, snapshotter bool, scheme string) StateTestState {
	// Use the same approach as go-ethereum's MakePreState
	tconf := &triedb.Config{Preimages: true}
	if scheme == rawdb.HashScheme {
		tconf.HashDB = &hashdb.Config{}
	} else {
		tconf.PathDB = &pathdb.Config{}
	}
	trieDB := triedb.NewDatabase(db, tconf)
	sdb := state.NewDatabase(trieDB, nil)
	statedb, _ := state.New(types.EmptyRootHash, sdb)

	// Populate accounts
	for addr, a := range accounts {
		statedb.SetCode(addr, a.Code, tracing.CodeChangeUnspecified)
		statedb.SetNonce(addr, a.Nonce, tracing.NonceChangeUnspecified)
		statedb.SetBalance(addr, uint256.MustFromBig(a.Balance), tracing.BalanceChangeUnspecified)
		for k, v := range a.Storage {
			statedb.SetState(addr, k, v)
		}
	}

	// Commit and re-open to start with a clean state
	root, _ := statedb.Commit(0, false, false)

	// If snapshot is requested, initialize the snapshotter and use it in state
	var snaps *snapshot.Tree
	if snapshotter {
		snapconfig := snapshot.Config{
			CacheSize:  1,
			Recovery:   false,
			NoBuild:    false,
			AsyncBuild: false,
		}
		snaps, _ = snapshot.New(snapconfig, db, trieDB, root)
	}
	sdb = state.NewDatabase(trieDB, snaps)
	statedb, _ = state.New(root, sdb)

	// Wrap with logger for debugging
	loggedStateDB := endorser.NewEthStateDBLogger(statedb)

	// Return plain ethStateDB (not wrapped in DualStateDB)
	// The Run method already handles both DualStateDB and plain StateDB
	return StateTestState{loggedStateDB, trieDB, snaps}
}

//lint:ignore U1000 kept for future tests / debugging
func makePreStateWithDualState(db ethdb.Database, accounts types.GenesisAlloc, snapshotter bool, scheme string) StateTestState {
	// Use the same approach as go-ethereum's MakePreState
	tconf := &triedb.Config{Preimages: true}
	if scheme == rawdb.HashScheme {
		tconf.HashDB = &hashdb.Config{}
	} else {
		tconf.PathDB = &pathdb.Config{}
	}
	trieDB := triedb.NewDatabase(db, tconf)
	sdb := state.NewDatabase(trieDB, nil)
	ethStateDB, _ := state.New(types.EmptyRootHash, sdb)

	// Create a mock StateDB for DualStateDB
	fabricDB, _ := fabricstate.NewWriteDB("testchannel", ":memory:")
	fabricStateDB, _ := endorser.NewStateDB(context.TODO(), fabricDB, "testns", 0, false)

	// Use DualStateDB instead of plain StateDB for debugging
	statedb := endorser.NewDualStateDB(ethStateDB, fabricStateDB)

	// Populate accounts
	for addr, a := range accounts {
		statedb.CreateAccount(addr)
		statedb.AddBalance(addr, uint256.MustFromBig(a.Balance), tracing.BalanceChangeUnspecified)
		statedb.SetCode(addr, a.Code, tracing.CodeChangeUnspecified)
		statedb.SetNonce(addr, a.Nonce, tracing.NonceChangeUnspecified)
		for k, v := range a.Storage {
			statedb.SetState(addr, k, v)
		}
	}

	// Commit and re-open to start with a clean state
	root, _ := statedb.EthStateDB().Commit(0, false, false)

	// Commit the fabric state to the database
	rws := fabricStateDB.Result()
	err := fabricDB.UpdateWorldState(context.TODO(), blocks.Block{Number: 0, Transactions: []blocks.Transaction{
		{
			ID:     "setup",
			Number: 0,
			Valid:  true,
			NsRWS: []blocks.NsReadWriteSet{
				{
					Namespace: "testns",
					RWS:       rws,
				},
			},
		},
	}})
	if err != nil {
		panic(err)
	}

	// If snapshot is requested, initialize the snapshotter and use it in state
	var snaps *snapshot.Tree
	if snapshotter {
		snapconfig := snapshot.Config{
			CacheSize:  1,
			Recovery:   false,
			NoBuild:    false,
			AsyncBuild: false,
		}
		snaps, _ = snapshot.New(snapconfig, db, trieDB, root)
	}
	sdb = state.NewDatabase(trieDB, snaps)
	ethStateDB, _ = state.New(root, sdb)

	// Create new StateDB for the reopened state - now reading from block 1
	// since we just committed block 0
	fabricStateDB, _ = endorser.NewStateDB(context.TODO(), fabricDB, "testns", 1, false)
	statedb = endorser.NewDualStateDB(ethStateDB, fabricStateDB)

	return StateTestState{statedb, trieDB, snaps}
}

func vmTestBlockHash(n uint64) common.Hash {
	return common.BytesToHash(crypto.Keccak256([]byte(big.NewInt(int64(n)).String())))
}

func rlpHash(x interface{}) (h common.Hash) {
	hw := sha3.NewLegacyKeccak256()
	rlp.Encode(hw, x)
	hw.Sum(h[:0])
	return h
}

type dummyChain struct {
	config *params.ChainConfig
}

func (d *dummyChain) Engine() consensus.Engine                        { return nil }
func (d *dummyChain) GetHeader(h common.Hash, n uint64) *types.Header { return nil }
func (d *dummyChain) GetHeaderByHash(h common.Hash) *types.Header     { return nil }
func (d *dummyChain) GetHeaderByNumber(n uint64) *types.Header        { return nil }
func (d *dummyChain) Config() *params.ChainConfig                     { return d.config }
func (d *dummyChain) CurrentHeader() *types.Header                    { return nil }

// Made with Bob
