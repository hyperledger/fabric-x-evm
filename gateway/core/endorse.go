/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"

	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	fabCommon "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/protoutil"

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

	// Derive a cancellable context so goroutines can stop early on error
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	res := make([]*peer.ProposalResponse, len(e.endorsers))
	errs := make([]error, len(e.endorsers)) // indexed — deterministic error order

	for i, end := range e.endorsers {
		processEndorsement := func(index int, endorser api.Service) {
			pResp, err := endorser.ProcessEVMTransaction(ctx, inv, tx)
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

	// Return first error in slice order — stable and deterministic
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
// Status 201 from the endorser signals an EVM revert; surface as
// *domain.RevertError so the API layer can map to JSON-RPC -32000.
func (e *EndorsementClient) CallContract(ctx context.Context, args ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	res, err := e.endorsers[0].ProcessCall(ctx, &args, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("process call: %w", err)
	}
	if res.Response.Status == common.StatusEVMRevert {
		return nil, &domain.RevertError{Reason: res.Response.Message, Data: res.Response.Payload}
	}
	// For a call, both a failed execution and a rejected tx are surfaced as an
	// execution error (-32000); only the reverted case carries data.
	if res.Response.Status == common.StatusExecFailure || res.Response.Status == common.StatusTxRejected {
		return nil, &domain.ExecutionError{Message: res.Response.Message}
	}
	if res.Response.Status < 200 || res.Response.Status >= 400 {
		return nil, fmt.Errorf("query response was not successful, error code %d, msg %s", res.Response.Status, res.Response.Message)
	}

	return res.Response.Payload, nil
}

// GetState returns ledger state.
func (e *EndorsementClient) GetState(ctx context.Context, query common.StateQuery) ([]byte, error) {
	res, err := e.endorsers[0].ProcessStateQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("process state query: %w", err)
	}
	if res.Response.Status < 200 || res.Response.Status >= 400 {
		return nil, fmt.Errorf("query response was not successful, error code %d, msg %s", res.Response.Status, res.Response.Message)
	}

	return res.Response.Payload, nil
}

// createInvocation creates an endorsement.Invocation from the given parameters
func (e *EndorsementClient) createInvocation(args [][]byte) (endorsement.Invocation, error) {
	// Get the creator from the signer
	creator, err := e.signer.Serialize()
	if err != nil {
		return endorsement.Invocation{}, fmt.Errorf("failed to serialize creator: %w", err)
	}

	// Generate a random nonce
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return endorsement.Invocation{}, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Compute TxID from nonce and creator
	txID := protoutil.ComputeTxID(nonce, creator)

	proposal, _, err := protoutil.CreateChaincodeProposalWithTxIDNonceAndTransient(
		protoutil.ComputeTxID(nonce, creator),
		fabCommon.HeaderType_ENDORSER_TRANSACTION,
		e.channel,
		&peer.ChaincodeInvocationSpec{
			ChaincodeSpec: &peer.ChaincodeSpec{
				Type: peer.ChaincodeSpec_CAR, // FIXME: should we put some special value here?
				ChaincodeId: &peer.ChaincodeID{
					Name:    e.namespace,
					Version: e.nsVersion,
				},
				Input: &peer.ChaincodeInput{
					Args: args,
				},
			},
		},
		nonce,
		creator,
		nil,
	)
	if err != nil {
		return endorsement.Invocation{}, fmt.Errorf("failed to create the proposal: %w", err)
	}

	hdr, err := protoutil.UnmarshalHeader(proposal.Header)
	if err != nil {
		return endorsement.Invocation{}, fmt.Errorf("failed to deserialise header: %w", err)
	}

	proposalHash, err := protoutil.GetProposalHash1(hdr, proposal.Payload)
	if err != nil {
		return endorsement.Invocation{}, fmt.Errorf("failed to compute proposal hash: %w", err)
	}

	return endorsement.Invocation{
		TxID:         txID,
		Nonce:        nonce,
		Creator:      creator,
		Args:         args,
		CCID:         &peer.ChaincodeID{Name: e.namespace, Version: e.nsVersion},
		Channel:      e.channel,
		Proposal:     proposal,
		ProposalHash: proposalHash,
	}, nil
}
