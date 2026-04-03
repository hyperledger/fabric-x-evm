/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/params"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser"
	"github.com/hyperledger/fabric-x-evm/utils"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

func New(channel, namespace, nsVersion string, des IdentityDeserializer, end *endorser.Endorser, ethChainConfig *params.ChainConfig) *EndorserAPI {
	return &EndorserAPI{
		channel:        channel,
		namespace:      namespace,
		nsVersion:      nsVersion,
		endorser:       end,
		des:            des,
		ethChainConfig: ethChainConfig,
	}
}

type EndorserAPI struct {
	channel        string
	namespace      string
	nsVersion      string
	des            IdentityDeserializer
	endorser       *endorser.Endorser
	ethChainConfig *params.ChainConfig
}

// ProcessProposal receives a signed proposal, processes it and outputs a proposal response
// note: this is the same signature of the method exposed by the endorser
func (api *EndorserAPI) ProcessProposal(ctx context.Context, signedProp *peer.SignedProposal, blockInfo *utils.BlockInfo) (*peer.ProposalResponse, error) {
	inv, err := endorsement.Parse(signedProp, time.Now())
	if err != nil {
		return nil, err
	}
	if err := api.validateChannelAndNamespace(inv); err != nil {
		return nil, err
	}
	if len(inv.Args) < 2 {
		return nil, errors.New("fcn and arg required")
	}

	switch common.ProposalType(inv.Args[0][0]) {
	// EVM Transaction
	case common.ProposalTypeEVMTx:
		if len(inv.Args[1]) == 0 {
			return nil, errors.New("tx is required")
		}

		ethTx, err := common.ValidateEthTx(inv.Args[1], api.ethChainConfig)
		if err != nil {
			return nil, err
		}
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

	// Call (query only)
	case common.ProposalTypeCall:
		if len(inv.Args[1]) == 0 {
			return nil, errors.New("callMsg is required")
		}
		callMsg := &ethereum.CallMsg{}
		if err := json.Unmarshal(inv.Args[1], callMsg); err != nil {
			return nil, err
		}
		var blockNumber *big.Int
		if blockInfo != nil {
			blockNumber = blockInfo.BlockNumber
		}
		res, err := api.endorser.CallContract(ctx, callMsg, blockNumber)

		return response(res, err), nil

	// Get state from the ledger
	case common.ProposalTypeState:
		if len(inv.Args[1]) == 0 {
			return nil, errors.New("callMsg is required")
		}
		query := common.StateQuery{}
		if err := json.Unmarshal(inv.Args[1], &query); err != nil {
			return nil, err
		}
		res, err := api.endorser.GetState(ctx, query)
		return response(res, err), nil
	}
	return nil, errors.New("unknown transaction type")
}

func (api *EndorserAPI) validateChannelAndNamespace(inv endorsement.Invocation) error {
	if inv.CCID.Name != api.namespace {
		return fmt.Errorf("namespace mismatch, expected %s, got %s", api.namespace, inv.CCID.Name)
	}
	if inv.CCID.Version != api.nsVersion {
		return fmt.Errorf("namespace version mismatch, expected %s, got %s", api.nsVersion, inv.CCID.Version)
	}
	if inv.Channel != api.channel {
		return fmt.Errorf("channel mismatch, expected %s, got %s", api.channel, inv.Channel)
	}
	return nil
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
