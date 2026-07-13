/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"encoding/binary"
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

// ProcessEVMTransaction processes an Ethereum transaction and returns a signed proposal response.
// Reverts are endorsed and submitted (so the receipt records status=0); client-caused failures
// (invalid tx or failed execution) surface as a non-2xx status that CreateSignedTx won't submit.
func (f *Endorser) ProcessEVMTransaction(ctx context.Context, inv endorsement.Invocation, ethTx *types.Transaction) (*peer.ProposalResponse, error) {
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

// ProcessCall processes an Ethereum call (query) and returns a proposal response
func (f *Endorser) ProcessCall(ctx context.Context, callMsg *ethereum.CallMsg, blockNumber *big.Int) (*peer.ProposalResponse, error) {
	res, err := f.Engine.Call(*callMsg, blockNumber)
	return response(res, err), nil
}

// ProcessStateQuery processes a state query and returns a proposal response
func (f *Endorser) ProcessStateQuery(ctx context.Context, query common.StateQuery) (*peer.ProposalResponse, error) {
	// Execute the query based on query type
	var res []byte
	var err error

	switch query.Type {
	case common.QueryTypeBalance:
		bal, balErr := f.Engine.BalanceAt(ctx, query.Account, query.BlockNumber)
		if balErr != nil {
			return response(nil, balErr), nil
		}
		res = bal.Bytes()
	case common.QueryTypeCode:
		res, err = f.Engine.CodeAt(ctx, query.Account, query.BlockNumber)
	case common.QueryTypeStorage:
		res, err = f.Engine.StorageAt(ctx, query.Account, query.Key, query.BlockNumber)
	case common.QueryTypeNonce:
		nonce, nonceErr := f.Engine.NonceAt(ctx, query.Account, query.BlockNumber)
		if nonceErr != nil {
			return response(nil, nonceErr), nil
		}
		res = make([]byte, 8)
		binary.BigEndian.PutUint64(res, nonce)
	default:
		return response(nil, fmt.Errorf("unknown state query %d", query.Type)), nil
	}

	return response(res, err), nil
}

func response(res []byte, err error) *peer.ProposalResponse {
	if err != nil {
		// A revert is a committed outcome; an *execution.ExecFailure is a valid tx
		// whose EVM execution failed; an *execution.TxRejected is an invalid client
		// tx; anything else is a server-side fault.
		status := common.StatusServerError
		if errors.Is(err, vm.ErrExecutionReverted) {
			status = common.StatusEVMRevert
		} else if _, ok := errors.AsType[*execution.ExecFailure](err); ok {
			status = common.StatusExecFailure
		} else if _, ok := errors.AsType[*execution.TxRejected](err); ok {
			status = common.StatusTxRejected
		}
		return &peer.ProposalResponse{
			Version: 1,
			Response: &peer.Response{
				Status:  status,
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
