/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package endorser

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"regexp"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/utils"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

type Config struct {
	Channel   string
	Namespace string
	Peer      PeerConf
}
type PeerConf struct {
	Address string
	TLSPath string
}

// Endorser implements the ProcessProposal API to simulate the execution of ethereum transaction
type Endorser struct {
	Engine    EVMEngineInterface // Exported to allow injection of wrappers
	builder   endorsement.Builder
	ethSigner types.Signer
}

// EVMEngineInterface defines the interface for EVM execution engines.
// This allows both *EVMEngine and *testimpl.EVMEngineWrapper to be used.
type EVMEngineInterface interface {
	Execute(blockInfo *utils.BlockInfo, tx *types.Transaction) (endorsement.ExecutionResult, error)
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
//   - `chainID`: Ethereum chain ID used to validate transaction signatures.
func New(engine *EVMEngine, builder endorsement.Builder, chainID int64) (*Endorser, error) {
	return &Endorser{
		Engine:    engine,
		builder:   builder,
		ethSigner: types.LatestSignerForChainID(big.NewInt(chainID)),
	}, nil
}

// ProcessEVMTransaction processes an Ethereum transaction and returns a signed proposal response
func (f *Endorser) ProcessEVMTransaction(ctx context.Context, inv endorsement.Invocation, ethTx *types.Transaction, blockInfo *utils.BlockInfo) (*peer.ProposalResponse, error) {
	// Validate the ethereum transaction signature
	if _, err := types.Sender(f.ethSigner, ethTx); err != nil {
		return nil, fmt.Errorf("invalid ethereum signature: %w", err)
	}

	// Execute the transaction
	res, err := f.Engine.Execute(blockInfo, ethTx)
	if err != nil {
		// Distinguish between pre-execution validation errors and execution errors.
		// Pre-execution errors (from ApplyMessage) indicate the transaction is invalid
		// and should be rejected. Execution errors (from result.Err) indicate the
		// transaction executed but failed, and should be included in the response.
		if isPreExecutionError(err) {
			// Pre-execution validation error: reject the transaction
			return nil, err
		}
		// Execution error: include in response with error status
		return response(nil, err), nil
	}

	// Build and sign the endorsement
	return f.builder.Endorse(inv, res)
}

// ProcessCall processes an Ethereum call (query) and returns a proposal response
func (f *Endorser) ProcessCall(ctx context.Context, callMsg *ethereum.CallMsg, blockInfo *utils.BlockInfo) (*peer.ProposalResponse, error) {
	// Execute the call
	var blockNumber *big.Int
	if blockInfo != nil {
		blockNumber = blockInfo.BlockNumber
	}
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
		res = uint64ToBytes(nonce)
	default:
		return response(nil, fmt.Errorf("unknown state query %d", query.Type)), nil
	}

	return response(res, err), nil
}

func response(res []byte, err error) *peer.ProposalResponse {
	if err != nil {
		return &peer.ProposalResponse{
			Version: 1,
			Response: &peer.Response{
				Status:  500,
				Message: err.Error(),
			},
		}
	}

	return &peer.ProposalResponse{
		Version: 1,
		Response: &peer.Response{
			Status:  200,
			Message: "OK",
			Payload: res,
		},
	}
}

// isPreExecutionError checks if an error is a pre-execution validation error
// that should reject the transaction, as opposed to an execution error that
// should be included in the transaction result.
//
// According to go-ethereum's ApplyMessage documentation:
// "An error always indicates a core error meaning that the message would always
// fail for that particular state and would never be accepted within a block."
//
// Pre-execution errors include:
// - Nonce errors (too low, too high)
// - Insufficient funds
// - Gas limit errors
// - Init code size exceeded (EIP-3860)
//
// Execution errors (NOT pre-execution) include:
// - Out of gas during execution
// - Execution reverted
// - Invalid opcode
func isPreExecutionError(err error) bool {
	// Check for pre-execution validation errors from core package
	if errors.Is(err, core.ErrNonceTooLow) ||
		errors.Is(err, core.ErrNonceTooHigh) ||
		errors.Is(err, core.ErrNonceMax) ||
		errors.Is(err, core.ErrGasLimitReached) ||
		errors.Is(err, core.ErrInsufficientFundsForTransfer) ||
		errors.Is(err, core.ErrMaxInitCodeSizeExceeded) ||
		errors.Is(err, core.ErrInsufficientFunds) ||
		errors.Is(err, core.ErrGasUintOverflow) ||
		errors.Is(err, core.ErrIntrinsicGas) ||
		errors.Is(err, core.ErrTxTypeNotSupported) ||
		errors.Is(err, core.ErrTipAboveFeeCap) ||
		errors.Is(err, core.ErrTipVeryHigh) ||
		errors.Is(err, core.ErrFeeCapVeryHigh) ||
		errors.Is(err, core.ErrFeeCapTooLow) ||
		errors.Is(err, core.ErrSenderNoEOA) ||
		errors.Is(err, core.ErrBlobFeeCapTooLow) ||
		errors.Is(err, core.ErrMissingBlobHashes) ||
		errors.Is(err, core.ErrTooManyBlobs) ||
		errors.Is(err, core.ErrBlobTxCreate) {
		return true
	}

	// Check for blob validation errors that are created with fmt.Errorf
	// Pattern: "blob <number> has invalid hash version"
	errMsg := err.Error()
	matched, _ := regexp.MatchString(`^blob \d+ has invalid hash version$`, errMsg)
	return matched
}
