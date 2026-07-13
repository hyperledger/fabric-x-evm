/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package execution

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
	"github.com/holiman/uint256"
	fxcommon "github.com/hyperledger/fabric-x-evm/common"
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

// KVSSnapshotter is the port execution uses to obtain a versioned read snapshot
// of the world state. storage.LightKVS and storage.VersionedDBWrapper implement it.
type KVSSnapshotter interface {
	NewSnapshot(blockNumber uint64) (ReadStore, error)
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
	if err != nil {
		if !errors.Is(err, vm.ErrExecutionReverted) {
			return endorsement.ExecutionResult{}, err
		}
		// Revert: a committed outcome, endorsed as Status 201 with a revert event.
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
			return endorsement.ExecutionResult{}, fmt.Errorf("marshal logs: %w", err)
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

	ex, err := NewExecutor(state, reader, blockNumber, e.evmConfig)
	if err != nil {
		reader.Close()
		return nil, err
	}
	return ex, nil
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

// callMsgToMessage converts an ethereum.CallMsg into a core.Message.
// The baseFee parameter is used to calculate the effective gas price for EIP-1559 transactions.
// If baseFee is nil, legacy gas pricing is used.
// skipNonceCheck and skipTxCheck control whether nonce and EOA checks should be skipped.
func callMsgToMessage(msg ethereum.CallMsg, baseFee *big.Int, skipNonceCheck, skipTxCheck bool) *core.Message {
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
		Value:                 uint256.MustFromBig(value),
		Nonce:                 0, // CallMsg doesn't have a nonce
		GasLimit:              msg.Gas,
		GasPrice:              uint256.MustFromBig(gasPrice),
		GasFeeCap:             uint256.MustFromBig(gasFeeCap),
		GasTipCap:             uint256.MustFromBig(gasTipCap),
		Data:                  msg.Data,
		AccessList:            msg.AccessList,
		BlobGasFeeCap:         uint256.MustFromBig(blobGasFeeCap),
		BlobHashes:            msg.BlobHashes,
		SetCodeAuthorizations: msg.AuthorizationList,
		SkipNonceChecks:       skipNonceCheck,
		SkipTransactionChecks: skipTxCheck,
	}
}

// Call executes a read-only call (eth_call semantics).
// An empty revert is treated as a non-error: many Ethereum tools probe contracts this way.
func (h *Executor) Call(msg ethereum.CallMsg) ([]byte, error) {
	ret, err := h.execute(callMsgToMessage(msg, h.BlockCtx.BaseFee, true, true))
	if errors.Is(err, vm.ErrExecutionReverted) && len(ret) == 0 {
		return nil, nil // empty revert on a call is not an error
	}
	return ret, err
}

// PrepareMessage is the transaction gate: it recovers the sender (validating the
// signature), checks the nonce against ledger state, and converts tx to a
// core.Message. Exported for testimpl wrappers that build a message without the
// production free-gas defaults.
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
		// Invalid transaction rejected before execution (bad signature, nonce, ...).
		return nil, &TxRejected{err: err}
	}

	// Return the raw EVM result: on a revert that ret is the revert data (used to
	// build the revert event); on other faults geth leaves it empty.
	return h.execute(msg)
}

// execute applies production defaults then runs the EVM via ApplyMessage.
// Gas prices are always zeroed (free gas) so buyGas never requires ETH balance.
// If MaxTxGas is set, msg.GasLimit is capped before execution.
func (h *Executor) execute(msg *core.Message) ([]byte, error) {
	if msg.GasLimit == 0 {
		msg.GasLimit = 5_000_000
	}

	// Free gas: zero all prices so buyGas never requires ETH balance from the sender.
	msg.GasPrice = new(uint256.Int)
	msg.GasFeeCap = new(uint256.Int)
	msg.GasTipCap = new(uint256.Int)

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
	gp := core.NewGasPool(h.BlockCtx.GasLimit)

	// Use ApplyMessage to execute the transaction
	result, err := core.ApplyMessage(evm, msg, gp)
	if err != nil {
		// Pre-execution rejection: the message can't be applied to this state
		// (nonce, funds, intrinsic gas, ...) and would never be accepted in a block.
		// Snapshot revert mirrors geth.
		h.state.RevertToSnapshot(snapshot)
		return nil, &TxRejected{err: err}
	}

	if result.Err != nil {
		// The transaction is valid but its EVM execution failed. A revert is a
		// committed outcome carrying a reason; other faults (out of gas, invalid
		// opcode, ...) are ExecFailures. Both are distinct from a pre-execution
		// rejection.
		if errors.Is(result.Err, vm.ErrExecutionReverted) {
			if reason, uErr := abi.UnpackRevert(result.ReturnData); uErr == nil {
				return result.ReturnData, fmt.Errorf("%w: %v", vm.ErrExecutionReverted, reason)
			}
			return result.ReturnData, result.Err
		}
		return result.ReturnData, &ExecFailure{err: result.Err}
	}
	return result.ReturnData, nil
}

// TxRejected tags an invalid transaction rejected before execution (nonce, funds,
// intrinsic gas, signature, ...): it can never be included in a block, so the
// caller sees an error rather than an endorsement.
type TxRejected struct{ err error }

// NewTxRejected wraps err as a TxRejected fault. Exported for tests and callers
// outside this package that need to construct one (the err field itself stays
// unexported so callers always go through Unwrap()).
func NewTxRejected(err error) *TxRejected { return &TxRejected{err: err} }

func (e *TxRejected) Error() string { return e.err.Error() }
func (e *TxRejected) Unwrap() error { return e.err }

// ExecFailure tags a valid transaction whose EVM execution faulted (out of gas,
// invalid opcode, ...) — in Ethereum it is mined with a failed receipt, distinct
// from a revert (which carries a reason) and from a pre-execution rejection. Both
// wrap the original go-ethereum error so errors.Is/errors.As still match the
// underlying value (e.g. vm.ErrOutOfGas).
type ExecFailure struct{ err error }

// NewExecFailure wraps err as an ExecFailure fault. Exported for tests and
// callers outside this package; see NewTxRejected for why err stays unexported.
func NewExecFailure(err error) *ExecFailure { return &ExecFailure{err: err} }

func (e *ExecFailure) Error() string { return e.err.Error() }
func (e *ExecFailure) Unwrap() error { return e.err }
