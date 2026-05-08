/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package endorser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/hyperledger/fabric-x-evm/utils"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// EVMConfig holds the configuration for EVM execution.
// It allows callers to specify the BlockContext, ChainConfig, and VMConfig
// that will be used when creating the EVM instance.
type EVMConfig struct {
	BlockContext *vm.BlockContext
	ChainConfig  *params.ChainConfig
	VMConfig     *vm.Config
	// FreeGas disables gas-fee balance enforcement.
	FreeGas bool
}

type KVSSnapshotter interface {
	NewSnapshot() ReadStore
}

// EVMEngine manages EVM execution and state reads for an endorser.
// It creates isolated per-transaction snapshots for execution and reads state directly
// for ChainStateReader calls.
type EVMEngine struct {
	namespace         string
	monotonicVersions bool

	// LightKVS provides versioned storage with snapshot isolation
	kvs       KVSSnapshotter
	evmConfig EVMConfig
}

// NewEVMEngine creates a new EVMEngine.
func NewEVMEngine(namespace string, kvs KVSSnapshotter, evmConfig EVMConfig, monotonicVersions bool) *EVMEngine {
	return &EVMEngine{
		namespace:         namespace,
		kvs:               kvs,
		monotonicVersions: monotonicVersions,
		evmConfig:         evmConfig,
	}
}

// Execute runs a state-changing transaction and returns the EVM result,
// the Fabric read-write set, and any EVM logs emitted.
// State is always read from the latest block: endorsement must simulate against current state
// so that the resulting read-write set passes MVCC validation at commit time.
func (e *EVMEngine) Execute(blockInfo *utils.BlockInfo, tx *types.Transaction) (endorsement.ExecutionResult, error) {
	ex, err := e.newExecutor(blockInfo, 0)
	if err != nil {
		return endorsement.ExecutionResult{}, err
	}
	defer ex.Close()

	ret, err := ex.Send(tx)
	if err != nil {
		return endorsement.ExecutionResult{}, err
	}
	var logs []byte
	if l := ex.state.Logs(); len(l) > 0 {
		logs, err = json.Marshal(l)
		if err != nil {
			return endorsement.ExecutionResult{}, errors.New("error marshaling logs")
		}
	}

	return endorsement.Success(ex.state.Result(), logs, ret), nil
}

// Call executes a read-only call (eth_call semantics) against the state at blockNumber
// (0 / nil = latest). The EVM block context is not reconstructed for historical blocks —
// with all forks enabled from block 0 this is harmless.
func (e *EVMEngine) Call(msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	stateBlock := uint64(0)
	if blockNumber != nil {
		stateBlock = blockNumber.Uint64()
	}
	ex, err := e.newExecutor(nil, stateBlock)
	if err != nil {
		return nil, err
	}
	defer ex.Close()

	return ex.Call(msg)
}

func (e *EVMEngine) BalanceAt(_ context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	snap, reader, err := e.newSnapshotAt(blockNumber)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return snap.GetBalance(account).ToBig(), nil
}

func (e *EVMEngine) StorageAt(_ context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error) {
	snap, reader, err := e.newSnapshotAt(blockNumber)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return snap.GetState(account, key).Bytes(), nil
}

func (e *EVMEngine) CodeAt(_ context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	snap, reader, err := e.newSnapshotAt(blockNumber)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return snap.GetCode(account), nil
}

func (e *EVMEngine) NonceAt(_ context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	snap, reader, err := e.newSnapshotAt(blockNumber)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	return snap.GetNonce(account), nil
}

// newExecutor creates a fresh executor with an isolated StateDB.
// stateBlockNum selects the Fabric block height for the state snapshot (0 = latest).
func (e *EVMEngine) newExecutor(blockInfo *utils.BlockInfo, stateBlockNum uint64) (*Executor, error) {
	// Begin a new reader to get snapshot isolation
	reader := e.kvs.NewSnapshot()

	// Create StateDB with the reader
	stateDB, err := NewStateDB(context.TODO(), reader, e.namespace, stateBlockNum, e.monotonicVersions)
	if err != nil {
		reader.Close()
		return nil, err
	}
	return NewExecutor(stateDB, reader, blockInfo, e.evmConfig)
}

// newSnapshotAt returns an ExtendedStateDB over the state at the given Fabric block height (0 = latest).
// The caller must close the returned reader when done.
func (e *EVMEngine) newSnapshotAt(blockNumber *big.Int) (ExtendedStateDB, ReadStore, error) {
	blockNum := uint64(0)
	if blockNumber != nil {
		blockNum = blockNumber.Uint64()
	}

	// Begin a new reader to get snapshot isolation
	reader := e.kvs.NewSnapshot()

	// Create StateDB with the reader
	stateDB, err := NewStateDB(context.TODO(), reader, e.namespace, blockNum, e.monotonicVersions)
	if err != nil {
		reader.Close()
		return nil, nil, err
	}
	return stateDB, reader, nil
}

