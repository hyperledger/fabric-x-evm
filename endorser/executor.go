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
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	cmn "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/utils"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
	"github.com/hyperledger/fabric-x-sdk/state"
)

// EVMEngine manages EVM execution and state reads for an endorser.
// It creates isolated per-transaction snapshots for execution and reads state directly
// for ChainStateReader calls.
type EVMEngine struct {
	namespace         string
	chainCfg          *params.ChainConfig
	monotonicVersions bool
	db                state.ReadStore
}

// NewEVMEngine creates a new EVMEngine.
func NewEVMEngine(namespace string, db state.ReadStore, chainCfg *params.ChainConfig, monotonicVersions bool) *EVMEngine {
	return &EVMEngine{namespace: namespace, db: db, chainCfg: chainCfg, monotonicVersions: monotonicVersions}
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
	ret, err := ex.send(tx)
	if err != nil {
		return endorsement.ExecutionResult{}, err
	}
	var logs []byte
	if l := ex.state.Logs(); len(l) > 0 {
		logs, err = json.Marshal(logs)
		if err != nil {
			return endorsement.ExecutionResult{}, errors.New("error marshaling logs")
		}
	}

	return endorsement.Success(ex.state.Result(), logs, ret), nil
}

// Call executes a read-only call (eth_call semantics) against the state at blockNumber
// (0 / nil = latest). The EVM block context is not reconstructed for historical blocks —
// with AllEthashProtocolChanges fixed from block 0 this is harmless.
func (e *EVMEngine) Call(msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	stateBlock := uint64(0)
	if blockNumber != nil {
		stateBlock = blockNumber.Uint64()
	}
	ex, err := e.newExecutor(nil, stateBlock)
	if err != nil {
		return nil, err
	}
	return ex.call(msg)
}

func (e *EVMEngine) BalanceAt(_ context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	snap, err := e.newSnapshotAt(blockNumber)
	if err != nil {
		return nil, err
	}
	return snap.GetBalance(account).ToBig(), nil
}

func (e *EVMEngine) StorageAt(_ context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error) {
	snap, err := e.newSnapshotAt(blockNumber)
	if err != nil {
		return nil, err
	}
	return snap.GetState(account, key).Bytes(), nil
}

func (e *EVMEngine) CodeAt(_ context.Context, account common.Address, blockNumber *big.Int) ([]byte, error) {
	snap, err := e.newSnapshotAt(blockNumber)
	if err != nil {
		return nil, err
	}
	return snap.GetCode(account), nil
}

func (e *EVMEngine) NonceAt(_ context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	snap, err := e.newSnapshotAt(blockNumber)
	if err != nil {
		return 0, err
	}
	return snap.GetNonce(account), nil
}

// newExecutor creates a fresh executor with an isolated SimulationStore.
// stateBlockNum selects the Fabric block height for the state snapshot (0 = latest).
func (e *EVMEngine) newExecutor(blockInfo *utils.BlockInfo, stateBlockNum uint64) (*executor, error) {
	sim, err := state.NewSimulationStore(context.TODO(), e.db, e.namespace, stateBlockNum, e.monotonicVersions)
	if err != nil {
		return nil, err
	}
	return newExecutor(sim, blockInfo, e.chainCfg), nil
}

// newSnapshotAt returns a SnapshotDB over the state at the given Fabric block height (0 = latest).
func (e *EVMEngine) newSnapshotAt(blockNumber *big.Int) (*SnapshotDB, error) {
	blockNum := uint64(0)
	if blockNumber != nil {
		blockNum = blockNumber.Uint64()
	}
	sim, err := state.NewSimulationStore(context.TODO(), e.db, e.namespace, blockNum, e.monotonicVersions)
	if err != nil {
		return nil, err
	}
	return NewSnapshotDB(sim), nil
}

// executor is a per-transaction EVM execution context. It is an internal type;
// callers outside this package interact with EVMEngine instead.
type executor struct {
	state    *SnapshotDB
	chainID  *big.Int
	chainCfg *params.ChainConfig
	blockCtx vm.BlockContext
}

// newExecutor creates an executor with the provided SimulationStore.
// If blockInfo is not provided, the store's current version is used as the block number.
func newExecutor(sim *state.SimulationStore, blockInfo *utils.BlockInfo, ethChainConfig *params.ChainConfig) *executor {
	if blockInfo == nil {
		// Note: sim.Version() is a Fabric block number, not an Ethereum block number — these are
		// separate namespaces. With AllEthashProtocolChanges active from block 0 this is harmless,
		// but callers executing real transactions should always supply blockInfo explicitly.
		blockInfo = &utils.BlockInfo{
			BlockNumber: new(big.Int).SetUint64(sim.Version()),
			BlockTime:   1_000_000,
		}
	}
	if ethChainConfig == nil {
		ethChainConfig = params.AllEthashProtocolChanges
	}
	return &executor{
		state:    NewSnapshotDB(sim),
		chainID:  cmn.ChainConfig.ChainID,
		chainCfg: ethChainConfig,
		blockCtx: vm.BlockContext{
			CanTransfer: core.CanTransfer,
			Transfer:    core.Transfer,
			GetHash:     func(uint64) common.Hash { return common.Hash{} },
			Coinbase:    common.HexToAddress("0x0"),
			BlockNumber: blockInfo.BlockNumber,
			Time:        blockInfo.BlockTime,
			Difficulty:  big.NewInt(1),
			GasLimit:    10_000_000,
			BaseFee:     big.NewInt(1),
		},
	}
}

// call executes a read-only call (eth_call semantics).
// An empty revert is treated as a non-error: many Ethereum tools probe contracts this way.
func (h *executor) call(msg ethereum.CallMsg) ([]byte, error) {
	ret, err := h.execute(msg.From, msg.To, msg.Data, msg.Gas, msg.Value)
	if errors.Is(err, vm.ErrExecutionReverted) && len(ret) == 0 {
		return nil, nil // empty revert on a call is not an error
	}
	return ret, formatRevert(ret, err)
}

// send executes a state-changing transaction, increments the sender nonce and returns the result.
func (h *executor) send(tx *types.Transaction) ([]byte, error) {
	from, err := types.Sender(types.MakeSigner(h.chainCfg, h.blockCtx.BlockNumber, h.blockCtx.Time), tx)
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

	ret, err := h.execute(from, tx.To(), tx.Data(), tx.Gas(), tx.Value())
	if err != nil {
		return nil, formatRevert(ret, err)
	}

	return ret, nil
}

// execute dispatches a call or deployment to the EVM.
// If to is nil, evm.Create is used (contract deployment); otherwise evm.Call.
// A nil value defaults to 0; zero gas defaults to 5_000_000.
func (h *executor) execute(from common.Address, to *common.Address, data []byte, gas uint64, value *big.Int) ([]byte, error) {
	if value == nil {
		value = big.NewInt(0)
	}
	if gas == 0 {
		gas = 5_000_000
	}
	evm := vm.NewEVM(h.blockCtx, h.state, h.chainCfg, vm.Config{})
	evm.SetTxContext(vm.TxContext{
		Origin:   from,
		GasPrice: new(big.Int),
	})

	// contract creation
	if to == nil {
		ret, _, _, err := evm.Create(from, data, gas, uint256.MustFromBig(value))
		return ret, err
	}

	// set the new nonce before the EVM execution like geth does
	// note that we don't do it for contract creation since geth does it for us
	h.state.SetNonce(from, h.state.GetNonce(from)+1, tracing.NonceChangeUnspecified)

	// regular call
	ret, _, err := evm.Call(from, *to, data, gas, uint256.MustFromBig(value))

	return ret, err
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
