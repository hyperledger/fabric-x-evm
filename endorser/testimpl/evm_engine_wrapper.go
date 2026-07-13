/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	ethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
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
	*execution.EVMEngine
	mode              ExecutorMode
	ethStateDB        *ethstate.StateDB
	balancePriming    *BalancePrimingConfig
	blockContext      *vm.BlockContext
	namespace         string
	kvs               execution.KVSSnapshotter
	evmConfig         execution.EVMConfig
	monotonicVersions bool
}

// NewEVMEngineWrapper creates a new EVMEngineWrapper in DualStateDB mode.
// The wrapper needs access to the engine's configuration to create custom executors.
func NewEVMEngineWrapper(
	namespace string,
	kvs execution.KVSSnapshotter,
	evmConfig execution.EVMConfig,
	monotonicVersions bool,
	engine *execution.EVMEngine,
) *EVMEngineWrapper {
	return &EVMEngineWrapper{
		EVMEngine:         engine,
		mode:              DualStateDBMode,
		ethStateDB:        nil,
		balancePriming:    nil,
		blockContext:      nil,
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

// SetBlockContext sets the EVM block context to use for test executions.
// When set, the executor's BlockCtx is overridden with this value after creation,
// replacing the defaults derived from BlockInfo.
func (w *EVMEngineWrapper) SetBlockContext(ctx *vm.BlockContext) {
	w.blockContext = ctx
}

// SetBalancePriming configures the wrapper for BalancePriming mode.
func (w *EVMEngineWrapper) SetBalancePriming(config *BalancePrimingConfig) {
	w.mode = BalancePrimingMode
	w.balancePriming = config
}

// Execute runs a state-changing transaction and returns the EVM result.
// The behavior depends on the configured mode.
func (w *EVMEngineWrapper) Execute(ctx context.Context, tx *types.Transaction) (endorsement.ExecutionResult, error) {
	// Create the appropriate executor based on mode
	type executor interface {
		Execute(*types.Transaction) (endorsement.ExecutionResult, error)
		Close() error
	}

	var ex executor
	var err error

	switch w.mode {
	case BalancePrimingMode:
		ex, err = w.newBalancePrimingExecutor()
	case DualStateDBMode:
		ex, err = w.newExecutorWrapper()
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
	// Create the appropriate executor based on mode
	type caller interface {
		Call(ethereum.CallMsg) ([]byte, error)
		Close() error
	}

	var ex caller
	var err error

	switch w.mode {
	case BalancePrimingMode:
		ex, err = w.newBalancePrimingExecutor()
	case DualStateDBMode:
		ex, err = w.newExecutorWrapper()
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
func (w *EVMEngineWrapper) newExecutorWrapper() (*ExecutorWrapper, error) {
	return NewExecutorWrapper(
		w.namespace,
		w.kvs,
		w.evmConfig,
		w.monotonicVersions,
		w.ethStateDB,
		w.blockContext,
	)
}

// newBalancePrimingExecutor creates an executor with balance priming support.
func (w *EVMEngineWrapper) newBalancePrimingExecutor() (*BalancePrimingExecutor, error) {
	return NewBalancePrimingExecutor(
		w.namespace,
		w.kvs,
		w.evmConfig,
		w.monotonicVersions,
		w.balancePriming,
		w.blockContext,
	)
}
