/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"regexp"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser"
	"github.com/hyperledger/fabric-x-evm/utils"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

func New(channel, namespace, nsVersion string, end *endorser.Endorser, ethChainConfig *params.ChainConfig) *EndorserAPI {
	return &EndorserAPI{
		channel:        channel,
		namespace:      namespace,
		nsVersion:      nsVersion,
		endorser:       end,
		ethChainConfig: ethChainConfig,
	}
}

type EndorserAPI struct {
	channel        string
	namespace      string
	nsVersion      string
	endorser       *endorser.Endorser
	ethChainConfig *params.ChainConfig
}

// ProcessEVMTransaction processes an Ethereum transaction and returns a signed proposal response
func (api *EndorserAPI) ProcessEVMTransaction(ctx context.Context, inv endorsement.Invocation, ethTx *types.Transaction, blockInfo *utils.BlockInfo) (*peer.ProposalResponse, error) {
	// Validate the ethereum transaction signature
	ethChainConfig := api.ethChainConfig
	if ethChainConfig == nil {
		ethChainConfig = common.ChainConfig
	}
	ethSigner := types.LatestSigner(ethChainConfig)
	if _, err := types.Sender(ethSigner, ethTx); err != nil {
		return nil, fmt.Errorf("invalid ethereum signature: %w", err)
	}

	// Execute the transaction
	res, err := api.endorser.ExecuteTransaction(ctx, inv, ethTx, blockInfo)
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
	return res, nil
}

// ProcessCall processes an Ethereum call (query) and returns a proposal response
func (api *EndorserAPI) ProcessCall(ctx context.Context, callMsg *ethereum.CallMsg, blockInfo *utils.BlockInfo) (*peer.ProposalResponse, error) {
	// Execute the call
	var blockNumber *big.Int
	if blockInfo != nil {
		blockNumber = blockInfo.BlockNumber
	}
	res, err := api.endorser.CallContract(ctx, callMsg, blockNumber)

	return response(res, err), nil
}

// ProcessStateQuery processes a state query and returns a proposal response
func (api *EndorserAPI) ProcessStateQuery(ctx context.Context, query common.StateQuery) (*peer.ProposalResponse, error) {
	// Execute the query
	res, err := api.endorser.GetState(ctx, query)
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
