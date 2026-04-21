/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	fabCommon "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/protoutil"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser"
	"github.com/hyperledger/fabric-x-evm/utils"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// Endorser interface defines the contract for endorsement providers.
// This allows different implementations (e.g., local, gRPC client, mock).
type Endorser interface {
	ProcessEVMTransaction(ctx context.Context, inv endorsement.Invocation, ethTx *types.Transaction, blockInfo *utils.BlockInfo) (*peer.ProposalResponse, error)
	ProcessCall(ctx context.Context, callMsg *ethereum.CallMsg, blockInfo *utils.BlockInfo) (*peer.ProposalResponse, error)
	ProcessStateQuery(ctx context.Context, query common.StateQuery) (*peer.ProposalResponse, error)
}

// EndorsementClient forwards ethereum-style transactions and calls
// to the endorsers and returns their signed fabric-style responses.
type EndorsementClient struct {
	endorsers      []*endorser.Endorser
	signer         Signer
	channel        string
	namespace      string
	nsVersion      string
	ethChainConfig *params.ChainConfig
}

func NewEndorsementClient(endorsers []*endorser.Endorser, signer Signer, channel, namespace, nsVersion string, ethChainConfig *params.ChainConfig) (*EndorsementClient, error) {
	return &EndorsementClient{
		endorsers:      endorsers,
		signer:         signer,
		channel:        channel,
		namespace:      namespace,
		nsVersion:      nsVersion,
		ethChainConfig: ethChainConfig,
	}, nil
}

func (e EndorsementClient) ExecuteTransaction(ctx context.Context, tx *types.Transaction, blockInfo *utils.BlockInfo) (sdk.Endorsement, error) {
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

	// TODO: request endorsement in parallel
	res := []*peer.ProposalResponse{}
	for _, end := range e.endorsers {
		pResp, err := end.ProcessEVMTransaction(ctx, inv, tx, blockInfo)
		if err != nil {
			return sdk.Endorsement{}, fmt.Errorf("process EVM transaction: %w", err)
		}
		res = append(res, pResp)
	}

	return sdk.Endorsement{
		Proposal:  inv.Proposal,
		Responses: res,
	}, nil
}

// CallContract queries a smart contract and returns the value.
func (e *EndorsementClient) CallContract(ctx context.Context, args ethereum.CallMsg, blockInfo *utils.BlockInfo) ([]byte, error) {
	res, err := e.endorsers[0].ProcessCall(ctx, &args, blockInfo)
	if err != nil {
		return nil, fmt.Errorf("process call: %w", err)
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
