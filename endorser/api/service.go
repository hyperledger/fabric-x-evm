/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

// Package api defines the endorser's published service contract: what a
// gateway (in this org or another) calls to get a transaction endorsed or to
// read state. It has no dependency on how the endorser is implemented or
// transported — today core.Endorser satisfies it in-process; a gRPC
// client/server pair implements it over the wire without changing this
// contract.
package api

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// BlockEnv is the EVM block context the gateway picks once per transaction.
type BlockEnv struct {
	// Number is EVM block.number: the gateway's ledger height + 1.
	Number uint64
	// Time is EVM block.timestamp (wall time, truncated to the Unix second).
	Time time.Time
}

// Service is one typed method per endorser function, mirroring the EVM engine.
// Execute is the only method that produces an endorsement; the read-only
// methods return plain values.
//
// The error return is reserved for transport/delivery failures — over gRPC it
// is the call status. Application outcomes travel in the return value: a
// signed response for Execute, a *common.CallError for Call.
type Service interface {
	// Execute endorses an Ethereum transaction. The signed response carries the
	// outcome in its Status (OK, revert, rejected, exec failure, server error).
	//
	// env is the gateway-supplied EVM block context. Every endorser must receive
	// the same value so RWsets match; each validates it against its own clock
	// and ledger height.
	//
	// It returns a *peer.ProposalResponse because the gateway packages it into
	// an sdk.Endorsement, and both SDK packagers require one. Dropping it
	// depends on the fabricx.TxPackager change tracked for the client PR.
	Execute(ctx context.Context, inv endorsement.Invocation, ethTx *types.Transaction, env BlockEnv) (*peer.ProposalResponse, error)

	// Call runs a read-only eth_call. On an EVM revert or a failed execution it
	// returns a *common.CallError; the revert payload is returned alongside it.
	// maxUsedGas is the EVM gas the simulation needed before EIP-3529 refunds
	// are credited; callers that only need return data (eth_call) may ignore it.
	Call(ctx context.Context, msg *ethereum.CallMsg, blockNumber *big.Int) (ret []byte, maxUsedGas uint64, err error)

	BalanceAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) (*big.Int, error)
	StorageAt(ctx context.Context, account ethcommon.Address, key ethcommon.Hash, blockNumber *big.Int) ([]byte, error)
	CodeAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) ([]byte, error)
	NonceAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) (uint64, error)
}
