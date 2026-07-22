/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// Endorser implements the ProcessProposal API to simulate the execution of ethereum transaction
type Endorser struct {
	Engine  EVMEngineInterface // Exported to allow injection of wrappers
	builder endorsement.Builder
}

// EVMEngineInterface defines the interface for EVM execution engines.
// This allows both *EVMEngine and *testimpl.EVMEngineWrapper to be used.
type EVMEngineInterface interface {
	Execute(ctx context.Context, tx *types.Transaction) (endorsement.ExecutionResult, error)
	Call(msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
	BalanceAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) (*big.Int, error)
	StorageAt(ctx context.Context, account ethcommon.Address, key ethcommon.Hash, blockNumber *big.Int) ([]byte, error)
	CodeAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) ([]byte, error)
	NonceAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) (uint64, error)
}

// New returns a new Endorser.
//
// Arguments:
//   - `engine`:  Manages EVM execution and state reads.
//   - `builder`: Creates the signed ProposalResponse.
func New(engine *execution.EVMEngine, builder endorsement.Builder) (*Endorser, error) {
	return &Endorser{
		Engine:  engine,
		builder: builder,
	}, nil
}

// Execute endorses an Ethereum transaction and returns a signed proposal response.
// Reverts are endorsed and submitted (so the receipt records status=0); client-caused failures
// (invalid tx or failed execution) surface as a non-2xx status that CreateSignedTx won't submit.
func (f *Endorser) Execute(ctx context.Context, inv endorsement.Invocation, ethTx *types.Transaction) (*peer.ProposalResponse, error) {
	// Signature and nonce are validated inside the engine during execution.
	res, err := f.Engine.Execute(ctx, ethTx)
	if err != nil {
		return response(nil, err), nil
	}

	// Build and sign the endorsement. A signing failure is a server fault, so it
	// rides in the response (500) like every other outcome, not as a Go error.
	resp, err := f.builder.Endorse(inv, res)
	if err != nil {
		return response(nil, fmt.Errorf("endorse: %w", err)), nil
	}
	return resp, nil
}

// Call runs a read-only eth_call. A revert or failed execution comes back as a
// *common.CallError; on a revert the payload is returned alongside it.
func (f *Endorser) Call(ctx context.Context, msg *ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	res, err := f.Engine.Call(*msg, blockNumber)
	if err != nil {
		return res, &common.CallError{Status: classify(err), Message: err.Error(), Data: res}
	}
	return res, nil
}

func (f *Endorser) BalanceAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) (*big.Int, error) {
	return f.Engine.BalanceAt(ctx, account, blockNumber)
}

func (f *Endorser) StorageAt(ctx context.Context, account ethcommon.Address, key ethcommon.Hash, blockNumber *big.Int) ([]byte, error) {
	return f.Engine.StorageAt(ctx, account, key, blockNumber)
}

func (f *Endorser) CodeAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) ([]byte, error) {
	return f.Engine.CodeAt(ctx, account, blockNumber)
}

func (f *Endorser) NonceAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) (uint64, error) {
	return f.Engine.NonceAt(ctx, account, blockNumber)
}

// classify maps an engine error to a status code. A revert is a committed
// outcome; an *execution.ExecFailure is a valid tx whose EVM execution failed;
// an *execution.TxRejected is an invalid client tx; anything else is a
// server-side fault.
func classify(err error) int32 {
	if errors.Is(err, vm.ErrExecutionReverted) {
		return common.StatusEVMRevert
	}
	if _, ok := errors.AsType[*execution.ExecFailure](err); ok {
		return common.StatusExecFailure
	}
	if _, ok := errors.AsType[*execution.TxRejected](err); ok {
		return common.StatusTxRejected
	}
	return common.StatusServerError
}

func response(res []byte, err error) *peer.ProposalResponse {
	if err != nil {
		return &peer.ProposalResponse{
			Version: 1,
			Response: &peer.Response{
				Status:  classify(err),
				Message: err.Error(),
				Payload: res,
			},
		}
	}

	return &peer.ProposalResponse{
		Version: 1,
		Response: &peer.Response{
			Status:  common.StatusOK,
			Message: "OK",
			Payload: res,
		},
	}
}