// Executor is a per-transaction EVM execution context.
type Executor struct {
	state    ExtendedStateDB
	reader   ReadStore // reader that must be closed when done
	chainCfg *params.ChainConfig
	blockCtx vm.BlockContext
	vmConfig vm.Config
	freeGas  bool
}

// NewExecutor creates an Executor with the provided StateDB and reader.
// If blockInfo is not provided, the store's current version is used as the block number.
// evmConfig.ChainConfig must be set.
// The caller is responsible for closing the reader when done with the Executor.
// The stateDB parameter accepts ExtendedStateDB interface to allow DualStateDB for testing.
func NewExecutor(stateDB ExtendedStateDB, reader ReadStore, blockInfo *utils.BlockInfo, evmConfig EVMConfig) (*Executor, error) {
	if evmConfig.ChainConfig == nil {
		return nil, fmt.Errorf("evmConfig.ChainConfig must be set")
	}
	if blockInfo == nil {
		blockInfo = &utils.BlockInfo{
			BlockNumber: new(big.Int),
			BlockTime:   1_000_000,
			GasLimit:    10_000_000,
		}
	}

	// Default block context
	blockCtx := vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    core.Transfer,
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
		Coinbase:    common.HexToAddress("0x0"),
		BlockNumber: blockInfo.BlockNumber,
		Time:        blockInfo.BlockTime,
		Difficulty:  big.NewInt(0),  // disabled post-merge
		Random:      &common.Hash{}, // Warning: PREVRANDAO stub must not be relied on by smart contracts.
		GasLimit:    blockInfo.GasLimit,
		BaseFee:     big.NewInt(0),
	}

	// Cancun requires a non-nil BlobBaseFee; state_transition.go dereferences it directly
	// for blob transactions. Calculate from ExcessBlobGas (0 → 1 wei minimum).
	if evmConfig.ChainConfig.IsCancun(blockInfo.BlockNumber, blockInfo.BlockTime) {
		excess := uint64(0)
		if blockInfo.ExcessBlobGas != nil {
			excess = *blockInfo.ExcessBlobGas
		}
		blockCtx.BlobBaseFee = eip4844.CalcBlobFee(evmConfig.ChainConfig, &types.Header{ExcessBlobGas: &excess})
	}

	// Default VM config
	vmConfig := vm.Config{}

	// Override with custom config if provided
	if evmConfig.BlockContext != nil {
		blockCtx = *evmConfig.BlockContext
	}
	if evmConfig.VMConfig != nil {
		vmConfig = *evmConfig.VMConfig
	}

	return &Executor{
		state:    stateDB,
		reader:   reader,
		chainCfg: evmConfig.ChainConfig,
		blockCtx: blockCtx,
		vmConfig: vmConfig,
		freeGas:  evmConfig.FreeGas,
	}, nil
}

// Close releases the reader's snapshot reference.
// This should be called when the Executor is done to allow garbage collection.
func (h *Executor) Close() error {
	if h.reader != nil {
		return h.reader.Close()
	}
	return nil
}

// CallMsgToMessage converts an ethereum.CallMsg into a core.Message.
// The baseFee parameter is used to calculate the effective gas price for EIP-1559 transactions.
// If baseFee is nil, legacy gas pricing is used.
// skipNonceCheck and skipTxCheck control whether nonce and EOA checks should be skipped.
func CallMsgToMessage(msg ethereum.CallMsg, baseFee *big.Int, skipNonceCheck, skipTxCheck bool) *core.Message {
	var (
		gasPrice  *big.Int
		gasFeeCap *big.Int
		gasTipCap *big.Int
	)

	if baseFee == nil {
		// Legacy gas pricing
		if msg.GasPrice != nil {
			gasPrice = msg.GasPrice
		} else {
			gasPrice = new(big.Int)
		}
		gasFeeCap, gasTipCap = gasPrice, gasPrice
	} else {
		// EIP-1559 gas pricing
		if msg.GasPrice != nil {
			// Legacy gas field provided, convert to 1559 gas typing
			gasPrice = msg.GasPrice
			gasFeeCap, gasTipCap = gasPrice, gasPrice
		} else {
			// Use 1559 gas fields
			if msg.GasFeeCap != nil {
				gasFeeCap = msg.GasFeeCap
			} else {
				gasFeeCap = new(big.Int)
			}
			if msg.GasTipCap != nil {
				gasTipCap = msg.GasTipCap
			} else {
				gasTipCap = new(big.Int)
			}
			// Calculate effective gas price for EVM execution
			gasPrice = new(big.Int)
			if gasFeeCap.BitLen() > 0 || gasTipCap.BitLen() > 0 {
				gasPrice = new(big.Int).Add(gasTipCap, baseFee)
				if gasPrice.Cmp(gasFeeCap) > 0 {
					gasPrice = gasFeeCap
				}
			}
		}
	}

	// Handle nil Value
	value := msg.Value
	if value == nil {
		value = new(big.Int)
	}

	// Handle nil blob gas fee cap
	blobGasFeeCap := msg.BlobGasFeeCap
	if blobGasFeeCap == nil {
		blobGasFeeCap = new(big.Int)
	}

	return &core.Message{
		From:                  msg.From,
		To:                    msg.To,
		Value:                 value,
		Nonce:                 0, // CallMsg doesn't have a nonce
		GasLimit:              msg.Gas,
		GasPrice:              gasPrice,
		GasFeeCap:             gasFeeCap,
		GasTipCap:             gasTipCap,
		Data:                  msg.Data,
		AccessList:            msg.AccessList,
		BlobGasFeeCap:         blobGasFeeCap,
		BlobHashes:            msg.BlobHashes,
		SetCodeAuthorizations: msg.AuthorizationList,
		SkipNonceChecks:       skipNonceCheck,
		SkipTransactionChecks: skipTxCheck,
	}
}

