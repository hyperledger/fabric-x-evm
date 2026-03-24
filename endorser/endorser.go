/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package endorser

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/utils"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

type Config struct {
	Channel   string
	Namespace string
	NsVersion string
	Peer      PeerConf
}
type PeerConf struct {
	Address string
	TLSPath string
}

// Endorser implements the ProcessProposal API to simulate the execution of ethereum transaction
type Endorser struct {
	engine  *EVMEngine
	builder endorsement.Builder
}

// New returns a new Endorser.
//
// Arguments:
//   - `engine`:     Manages EVM execution and state reads.
//   - `builder`:    Creates the signed ProposalResponse.
func New(engine *EVMEngine, builder endorsement.Builder) (*Endorser, error) {
	return &Endorser{
		engine:  engine,
		builder: builder,
	}, nil
}

// ExecuteTransaction processes a transaction and returns a signed proposal response.
func (f *Endorser) ExecuteTransaction(ctx context.Context, inv endorsement.Invocation, ethTx *types.Transaction, blockInfo *utils.BlockInfo) (*peer.ProposalResponse, error) {
	res, err := f.engine.Execute(blockInfo, ethTx)
	if err != nil {
		return nil, err
	}

	return f.builder.Endorse(inv, res)
}

// CallContract executes a message call transaction, which is directly executed in the VM of the node.
// It is the equivalent of a fabric "query". BlockNumber selects the state snapshot (nil = latest).
func (f *Endorser) CallContract(_ context.Context, args *ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	return f.engine.Call(*args, blockNumber)
}

// GetState queries the ledger state.
func (f *Endorser) GetState(ctx context.Context, query common.StateQuery) ([]byte, error) {
	switch query.Type {
	case common.QueryTypeBalance:
		bal, err := f.engine.BalanceAt(ctx, query.Account, query.BlockNumber)
		if err != nil {
			return nil, err
		}
		return bal.Bytes(), nil
	case common.QueryTypeCode:
		return f.engine.CodeAt(ctx, query.Account, query.BlockNumber)
	case common.QueryTypeStorage:
		return f.engine.StorageAt(ctx, query.Account, query.Key, query.BlockNumber)
	case common.QueryTypeNonce:
		nonce, err := f.engine.NonceAt(ctx, query.Account, query.BlockNumber)
		if err != nil {
			return nil, err
		}
		return uint64ToBytes(nonce), nil
	}
	return nil, fmt.Errorf("unknown state query %d", query.Type)
}
