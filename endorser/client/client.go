/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

// Package client is an api.Service backed by a remote endorser over gRPC.
package client

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"google.golang.org/grpc"

	"github.com/hyperledger/fabric-x-evm/api/endorsementpb"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// Client adapts a remote EvmEndorsement gRPC endpoint to the api.Service seam.
type Client struct {
	rpc  endorsementpb.EvmEndorsementClient
	conn *grpc.ClientConn
}

// New returns a Client that talks to the endorser over conn.
func New(conn *grpc.ClientConn) *Client {
	return &Client{rpc: endorsementpb.NewEvmEndorsementClient(conn), conn: conn}
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Execute endorses an Ethereum transaction. A gRPC error is a transport fault;
// application outcomes ride in the response status.
func (c *Client) Execute(ctx context.Context, inv endorsement.Invocation, ethTx *types.Transaction) (*peer.ProposalResponse, error) {
	raw, err := ethTx.MarshalBinary()
	if err != nil {
		return nil, err
	}
	resp, err := c.rpc.Execute(ctx, &endorsementpb.ExecuteRequest{
		EthereumTx:   raw,
		ProposalHash: inv.ProposalHash,
		Invocation:   invocationMsg(inv),
	})
	if err != nil {
		return nil, err
	}
	return proposalResponse(resp), nil
}

// Call runs a read-only eth_call. A non-OK status is returned as a
// *common.CallError; only transport faults surface as a gRPC error.
func (c *Client) Call(ctx context.Context, msg *ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	resp, err := c.rpc.Call(ctx, callRequest(msg, blockNumber))
	if err != nil {
		return nil, err
	}
	if resp.GetStatus() != common.StatusOK {
		return resp.GetReturnData(), &common.CallError{Status: resp.GetStatus(), Message: resp.GetMessage(), Data: resp.GetReturnData()}
	}
	return resp.GetReturnData(), nil
}

func (c *Client) BalanceAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) (*big.Int, error) {
	resp, err := c.rpc.BalanceAt(ctx, &endorsementpb.BalanceRequest{Account: account.Bytes(), BlockNumber: blockNumberProto(blockNumber)})
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(resp.GetBalance()), nil
}

func (c *Client) StorageAt(ctx context.Context, account ethcommon.Address, key ethcommon.Hash, blockNumber *big.Int) ([]byte, error) {
	resp, err := c.rpc.StorageAt(ctx, &endorsementpb.StorageRequest{Account: account.Bytes(), Key: key.Bytes(), BlockNumber: blockNumberProto(blockNumber)})
	if err != nil {
		return nil, err
	}
	return resp.GetValue(), nil
}

func (c *Client) CodeAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) ([]byte, error) {
	resp, err := c.rpc.CodeAt(ctx, &endorsementpb.CodeRequest{Account: account.Bytes(), BlockNumber: blockNumberProto(blockNumber)})
	if err != nil {
		return nil, err
	}
	return resp.GetCode(), nil
}

func (c *Client) NonceAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) (uint64, error) {
	resp, err := c.rpc.NonceAt(ctx, &endorsementpb.NonceRequest{Account: account.Bytes(), BlockNumber: blockNumberProto(blockNumber)})
	if err != nil {
		return 0, err
	}
	return resp.GetNonce(), nil
}

// invocationMsg maps the endorsement invocation onto its wire message.
func invocationMsg(inv endorsement.Invocation) *endorsementpb.Invocation {
	msg := &endorsementpb.Invocation{TxId: inv.TxID, Args: inv.Args}
	if inv.CCID != nil {
		msg.ChaincodeName = inv.CCID.Name
		msg.ChaincodeVersion = inv.CCID.Version
	}
	return msg
}

// proposalResponse maps the wire response onto the ProposalResponse the gateway
// packages. The top-level Payload is the signed endorsed result the tx packager
// assembles the transaction from; Response.Payload is the EVM return data.
func proposalResponse(resp *endorsementpb.ExecuteResponse) *peer.ProposalResponse {
	return &peer.ProposalResponse{
		Payload:     resp.GetReadWriteSet(),
		Response:    &peer.Response{Status: resp.GetStatus(), Message: resp.GetMessage(), Payload: resp.GetPayload()},
		Endorsement: &peer.Endorsement{Endorser: resp.GetEndorserId(), Signature: resp.GetSignature()},
	}
}

// callRequest builds a CallRequest from the call message. Empty from/to are left
// unset; gas price and value are big-endian integer bytes.
func callRequest(msg *ethereum.CallMsg, blockNumber *big.Int) *endorsementpb.CallRequest {
	req := &endorsementpb.CallRequest{Gas: msg.Gas, Data: msg.Data, BlockNumber: blockNumberProto(blockNumber)}
	if msg.From != (ethcommon.Address{}) {
		req.From = msg.From.Bytes()
	}
	if msg.To != nil {
		req.To = msg.To.Bytes()
	}
	if msg.GasPrice != nil {
		req.GasPrice = msg.GasPrice.Bytes()
	}
	if msg.Value != nil {
		req.Value = msg.Value.Bytes()
	}
	return req
}

// blockNumberProto turns the block selector into the optional wire field. A nil
// selector, or a negative (unresolved block-tag sentinel) value, means latest.
func blockNumberProto(bn *big.Int) *uint64 {
	if bn == nil || bn.Sign() < 0 {
		return nil
	}
	v := bn.Uint64()
	return &v
}
