/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

// Package api defines the endorser's published service contract: what a
// gateway (in this org or another) calls to get a proposal endorsed. It has
// no dependency on how the endorser is implemented or transported — today
// core.Endorser satisfies it in-process; a future gRPC client/server pair
// would implement it over the wire without changing this contract.
package api

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// Service processes proposals into a ProposalResponse whose Status carries the
// outcome (OK, revert, client error, server error). The error return is reserved
// for transport/delivery failures — over gRPC it is the call status; the
// in-process implementation always returns nil and reports faults via the Status.
// This allows different implementations (e.g., local, gRPC client, mock).
type Service interface {
	ProcessEVMTransaction(ctx context.Context, inv endorsement.Invocation, ethTx *types.Transaction) (*peer.ProposalResponse, error)
	ProcessCall(ctx context.Context, callMsg *ethereum.CallMsg, blockNumber *big.Int) (*peer.ProposalResponse, error)
	ProcessStateQuery(ctx context.Context, query common.StateQuery) (*peer.ProposalResponse, error)
}
