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
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"

	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/api"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// EndorsementClient forwards ethereum-style transactions and calls
// to the endorsers and returns their signed fabric-style responses.
type EndorsementClient struct {
	endorsers []api.Service
	signer    Signer
	channel   string
	namespace string
	nsVersion string
}

// NewEndorsementClient creates an EndorsementClient from api.Service instances.
// This allows using concrete endorsers, wrapped endorsers (e.g., from testimpl package), remote
// gRPC clients to other organizations' endorsers, or other implementations.
func NewEndorsementClient(endorsers []api.Service, signer Signer, channel, namespace, nsVersion string) (*EndorsementClient, error) {
	return &EndorsementClient{
		endorsers: endorsers,
		signer:    signer,
		channel:   channel,
		namespace: namespace,
		nsVersion: nsVersion,
	}, nil
}

func (e EndorsementClient) ExecuteTransaction(ctx context.Context, tx *types.Transaction) (sdk.Endorsement, error) {
	// Marshal the transaction for the invocation args
	ethTxBytes, err := tx.MarshalBinary()
	if err != nil {
		return sdk.Endorsement{}, err
	}

	// Create invocation
	inv, err := e.createInvocation([][]byte{{byte(common.ProposalTypeEVMTx)}, ethTxBytes})
	if err != nil {
		return sdk.Endorsement{}, err
	}

	// Single timestamp for all endorsers so RWsets match.
	reqTime := time.Now()

	// Derive a cancellable context so goroutines can stop early on error
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	res := make([]*peer.ProposalResponse, len(e.endorsers))
	errs := make([]error, len(e.endorsers)) // indexed — deterministic error order

	for i, end := range e.endorsers {
		processEndorsement := func(index int, endorser api.Service) {
			pResp, err := endorser.Execute(ctx, inv, tx, reqTime)
			if err != nil {
				// A Go error is a transport/delivery failure (e.g. gRPC), not a tx outcome.
				errs[index] = fmt.Errorf("call endorser: %w", err)
				cancel() // signal other goroutines to stop early
				return
			}
			// Application outcomes ride in the status. A success and a revert are
			// committed; a valid tx whose execution failed is endorsable here but
			// not yet committed (a follow-up will mine it). An invalid (rejected) tx
			// or a server fault is an error the caller must see.
			switch pResp.Response.Status {
			case common.StatusOK, common.StatusEVMRevert, common.StatusExecFailure:
				res[index] = pResp
			default:
				errs[index] = fmt.Errorf("process EVM transaction: %s", pResp.Response.Message)
				cancel()
			}
		}

		if len(e.endorsers) > 1 {
			wg.Add(1)
			go func(index int, endorser api.Service) {
				defer wg.Done()
				processEndorsement(index, endorser)
			}(i, end)
		} else {
			processEndorsement(i, end)
		}
	}

	wg.Wait()

	// Return the first error in slice order, stable and deterministic, but skip
	// cancellations on the first pass: the endorser that reports one was most
	// likely aborted by the cancel above, and returning that would hide the
	// endorsement error which caused it. A genuinely cancelled caller has
	// nothing but cancellations, and the second pass returns one of those.
	for _, err := range errs {
		if err != nil && !errors.Is(err, context.Canceled) {
			return sdk.Endorsement{}, err
		}
	}
	for _, err := range errs {
		if err != nil {
			return sdk.Endorsement{}, err
		}
	}

	return sdk.Endorsement{
		Proposal:  inv.Proposal,
		Responses: res,
	}, nil
}

// CallContract queries a smart contract and returns the value.
// An EVM revert from the endorser is surfaced as *domain.RevertError so the API
// layer can map it to JSON-RPC -32000.
func (e *EndorsementClient) CallContract(ctx context.Context, args ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	payload, _, err := e.call(ctx, args, blockNumber)
	return payload, err
}

// estimateGasCeiling bounds the search. The endorser never charges for gas,
// so there's no cost to simulating generously.
const estimateGasCeiling = uint64(10_000_000)

