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
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/api"
	"github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

var logger = flogging.MustGetLogger("endorser.core")

// Endorser implements the ProcessProposal API to simulate the execution of ethereum transaction
type Endorser struct {
	Engine  EVMEngineInterface // Exported to allow injection of wrappers
	builder endorsement.Builder

	// Clock skew bounds for gateway-supplied request timestamps.
	// Set only at construction via New; there is no setter.
	maxFuture time.Duration
	maxPast   time.Duration
	// Window for gateway-supplied block numbers, relative to height + 1.
	maxAhead  uint64
	maxBehind uint64
	// now is time.Now in production; tests inject a fixed clock.
	now func() time.Time
}

// EVMEngineInterface defines the interface for EVM execution engines.
// This allows both *EVMEngine and *testimpl.EVMEngineWrapper to be used.
type EVMEngineInterface interface {
	// Execute runs a state-changing tx with the given EVM block.number and
	// block.timestamp (Unix second, must be non-zero; gateway always supplies it).
	Execute(ctx context.Context, tx *types.Transaction, blockNumber, blockTime uint64) (endorsement.ExecutionResult, error)
	// BlockNumber returns the latest committed height.
	BlockNumber(ctx context.Context) (uint64, error)
	// Call returns return data and the EVM gas the simulation needed before
	// EIP-3529 refunds are credited.
	Call(msg ethereum.CallMsg, blockNumber *big.Int) (ret []byte, maxUsedGas uint64, err error)
	BalanceAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) (*big.Int, error)
	StorageAt(ctx context.Context, account ethcommon.Address, key ethcommon.Hash, blockNumber *big.Int) ([]byte, error)
	CodeAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) ([]byte, error)
	NonceAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) (uint64, error)
}

// New returns a new Endorser. Timestamp skew and block number bounds are taken from cfg via the
// config package helpers (single source of truth) and applied at construction.
// A zero cfg uses the package defaults.
//
// Arguments:
//   - `engine`:  Manages EVM execution and state reads.
//   - `builder`: Creates the signed ProposalResponse.
//   - `cfg`:     Endorser config; only timestamp skew and block number fields are read here.
func New(engine *execution.EVMEngine, builder endorsement.Builder, cfg config.Endorser) (*Endorser, error) {
	return &Endorser{
		Engine:    engine,
		builder:   builder,
		maxFuture: cfg.TimestampFutureSkew(),
		maxPast:   cfg.TimestampPastSkew(),
		maxAhead:  cfg.BlockNumberAhead(),
		maxBehind: cfg.BlockNumberBehind(),
		now:       time.Now,
	}, nil
}

// Execute endorses an Ethereum transaction and returns a signed proposal response.
// Reverts are endorsed and submitted (so the receipt records status=0); client-caused failures
// (invalid tx or failed execution) surface as a non-2xx status that CreateSignedTx won't submit.
//
// env is the gateway-supplied EVM block.number and block.timestamp. Both are required,
// validated against the endorser's ledger height and clock skew window, and applied as-is
// (no clamping) so all endorsers share the same values.
func (f *Endorser) Execute(ctx context.Context, inv endorsement.Invocation, ethTx *types.Transaction, env api.BlockEnv) (*peer.ProposalResponse, error) {
	logger.Debugf("Endorser.Execute() called with tx=%s blockNumber=%d timestamp=%s", ethTx.Hash().Hex(), env.Number, env.Time.UTC().Format(time.RFC3339))
	if err := validateRequestTimestamp(env.Time, f.clock(), f.maxFuture, f.maxPast); err != nil {
		// Application outcome: invalid request from a misbehaving/skewed gateway.
		logger.Warnf("Endorser.Execute() tx=%s invalid timestamp: %v", ethTx.Hash().Hex(), err)
		return response(nil, execution.NewTxRejected(err)), nil
	}
	height, err := f.Engine.BlockNumber(ctx)
	if err != nil {
		return response(nil, fmt.Errorf("block number: %w", err)), nil
	}
	if err := validateRequestBlockNumber(env.Number, height, f.maxAhead, f.maxBehind); err != nil {
		logger.Warnf("Endorser.Execute() tx=%s invalid block number: %v", ethTx.Hash().Hex(), err)
		return response(nil, execution.NewTxRejected(err)), nil
	}

	blockTime := uint64(env.Time.Unix())
	// Signature and nonce are validated inside the engine during execution.
	res, err := f.Engine.Execute(ctx, ethTx, env.Number, blockTime)
	if err != nil {
		logger.Warnf("Endorser.Execute() tx=%s execution error: %v", ethTx.Hash().Hex(), err)
		return response(nil, err), nil
	}

	// Build and sign the endorsement. A signing failure is a server fault, so it
	// rides in the response (500) like every other outcome, not as a Go error.
	resp, err := f.builder.Endorse(inv, res)
	if err != nil {
		logger.Errorf("Endorser.Execute() tx=%s endorse signing failed: %v", ethTx.Hash().Hex(), err)
		return response(nil, fmt.Errorf("endorse: %w", err)), nil
	}
	logger.Debugf("Endorser.Execute() tx=%s endorsed successfully status=%d", ethTx.Hash().Hex(), resp.Response.Status)
	return resp, nil
}