// Call executes a read-only call (eth_call semantics).
// An empty revert is treated as a non-error: many Ethereum tools probe contracts this way.
func (h *Executor) Call(msg ethereum.CallMsg) ([]byte, error) {
	ret, err := h.Execute(CallMsgToMessage(msg, h.blockCtx.BaseFee, true, true))
	if errors.Is(err, vm.ErrExecutionReverted) && len(ret) == 0 {
		return nil, nil // empty revert on a call is not an error
	}
	return ret, formatRevert(ret, err)
}

// Send executes a state-changing transaction, increments the sender nonce and returns the result.
func (h *Executor) Send(tx *types.Transaction) ([]byte, error) {
	signer := types.MakeSigner(h.chainCfg, h.blockCtx.BlockNumber, h.blockCtx.Time)

	from, err := types.Sender(signer, tx)
	if err != nil {
		return nil, err
	}

	// Validate that the transaction nonce matches the ledger state nonce
	// This adds an explicit read dependency on the ledger key of the nonce
	ledgerNonce := h.state.GetNonce(from)
	if tx.Nonce() < ledgerNonce {
		return nil, core.ErrNonceTooLow
	} else if tx.Nonce() > ledgerNonce {
		return nil, core.ErrNonceTooHigh
	}

	msg, err := core.TransactionToMessage(tx, signer, h.blockCtx.BaseFee)
	if err != nil {
		return nil, err
	}

	ret, err := h.Execute(msg)
	if err != nil {
		return nil, formatRevert(ret, err)
	}

	return ret, nil
}

// Execute dispatches a call or deployment to the EVM using ApplyMessage.
// A nil value defaults to 0; zero gas defaults to 5_000_000.
func (h *Executor) Execute(msg *core.Message) ([]byte, error) {
	// Default gas limit to 5_000_000 if not set
	if msg.GasLimit == 0 {
		msg.GasLimit = 5_000_000
	}

	// When FreeGas is enabled, zero out gas prices so buyGas never requires
	// ETH balance from the sender. At the moment this is the default; we only
	// charge gas in ethereum compatibility tests.
	if h.freeGas {
		msg.GasPrice = new(big.Int)
		msg.GasFeeCap = new(big.Int)
		msg.GasTipCap = new(big.Int)
	}

	// Create EVM instance with configured VMConfig
	evm := vm.NewEVM(h.blockCtx, h.state, h.chainCfg, h.vmConfig)

	// Take a snapshot before executing the transaction
	// This mimicks geth's approach and permits tests to pass
	snapshot := h.state.Snapshot()

	// The block gas pool must reflect the enclosing block gas limit, not the tx gas
	// limit. Otherwise a tx with gas limit above the block gas limit incorrectly
	// passes preCheck and executes.
	gp := new(core.GasPool).AddGas(h.blockCtx.GasLimit)

	// Use ApplyMessage to execute the transaction
	result, err := core.ApplyMessage(evm, msg, gp)
	if err != nil {
		// Revert to the snapshot on error from ApplyMessage
		// This mimicks geth's approach and permits tests to pass
		h.state.RevertToSnapshot(snapshot)
		return nil, err
	}

	// Return the result data and any execution error
	// Note: result.Err contains execution errors (e.g., revert, out of gas, code size exceeded)
	// These are not fatal errors - the transaction is included but failed
	if result.Err != nil {
		return result.ReturnData, result.Err
	}
	return result.ReturnData, nil
}

// formatRevert enriches a revert error with the ABI-decoded reason string.
// If the data cannot be unpacked, the original error is returned unchanged.
func formatRevert(ret []byte, err error) error {
	if !errors.Is(err, vm.ErrExecutionReverted) {
		return err
	}
	reason, errUnpack := abi.UnpackRevert(ret)
	if errUnpack != nil {
		return err
	}
	return fmt.Errorf("%w: %v", vm.ErrExecutionReverted, reason)
}
