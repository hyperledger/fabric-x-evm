/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package server

import (
	"context"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hyperledger/fabric-x-evm/api/endorsementpb"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// Execute endorses an Ethereum transaction. Application outcomes ride in the
// response status; only transport faults surface as a gRPC error.
func (s *Server) Execute(ctx context.Context, req *endorsementpb.ExecuteRequest) (*endorsementpb.ExecuteResponse, error) {
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(req.GetEthereumTx()); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "unmarshal tx: %v", err)
	}
	resp, err := s.svc.Execute(ctx, invocation(req), tx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "execute: %v", err)
	}
	return executeResponse(resp), nil
}

// invocation maps the sender's invocation onto the endorsement invocation the
// builder consumes.
func invocation(req *endorsementpb.ExecuteRequest) endorsement.Invocation {
	i := req.GetInvocation()
	return endorsement.Invocation{
		TxID:         i.GetTxId(),
		Args:         i.GetArgs(),
		CCID:         &peer.ChaincodeID{Name: i.GetChaincodeName(), Version: i.GetChaincodeVersion()},
		ProposalHash: req.GetProposalHash(),
	}
}

// Call runs a read-only eth_call. A revert or failed execution is carried in
// the response; only transport faults are a gRPC error.
func (s *Server) Call(ctx context.Context, req *endorsementpb.CallRequest) (*endorsementpb.CallResponse, error) {
	out, err := s.svc.Call(ctx, callMsg(req), blockNumber(req.BlockNumber))
	if err != nil {
		if ce, ok := errors.AsType[*common.CallError](err); ok {
			return &endorsementpb.CallResponse{ReturnData: ce.Data, Status: ce.Status, Message: ce.Message}, nil
		}
		return nil, status.Errorf(codes.Internal, "call: %v", err)
	}
	return &endorsementpb.CallResponse{ReturnData: out, Status: common.StatusOK}, nil
}

func (s *Server) BalanceAt(ctx context.Context, req *endorsementpb.BalanceRequest) (*endorsementpb.BalanceResponse, error) {
	bal, err := s.svc.BalanceAt(ctx, ethcommon.BytesToAddress(req.GetAccount()), blockNumber(req.BlockNumber))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "balance: %v", err)
	}
	return &endorsementpb.BalanceResponse{Balance: bal.Bytes()}, nil
}

func (s *Server) StorageAt(ctx context.Context, req *endorsementpb.StorageRequest) (*endorsementpb.StorageResponse, error) {
	val, err := s.svc.StorageAt(ctx, ethcommon.BytesToAddress(req.GetAccount()), ethcommon.BytesToHash(req.GetKey()), blockNumber(req.BlockNumber))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "storage: %v", err)
	}
	return &endorsementpb.StorageResponse{Value: val}, nil
}

func (s *Server) CodeAt(ctx context.Context, req *endorsementpb.CodeRequest) (*endorsementpb.CodeResponse, error) {
	code, err := s.svc.CodeAt(ctx, ethcommon.BytesToAddress(req.GetAccount()), blockNumber(req.BlockNumber))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "code: %v", err)
	}
	return &endorsementpb.CodeResponse{Code: code}, nil
}

func (s *Server) NonceAt(ctx context.Context, req *endorsementpb.NonceRequest) (*endorsementpb.NonceResponse, error) {
	nonce, err := s.svc.NonceAt(ctx, ethcommon.BytesToAddress(req.GetAccount()), blockNumber(req.BlockNumber))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "nonce: %v", err)
	}
	return &endorsementpb.NonceResponse{Nonce: nonce}, nil
}

// blockNumber turns the optional block selector into the *big.Int the engine
// expects; a nil selector means latest.
func blockNumber(bn *uint64) *big.Int {
	if bn == nil {
		return nil
	}
	return new(big.Int).SetUint64(*bn)
}

// callMsg builds an ethereum.CallMsg from the request fields. Empty from/to are
// left unset; gas price and value are big-endian integer bytes.
func callMsg(req *endorsementpb.CallRequest) *ethereum.CallMsg {
	msg := &ethereum.CallMsg{Gas: req.GetGas(), Data: req.GetData()}
	if from := req.GetFrom(); len(from) > 0 {
		msg.From = ethcommon.BytesToAddress(from)
	}
	if to := req.GetTo(); len(to) > 0 {
		addr := ethcommon.BytesToAddress(to)
		msg.To = &addr
	}
	if gp := req.GetGasPrice(); len(gp) > 0 {
		msg.GasPrice = new(big.Int).SetBytes(gp)
	}
	if v := req.GetValue(); len(v) > 0 {
		msg.Value = new(big.Int).SetBytes(v)
	}
	return msg
}

// executeResponse maps the endorser's ProposalResponse onto the wire response.
func executeResponse(pr *peer.ProposalResponse) *endorsementpb.ExecuteResponse {
	out := &endorsementpb.ExecuteResponse{}
	if r := pr.GetResponse(); r != nil {
		out.Status = r.Status
		out.Message = r.Message
		out.Payload = r.Payload
	}
	if e := pr.GetEndorsement(); e != nil {
		out.EndorserId = e.Endorser
		out.Signature = e.Signature
	}
	return out
}
