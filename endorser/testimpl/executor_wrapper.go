/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"context"
	"encoding/json"
	"errors"

	ethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-evm/endorser"
	"github.com/hyperledger/fabric-x-evm/utils"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// ExecutorWrapper wraps an Executor and adds DualStateDB support for testing.
type ExecutorWrapper struct {
	*endorser.Executor
	state      *endorser.DualStateDB
	ethStateDB *ethstate.StateDB
}

// NewExecutorWrapper creates a new executor wrapper with DualStateDB support.
func NewExecutorWrapper(
	namespace string,
	kvs endorser.KVSSnapshotter,
	blockInfo *utils.BlockInfo,
	stateBlockNum uint64,
	evmConfig endorser.EVMConfig,
	monotonicVersions bool,
	ethStateDB *ethstate.StateDB,
) (*ExecutorWrapper, error) {
	// Begin a new reader to get snapshot isolation
	reader := kvs.NewSnapshot()

	// Create StateDB with the reader
	stateDB, err := endorser.NewStateDB(context.TODO(), reader, namespace, stateBlockNum, monotonicVersions)
	if err != nil {
		reader.Close()
		return nil, err
	}

	// Create DualStateDB that wraps both the Fabric StateDB and the Ethereum StateDB
	dualStateDB := endorser.NewDualStateDB(ethStateDB, stateDB)

	// Create the executor using the public API with the DualStateDB
	executor, err := endorser.NewExecutor(dualStateDB, reader, blockInfo, evmConfig)
	if err != nil {
		reader.Close()
		return nil, err
	}

	return &ExecutorWrapper{
		Executor:   executor,
		state:      dualStateDB,
		ethStateDB: ethStateDB,
	}, nil
}

// Execute runs a state-changing transaction.
func (w *ExecutorWrapper) Execute(tx *types.Transaction) (endorsement.ExecutionResult, error) {
	ret, err := w.Executor.Send(tx)
	if err != nil {
		return endorsement.ExecutionResult{}, err
	}

	var logs []byte
	if l := w.state.Logs(); len(l) > 0 {
		logs, err = json.Marshal(l)
		if err != nil {
			return endorsement.ExecutionResult{}, errors.New("error marshaling logs")
		}
	}

	return endorsement.Success(w.state.Result(), logs, ret), nil
}
