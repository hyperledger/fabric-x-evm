/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	ethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-evm/endorser"
	"github.com/hyperledger/fabric-x-evm/utils"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// ExecutorMode defines the type of custom executor to use.
type ExecutorMode int

const (
	// DualStateDBMode uses ExecutorWrapper with DualStateDB for Ethereum state tracking
	DualStateDBMode ExecutorMode = iota
	// BalancePrimingMode uses BalancePrimingExecutor for ERC-20 balance priming
	BalancePrimingMode
)

// EVMEngineWrapper wraps an EVMEngine and adds custom executor support for testing.
// It can operate in different modes to support different testing scenarios.
type EVMEngineWrapper struct {
	*endorser.EVMEngine
	mode              ExecutorMode
	ethStateDB        *ethstate.StateDB
	balancePriming    *BalancePrimingConfig
	namespace         string
	kvs               endorser.KVSSnapshotter
	evmConfig         endorser.EVMConfig
	monotonicVersions bool
}

// NewEVMEngineWrapper creates a new EVMEngineWrapper in DualStateDB mode.
// The wrapper needs access to the engine's configuration to create custom executors.
func NewEVMEngineWrapper(
	namespace string,
	kvs endorser.KVSSnapshotter,
	evmConfig endorser.EVMConfig,
	monotonicVersions bool,
	engine *endorser.EVMEngine,
) *EVMEngineWrapper {
	return &EVMEngineWrapper{
		EVMEngine:         engine,
		mode:              DualStateDBMode,
		ethStateDB:        nil,
		balancePriming:    nil,
		namespace:         namespace,
		kvs:               kvs,
		evmConfig:         evmConfig,
		monotonicVersions: monotonicVersions,
	}
}

// SetEthStateDB sets the ethStateDB for DualStateDB mode testing.
func (w *EVMEngineWrapper) SetEthStateDB(ethStateDB *ethstate.StateDB) {
	w.mode = DualStateDBMode
	w.ethStateDB = ethStateDB
}

// GetEthStateDB returns the ethStateDB used for testing.
func (w *EVMEngineWrapper) GetEthStateDB() *ethstate.StateDB {
	return w.ethStateDB
}

// SetBalancePriming configures the wrapper for BalancePriming mode.
func (w *EVMEngineWrapper) SetBalancePriming(config *BalancePrimingConfig) {
	w.mode = BalancePrimingMode
	w.balancePriming = config
}

// Execute runs a state-changing transaction and returns the EVM result.
// The behavior depends on the configured mode.
func (w *EVMEngineWrapper) Execute(blockInfo *utils.BlockInfo, tx *types.Transaction) (endorsement.ExecutionResult, error) {
	// Create the appropriate executor based on mode
	type executor interface {
		Execute(*types.Transaction) (endorsement.ExecutionResult, error)
		Close() error
	}

	var ex executor
	var err error

	switch w.mode {
	case BalancePrimingMode:
		ex, err = w.newBalancePrimingExecutor(blockInfo, 0)
	case DualStateDBMode:
		ex, err = w.newExecutorWrapper(blockInfo, 0)
	default:
		return endorsement.ExecutionResult{}, fmt.Errorf("unknown executor mode: %d", w.mode)
	}

	if err != nil {
		return endorsement.ExecutionResult{}, err
	}
	defer ex.Close()

	return ex.Execute(tx)
}

// Call executes a read-only call against the state.
// The behavior depends on the configured mode.
func (w *EVMEngineWrapper) Call(msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	stateBlock := uint64(0)
	if blockNumber != nil {
		stateBlock = blockNumber.Uint64()
	}

	// Create the appropriate executor based on mode
	type caller interface {
		Call(ethereum.CallMsg) ([]byte, error)
		Close() error
	}

	var ex caller
	var err error

	switch w.mode {
	case BalancePrimingMode:
		ex, err = w.newBalancePrimingExecutor(nil, stateBlock)
	case DualStateDBMode:
		ex, err = w.newExecutorWrapper(nil, stateBlock)
	default:
		return nil, fmt.Errorf("unknown executor mode: %d", w.mode)
	}

	if err != nil {
		return nil, err
	}
	defer ex.Close()

	return ex.Call(msg)
}

// newExecutorWrapper creates an executor wrapper with DualStateDB support.
func (w *EVMEngineWrapper) newExecutorWrapper(blockInfo *utils.BlockInfo, stateBlockNum uint64) (*ExecutorWrapper, error) {
	return NewExecutorWrapper(
		w.namespace,
		w.kvs,
		blockInfo,
		stateBlockNum,
		w.evmConfig,
		w.monotonicVersions,
		w.ethStateDB,
	)
}

// newBalancePrimingExecutor creates an executor with balance priming support.
func (w *EVMEngineWrapper) newBalancePrimingExecutor(blockInfo *utils.BlockInfo, stateBlockNum uint64) (*BalancePrimingExecutor, error) {
	return NewBalancePrimingExecutor(
		w.namespace,
		w.kvs,
		blockInfo,
		stateBlockNum,
		w.evmConfig,
		w.monotonicVersions,
		w.balancePriming,
	)
}