func (f *Endorser) clock() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Now()
}

// validateRequestTimestamp checks that ts is within [now-maxPast, now+maxFuture]
// and is representable as a non-negative Unix second (EVM block.timestamp).
func validateRequestTimestamp(ts, now time.Time, maxFuture, maxPast time.Duration) error {
	if ts.IsZero() {
		return fmt.Errorf("request timestamp is required")
	}
	// Negative Unix seconds would wrap if cast to uint64 for the EVM context.
	if ts.Unix() < 0 {
		return fmt.Errorf("request timestamp must be on or after the Unix epoch")
	}
	if ts.After(now.Add(maxFuture)) {
		return fmt.Errorf("request timestamp %s is more than %s in the future", ts.UTC().Format(time.RFC3339), maxFuture)
	}
	if ts.Before(now.Add(-maxPast)) {
		return fmt.Errorf("request timestamp %s is more than %s in the past", ts.UTC().Format(time.RFC3339), maxPast)
	}
	return nil
}

// validateRequestBlockNumber checks that n is within [height+1-maxBehind, height+1+maxAhead].
func validateRequestBlockNumber(n, height, maxAhead, maxBehind uint64) error {
	next := height + 1
	switch {
	case n == 0:
		return fmt.Errorf("request block number is required")
	case n > next+maxAhead:
		return fmt.Errorf("request block number %d is more than %d blocks ahead of %d", n, maxAhead, next)
	case n+maxBehind < next:
		return fmt.Errorf("request block number %d is more than %d blocks behind %d", n, maxBehind, next)
	}
	return nil
}

// Call runs a read-only eth_call. A revert or failed execution comes back as a
// *common.CallError; on a revert the payload is returned alongside it.
// maxUsedGas is the EVM gas the simulation needed before EIP-3529 refunds are
// credited.
func (f *Endorser) Call(ctx context.Context, msg *ethereum.CallMsg, blockNumber *big.Int) (ret []byte, maxUsedGas uint64, err error) {
	logger.Debugf("Endorser.Call() called with to=%s blockNumber=%v", msg.To, blockNumber)
	res, gas, err := f.Engine.Call(*msg, blockNumber)
	if err != nil {
		logger.Warnf("Endorser.Call() returning error: %v", err)
		return res, gas, &common.CallError{Status: classify(err), Message: err.Error(), Data: res}
	}
	logger.Debugf("Endorser.Call() returning %d bytes gasUsed=%d", len(res), gas)
	return res, gas, nil
}

func (f *Endorser) BalanceAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) (*big.Int, error) {
	return f.Engine.BalanceAt(ctx, account, blockNumber)
}

func (f *Endorser) StorageAt(ctx context.Context, account ethcommon.Address, key ethcommon.Hash, blockNumber *big.Int) ([]byte, error) {
	return f.Engine.StorageAt(ctx, account, key, blockNumber)
}

func (f *Endorser) CodeAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) ([]byte, error) {
	return f.Engine.CodeAt(ctx, account, blockNumber)
}

func (f *Endorser) NonceAt(ctx context.Context, account ethcommon.Address, blockNumber *big.Int) (uint64, error) {
	return f.Engine.NonceAt(ctx, account, blockNumber)
}

// classify maps an engine error to a status code. A revert is a committed
// outcome; an *execution.ExecFailure is a valid tx whose EVM execution failed;
// an *execution.TxRejected is an invalid client tx; anything else is a
// server-side fault.
func classify(err error) int32 {
	if errors.Is(err, vm.ErrExecutionReverted) {
		return common.StatusEVMRevert
	}
	if _, ok := errors.AsType[*execution.ExecFailure](err); ok {
		return common.StatusExecFailure
	}
	if _, ok := errors.AsType[*execution.TxRejected](err); ok {
		return common.StatusTxRejected
	}
	return common.StatusServerError
}

func response(res []byte, err error) *peer.ProposalResponse {
	if err != nil {
		return &peer.ProposalResponse{
			Version: 1,
			Response: &peer.Response{
				Status:  classify(err),
				Message: err.Error(),
				Payload: res,
			},
		}
	}

	return &peer.ProposalResponse{
		Version: 1,
		Response: &peer.Response{
			Status:  common.StatusOK,
			Message: "OK",
			Payload: res,
		},
	}
}
