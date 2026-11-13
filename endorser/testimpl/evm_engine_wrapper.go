/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// EVMEngineWrapper wraps an EVMEngine so that it executes with a
// BalancePrimingExecutor, for ERC-20 balance priming in replay tests.
type EVMEngineWrapper struct {
	*execution.EVMEngine
	balancePriming    *BalancePrimingConfig
	namespace         string
	kvs               execution.KVSSnapshotter
	evmConfig         execution.EVMConfig
	monotonicVersions bool
}

// NewEVMEngineWrapper creates a new EVMEngineWrapper. It primes no balances
// until SetBalancePriming configures it.
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
		namespace:         namespace,
		kvs:               kvs,
		evmConfig:         evmConfig,
		monotonicVersions: monotonicVersions,
	}
}

// SetBalancePriming configures which balances the wrapper primes.
func (w *EVMEngineWrapper) SetBalancePriming(config *BalancePrimingConfig) {
	w.balancePriming = config
}

// Execute runs a state-changing transaction and returns the EVM result.
// blockTime is the Unix second for EVM block.timestamp.
func (w *EVMEngineWrapper) Execute(ctx context.Context, tx *types.Transaction, blockTime uint64) (endorsement.ExecutionResult, error) {
	ex, err := w.newBalancePrimingExecutor()
	if err != nil {
		return endorsement.ExecutionResult{}, err
	}
	defer ex.Close()

	if blockTime != 0 {
		ex.BlockCtx.Time = blockTime
	}
	return ex.Execute(tx)
}

// Call executes a read-only call against the state.
// maxUsedGas is the EVM gas the simulation needed before EIP-3529 refunds are
// credited.
func (w *EVMEngineWrapper) Call(msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, uint64, error) {
	ex, err := w.newBalancePrimingExecutor()
	if err != nil {
		return nil, 0, err
	}
	defer ex.Close()

	return ex.Call(msg)
}

// newBalancePrimingExecutor creates an executor with balance priming support.
func (w *EVMEngineWrapper) newBalancePrimingExecutor() (*BalancePrimingExecutor, error) {
	return NewBalancePrimingExecutor(
		w.namespace,
		w.kvs,
		w.evmConfig,
		w.monotonicVersions,
		w.balancePriming,
		nil,
	)
}