// EstimateGas returns a gas limit verified to work if resubmitted, not a raw
// usedGas value: EVM rules like EIP-2200's SSTORE sentry and EIP-150's
// 63/64 call-forwarding need gas in reserve during execution, not just
// enough in total, so exact usedGas can sometimes still run out (view calls
// and other simple cases usually don't hit this). Since gas isn't charged,
// we generously double on each failure, until we hit the ceiling or succeed.
func (e *EndorsementClient) EstimateGas(ctx context.Context, args ethereum.CallMsg, blockNumber *big.Int) (uint64, error) {
	call := args

	probeGas := func(gas uint64) (usedGas uint64, ok bool, err error) {
		call.Gas = gas
		_, usedGas, err = e.endorsers[0].Call(ctx, &call, blockNumber)
		if err == nil {
			return usedGas, true, nil
		}
		callErr, isCallErr := errors.AsType[*common.CallError](err)
		if !isCallErr {
			return 0, false, fmt.Errorf("process call: %w", err)
		}
		if callErr.Reverted() && len(callErr.Data) > 0 {
			// A revert with a reason: a real business-logic outcome that no
			// amount of extra gas fixes, so stop instead of escalating.
			return 0, false, &domain.RevertError{Reason: callErr.Message, Data: callErr.Data}
		}
		// Out of gas, or an empty-data revert -- indistinguishable here from a
		// delegatecall (e.g. through an EIP-1167 minimal proxy) that ran out of
		// the gas forwarded to it. Either way, treat it as "not enough gas yet".
		return usedGas, false, nil
	}

	usedGas, ok, err := probeGas(estimateGasCeiling)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("gas required exceeds allowance (%d)", estimateGasCeiling)
	}

	for guess := usedGas; guess < estimateGasCeiling; guess *= 2 {
		if _, ok, err := probeGas(guess); err != nil {
			return 0, err
		} else if ok {
			return guess, nil
		}
	}
	return estimateGasCeiling, nil // already verified above
}

// call is CallContract's query path.
func (e *EndorsementClient) call(ctx context.Context, args ethereum.CallMsg, blockNumber *big.Int) ([]byte, uint64, error) {
	payload, gas, err := e.endorsers[0].Call(ctx, &args, blockNumber)
	if err == nil {
		return payload, gas, nil
	}

	callErr, ok := errors.AsType[*common.CallError](err)
	if !ok {
		// Not an application outcome: a transport/delivery failure.
		return nil, gas, fmt.Errorf("process call: %w", err)
	}
	if callErr.Reverted() {
		if len(callErr.Data) == 0 {
			// Empty revert on a call is not an error: many Ethereum tools probe
			// contracts this way. (EstimateGas does not apply this -- see its
			// probeGas -- since it can't tell that apart from a delegatecall
			// running out of the gas it was given.)
			return nil, gas, nil
		}
		return nil, gas, &domain.RevertError{Reason: callErr.Message, Data: callErr.Data}
	}
	// For a call, both a failed execution and a rejected tx are surfaced as an
	// execution error (-32000); only the reverted case carries data.
	if callErr.Status == common.StatusExecFailure || callErr.Status == common.StatusTxRejected {
		return nil, gas, &domain.ExecutionError{Message: callErr.Message}
	}
	return nil, gas, fmt.Errorf("query response was not successful, error code %d, msg %s", callErr.Status, callErr.Message)
}

// BalanceAt returns an account's balance at the given block.
func (e *EndorsementClient) BalanceAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) (*big.Int, error) {
	return e.endorsers[0].BalanceAt(ctx, account, blockNumber)
}

// StorageAt returns the storage word at key for an account.
func (e *EndorsementClient) StorageAt(ctx context.Context, account ethcommon.Address, key ethcommon.Hash, blockNumber *big.Int) ([]byte, error) {
	return e.endorsers[0].StorageAt(ctx, account, key, blockNumber)
}

// CodeAt returns an account's contract code.
func (e *EndorsementClient) CodeAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) ([]byte, error) {
	return e.endorsers[0].CodeAt(ctx, account, blockNumber)
}

// NonceAt returns an account's nonce.
func (e *EndorsementClient) NonceAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) (uint64, error) {
	return e.endorsers[0].NonceAt(ctx, account, blockNumber)
}

// createInvocation creates an endorsement.Invocation from the given parameters
func (e *EndorsementClient) createInvocation(args [][]byte) (endorsement.Invocation, error) {
	return endorsement.NewInvocation(e.signer, e.channel, e.namespace, e.nsVersion, args)
}
