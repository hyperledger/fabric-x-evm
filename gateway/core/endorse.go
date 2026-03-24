/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/utils"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/network"
	"github.com/hyperledger/fabric/protoutil"
)

// Endorser interface defines the contract for endorsement providers.
// This allows different implementations (e.g., local, gRPC client, mock).
type Endorser interface {
	ProcessProposal(ctx context.Context, signedProp *peer.SignedProposal, blockInfo *utils.BlockInfo) (*peer.ProposalResponse, error)
}

// EndorsementClient forwards ethereum-style transactions and calls
// to the endorsers and returns their signed fabric-style responses.
type EndorsementClient struct {
	endorsers      []Endorser
	signer         Signer
	channel        string
	namespace      string
	nsVersion      string
	ethChainConfig *params.ChainConfig
}

func NewEndorsementClient(endorsers []Endorser, signer Signer, channel, namespace, nsVersion string, ethChainConfig *params.ChainConfig) (*EndorsementClient, error) {
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
	// Generate a random fabric nonce (32 bytes)
	fabNonce := make([]byte, 32)
	if _, err := rand.Read(fabNonce); err != nil {
		return sdk.Endorsement{}, fmt.Errorf("failed to generate random nonce: %w", err)
	}

	ethTxBytes, err := tx.MarshalBinary()
	if err != nil {
		return sdk.Endorsement{}, err
	}

	prop, err := network.NewSignedProposal(e.signer, e.channel, e.namespace, e.nsVersion, [][]byte{{byte(common.ProposalTypeEVMTx)}, ethTxBytes}, fabNonce)
	if err != nil {
		return sdk.Endorsement{}, err
	}

	// TODO: request endorsement in parallel
	res := []*peer.ProposalResponse{}
	for _, end := range e.endorsers {
		pResp, err := end.ProcessProposal(ctx, prop, blockInfo)
		if err != nil {
			return sdk.Endorsement{}, fmt.Errorf("process proposal: %w", err)
		}
		res = append(res, pResp)
	}

	proposal, err := protoutil.UnmarshalProposal(prop.ProposalBytes)
	if err != nil {
		return sdk.Endorsement{}, err
	}

	return sdk.Endorsement{
		Proposal:  proposal,
		Responses: res,
	}, nil
}

// CallContract queries a smart contract and returns the value.
func (e *EndorsementClient) CallContract(ctx context.Context, args ethereum.CallMsg, blockInfo *utils.BlockInfo) ([]byte, error) {
	msg, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("invalid callmessage: %w", err)
	}

	prop, err := network.NewSignedProposal(e.signer, e.channel, e.namespace, e.nsVersion, [][]byte{{byte(common.ProposalTypeCall)}, msg}, nil)
	if err != nil {
		return nil, err
	}

	res, err := e.endorsers[0].ProcessProposal(context.TODO(), prop, blockInfo)
	if err != nil {
		return nil, fmt.Errorf("process proposal: %w", err)
	}
	if res.Response.Status < 200 || res.Response.Status >= 400 {
		return nil, fmt.Errorf("query response was not successful, error code %d, msg %s", res.Response.Status, res.Response.Message)
	}

	return res.Response.Payload, nil
}

// GetState returns ledger state.
func (e *EndorsementClient) GetState(ctx context.Context, query common.StateQuery) ([]byte, error) {
	msg, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("invalid query: %w", err)
	}

	prop, err := network.NewSignedProposal(e.signer, e.channel, e.namespace, e.nsVersion, [][]byte{{byte(common.ProposalTypeState)}, msg}, nil)
	if err != nil {
		return nil, err
	}

	res, err := e.endorsers[0].ProcessProposal(ctx, prop, nil)
	if err != nil {
		return nil, fmt.Errorf("process proposal: %w", err)
	}
	if res.Response.Status < 200 || res.Response.Status >= 400 {
		return nil, fmt.Errorf("query response was not successful, error code %d, msg %s", res.Response.Status, res.Response.Message)
	}

	return res.Response.Payload, nil
}
