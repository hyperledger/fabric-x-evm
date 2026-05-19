/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	fxcommon "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser"
	"github.com/hyperledger/fabric-x-evm/utils"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// BalancePrimingConfig configures automatic ERC-20 balance priming.
type BalancePrimingConfig struct {
	// Enabled turns on balance priming
	Enabled bool
	// ContractAddress is the ERC-20 contract address to prime
	ContractAddress common.Address
	// MappingPosition is the storage slot position of the balances mapping
	MappingPosition uint64
}

// SenderAware is an interface for StateDB wrappers that need to know the transaction sender.
// This allows wrappers to perform sender-specific optimizations (e.g., balance priming).
type SenderAware interface {
	SetSender(addr common.Address)
}

// NonceAware is an interface for StateDB wrappers that need to know the
// expected transaction nonce. This allows wrappers to intercept nonce reads
// and return a synthetic value, bypassing ledger nonce validation.
// Used by the performance demo to make wrap-around replay seamless.
type NonceAware interface {
	SetExpectedNonce(nonce uint64)
}

// BalancePrimingExecutor wraps an Executor and adds balance priming support for testing.
type BalancePrimingExecutor struct {
	*endorser.Executor
	state endorser.ExtendedStateDB
}

// NewBalancePrimingExecutor creates a new executor with balance priming support.
func NewBalancePrimingExecutor(
	namespace string,
	kvs endorser.KVSSnapshotter,
	blockInfo *utils.BlockInfo,
	stateBlockNum uint64,
	evmConfig endorser.EVMConfig,
	monotonicVersions bool,
	balancePriming *BalancePrimingConfig,
) (*BalancePrimingExecutor, error) {
	// Begin a new reader to get snapshot isolation
	reader, err := kvs.NewSnapshot(0)
	if err != nil {
		return nil, err
	}

	// Create StateDB with the reader
	stateDB, err := endorser.NewStateDB(context.TODO(), reader, namespace, stateBlockNum, monotonicVersions)
	if err != nil {
		reader.Close()
		return nil, err
	}

	// Wrap with balance priming wrapper if configured
	var finalStateDB endorser.ExtendedStateDB = stateDB
	if balancePriming != nil && balancePriming.Enabled {
		finalStateDB = NewBalancePrimingWrapper(
			stateDB,
			balancePriming.ContractAddress,
			balancePriming.MappingPosition,
		)
	}

	// Create the base executor using the public API
	executor, err := endorser.NewExecutor(finalStateDB, reader, blockInfo, evmConfig)
	if err != nil {
		reader.Close()
		return nil, err
	}

	return &BalancePrimingExecutor{
		Executor: executor,
		state:    finalStateDB,
	}, nil
}

// Execute runs a state-changing transaction with SenderAware notification.
func (e *BalancePrimingExecutor) Execute(tx *types.Transaction) (endorsement.ExecutionResult, error) {
	// Notify NonceAware wrappers of the expected nonce for this transaction.
	if na, ok := e.state.(NonceAware); ok {
		na.SetExpectedNonce(tx.Nonce())
	}

	// Notify SenderAware wrappers of the actual transaction sender so that
	// balance priming targets the correct storage slot.
	// e.Executor.ChainCfg is the same chain config used by Executor.Send, so
	// the recovered address will match the one the EVM uses.
	if sa, ok := e.state.(SenderAware); ok {
		signer := types.LatestSignerForChainID(e.Executor.ChainCfg.ChainID)
		from, err := types.Sender(signer, tx)
		if err != nil {
			return endorsement.ExecutionResult{}, fmt.Errorf("recover sender: %w", err)
		}
		sa.SetSender(from)
	}

	// Execute the transaction using the base Executor
	ret, err := e.Executor.Send(tx)
	if err != nil && !errors.Is(err, vm.ErrExecutionReverted) {
		return endorsement.ExecutionResult{}, err
	}
	if errors.Is(err, vm.ErrExecutionReverted) {
		event, mErr := fxcommon.MarshalRevert(ret, "", tx.Hash().Hex())
		if mErr != nil {
			return endorsement.ExecutionResult{}, fmt.Errorf("marshal revert event: %w", mErr)
		}
		return endorsement.ExecutionResult{
			RWS:     e.state.Result(),
			Event:   event,
			Status:  201,
			Message: err.Error(),
			Payload: ret,
		}, nil
	}

	// Marshal logs if any
	var logs []byte
	if l := e.state.Logs(); len(l) > 0 {
		logs, err = json.Marshal(l)
		if err != nil {
			return endorsement.ExecutionResult{}, errors.New("error marshaling logs")
		}
	}

	return endorsement.Success(e.state.Result(), logs, ret), nil
}
