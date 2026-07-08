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
	fxcommon "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// EVMConfig holds the configuration for EVM execution.
type EVMConfig struct {
	ChainConfig *params.ChainConfig
	// MaxTxGas caps msg.GasLimit before execution. 0 means unlimited.
	MaxTxGas uint64
	// DebugLogs wraps the per-tx StateDB in StateDBLogger when true.
	DebugLogs bool
}

type KVSSnapshotter interface {
	NewSnapshot(blockNumber uint64) (ReadStore, error)
}

// KVS is implemented by both LightKVS and VersionedDBWrapper.
// It combines snapshot reads, block handling, and lifecycle management.
type KVS interface {
	KVSSnapshotter
	blocks.BlockHandler
	blocks.RecordGetter
	BlockNumber(context.Context) (uint64, error)
	Close() error
}

// EVMEngine manages EVM execution and state reads for an endorser.
// It creates isolated per-transaction snapshots for execution and reads state directly
// for ChainStateReader calls.
type EVMEngine struct {
	namespace         string
	monotonicVersions bool

	// kvs provides versioned storage with snapshot isolation
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
// Reverts produce a valid endorsement (Status 201 + revert event) instead of an error.
func (e *EVMEngine) Execute(ctx context.Context, tx *types.Transaction) (endorsement.ExecutionResult, error) {
	ex, err := e.newExecutor(nil)
	if err != nil {
		return endorsement.ExecutionResult{}, err
	}
	defer ex.Close()

	ret, err := ex.Send(tx)
	if err != nil && !errors.Is(err, vm.ErrExecutionReverted) {
		return endorsement.ExecutionResult{}, err
	}
	if errors.Is(err, vm.ErrExecutionReverted) {
		event, mErr := fxcommon.MarshalRevert(ret, "", tx.Hash().Hex())
		if mErr != nil {
			return endorsement.ExecutionResult{}, fmt.Errorf("marshal revert event: %w", mErr)
		}
		return endorsement.ExecutionResult{
			RWS:     ex.state.Result(),
			Event:   event,
			Status:  201,
			Message: err.Error(),
			Payload: ret,
		}, nil
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
	ex, err := e.newExecutor(blockNumber)
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
// blockNumber selects the Fabric block height for the state snapshot (nil = latest).
func (e *EVMEngine) newExecutor(blockNumber *big.Int) (*Executor, error) {
	var stateBlockNum uint64
	if blockNumber != nil {
		stateBlockNum = blockNumber.Uint64()
	}

	// Begin a new reader to get snapshot isolation
	reader, err := e.kvs.NewSnapshot(stateBlockNum)
	if err != nil {
		return nil, err
	}

	// Create StateDB with the reader
	stateDB, err := NewStateDB(context.TODO(), reader, e.namespace, stateBlockNum, e.monotonicVersions)
	if err != nil {
		reader.Close()
		return nil, err
	}
	var state ExtendedStateDB = stateDB
	if e.evmConfig.DebugLogs {
		state = NewStateDBLogger(stateDB)
	}
	return NewExecutor(state, reader, blockNumber, e.evmConfig)
}

// newSnapshotAt returns an ExtendedStateDB over the state at the given Fabric block height (0 = latest).
// The caller must close the returned reader when done.
func (e *EVMEngine) newSnapshotAt(blockNumber *big.Int) (ExtendedStateDB, ReadStore, error) {
	blockNum := uint64(0)
	if blockNumber != nil {
		blockNum = blockNumber.Uint64()
	}

	// Begin a new reader to get snapshot isolation
	reader, err := e.kvs.NewSnapshot(blockNum)
	if err != nil {
		return nil, nil, err
	}

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
	ChainCfg *params.ChainConfig
	BlockCtx vm.BlockContext
	maxTxGas uint64
}

// NewExecutor creates an Executor with the provided StateDB and reader.
// blockNumber sets the EVM block context (nil = 0). evmConfig.ChainConfig must be set.
// The caller is responsible for closing the reader when done with the Executor.
// The stateDB parameter accepts ExtendedStateDB interface to allow DualStateDB for testing.
func NewExecutor(stateDB ExtendedStateDB, reader ReadStore, blockNumber *big.Int, evmConfig EVMConfig) (*Executor, error) {
	if evmConfig.ChainConfig == nil {
		return nil, fmt.Errorf("evmConfig.ChainConfig must be set")
	}

	if blockNumber == nil {
		blockNumber = new(big.Int)
	}
	const defaultBlockTime = uint64(1_000_000)
	const defaultGasLimit = uint64(300_000_000)

	blockCtx := vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    core.Transfer,
		GetHash:     func(uint64) common.Hash { return common.Hash{} },
		Coinbase:    common.HexToAddress("0x0"),
		BlockNumber: blockNumber,
		Time:        defaultBlockTime,
		Difficulty:  big.NewInt(0),  // disabled post-merge
		Random:      &common.Hash{}, // Warning: PREVRANDAO stub must not be relied on by smart contracts.
		GasLimit:    defaultGasLimit,
		BaseFee:     big.NewInt(0),
	}

	// Cancun requires a non-nil BlobBaseFee; state_transition.go dereferences it directly
	// for blob transactions.
	if evmConfig.ChainConfig.IsCancun(blockNumber, defaultBlockTime) {
		excess := uint64(0)
		blockCtx.BlobBaseFee = eip4844.CalcBlobFee(evmConfig.ChainConfig, &types.Header{ExcessBlobGas: &excess})
	}

	return &Executor{
		state:    stateDB,
		reader:   reader,
		ChainCfg: evmConfig.ChainConfig,
		BlockCtx: blockCtx,
		maxTxGas: evmConfig.MaxTxGas,
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
	ret, err := h.Execute(CallMsgToMessage(msg, h.BlockCtx.BaseFee, true, true))
	if errors.Is(err, vm.ErrExecutionReverted) && len(ret) == 0 {
		return nil, nil // empty revert on a call is not an error
	}
	return ret, formatRevert(ret, err)
}

// PrepareMessage validates the sender nonce and converts tx to a core.Message.
// Exported for use by testimpl wrappers that need to build a message without
// applying the production free-gas defaults.
func (h *Executor) PrepareMessage(tx *types.Transaction) (*core.Message, error) {
	signer := types.MakeSigner(h.ChainCfg, h.BlockCtx.BlockNumber, h.BlockCtx.Time)

	from, err := types.Sender(signer, tx)
	if err != nil {
		return nil, err
	}

	// Validate that the transaction nonce matches the ledger state nonce.
	// This adds an explicit read dependency on the ledger key for the nonce.
	ledgerNonce := h.state.GetNonce(from)
	if tx.Nonce() < ledgerNonce {
		return nil, core.ErrNonceTooLow
	} else if tx.Nonce() > ledgerNonce {
		return nil, core.ErrNonceTooHigh
	}

	return core.TransactionToMessage(tx, signer, h.BlockCtx.BaseFee)
}

// Send validates nonce, converts tx to a message, applies production defaults, and executes.
func (h *Executor) Send(tx *types.Transaction) ([]byte, error) {
	msg, err := h.PrepareMessage(tx)
	if err != nil {
		return nil, err
	}

	ret, err := h.Execute(msg)
	if err != nil {
		return nil, formatRevert(ret, err)
	}

	return ret, nil
}

// Execute applies production defaults then runs the EVM via ApplyMessage.
// Gas prices are always zeroed (free gas) so buyGas never requires ETH balance.
// If MaxTxGas is set, msg.GasLimit is capped before execution.
func (h *Executor) Execute(msg *core.Message) ([]byte, error) {
	if msg.GasLimit == 0 {
		msg.GasLimit = 5_000_000
	}

	// Free gas: zero all prices so buyGas never requires ETH balance from the sender.
	msg.GasPrice = new(big.Int)
	msg.GasFeeCap = new(big.Int)
	msg.GasTipCap = new(big.Int)

	// Cap gas limit for DoS protection.
	if h.maxTxGas > 0 && msg.GasLimit > h.maxTxGas {
		msg.GasLimit = h.maxTxGas
	}

	return h.ApplyMessage(msg)
}

// ApplyMessage runs msg on the EVM exactly as provided, without production defaults.
// Use this in test infrastructure (testimpl) when real gas pricing is needed.
func (h *Executor) ApplyMessage(msg *core.Message) ([]byte, error) {
	evm := vm.NewEVM(h.BlockCtx, h.state, h.ChainCfg, vm.Config{})

	// Snapshot before execution mirrors geth's approach and allows reverting on error.
	snapshot := h.state.Snapshot()

	// The block gas pool must reflect the enclosing block gas limit, not the tx gas
	// limit. Otherwise a tx with gas limit above the block gas limit incorrectly
	// passes preCheck and executes.
	gp := new(core.GasPool).AddGas(h.BlockCtx.GasLimit)

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
