/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"math/big"

	"github.com/ethereum/go-ethereum"
	ethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-evm/endorser"
	"github.com/hyperledger/fabric-x-evm/utils"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// EVMEngineWrapper wraps an EVMEngine and adds ethStateDB management for testing.
// This wrapper handles the machinery for tracking Ethereum state root evolution
// alongside Fabric state, which is useful for testing and validation purposes.
type EVMEngineWrapper struct {
	*endorser.EVMEngine
	ethStateDB        *ethstate.StateDB
	namespace         string
	kvs               endorser.KVSSnapshotter
	evmConfig         endorser.EVMConfig
	monotonicVersions bool
}

// NewEVMEngineWrapper creates a new EVMEngineWrapper that wraps the given EVMEngine.
// The wrapper needs access to the engine's configuration to create executors with DualStateDB.
func NewEVMEngineWrapper(
	namespace string,
	kvs endorser.KVSSnapshotter,
	evmConfig endorser.EVMConfig,
	monotonicVersions bool,
	engine *endorser.EVMEngine,
) *EVMEngineWrapper {
	return &EVMEngineWrapper{
		EVMEngine:         engine,
		ethStateDB:        nil,
		namespace:         namespace,
		kvs:               kvs,
		evmConfig:         evmConfig,
		monotonicVersions: monotonicVersions,
	}
}

// SetEthStateDB sets the ethStateDB for testing purposes.
func (w *EVMEngineWrapper) SetEthStateDB(ethStateDB *ethstate.StateDB) {
	w.ethStateDB = ethStateDB
}

// GetEthStateDB returns the ethStateDB used for testing.
func (w *EVMEngineWrapper) GetEthStateDB() *ethstate.StateDB {
	return w.ethStateDB
}

// Execute runs a state-changing transaction and returns the EVM result.
// If ethStateDB is set, it creates an executor wrapper that uses DualStateDB.
func (w *EVMEngineWrapper) Execute(blockInfo *utils.BlockInfo, tx *types.Transaction) (endorsement.ExecutionResult, error) {
	// Create an executor wrapper that will use DualStateDB
	ex, err := w.newExecutorWrapper(blockInfo, 0)
	if err != nil {
		return endorsement.ExecutionResult{}, err
	}
	defer ex.Close()

	return ex.Execute(tx)
}

// Call executes a read-only call against the state.
func (w *EVMEngineWrapper) Call(msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	// Create an executor wrapper that will use DualStateDB
	stateBlock := uint64(0)
	if blockNumber != nil {
		stateBlock = blockNumber.Uint64()
	}
	ex, err := w.newExecutorWrapper(nil, stateBlock)
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
