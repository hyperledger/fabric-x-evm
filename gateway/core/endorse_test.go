/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/api"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

type stubEndorser struct {
	callPayload []byte
	callGas     uint64
	callErr     error
	// callFunc, if set, computes Call's result from the requested gas limit
	// instead of the fixed fields above (for EstimateGas tests).
	callFunc func(gas uint64) ([]byte, uint64, error)
	nonce    uint64
	nonceErr error
	balance  *big.Int
	storage  []byte
	code     []byte
	execResp *peer.ProposalResponse
	execErr  error
	// lastTS is the timestamp from the most recent Execute call.
	lastTS time.Time
}

func (s *stubEndorser) Execute(ctx context.Context, inv endorsement.Invocation, ethTx *types.Transaction, ts time.Time) (*peer.ProposalResponse, error) {
	s.lastTS = ts
	return s.execResp, s.execErr
}
func (s *stubEndorser) Call(ctx context.Context, msg *ethereum.CallMsg, _ *big.Int) ([]byte, uint64, error) {
	if s.callFunc != nil {
		return s.callFunc(msg.Gas)
	}
	return s.callPayload, s.callGas, s.callErr
}
func (s *stubEndorser) BalanceAt(ctx context.Context, _ ethcommon.Address, _ *big.Int) (*big.Int, error) {
	return s.balance, nil
}
func (s *stubEndorser) StorageAt(ctx context.Context, _ ethcommon.Address, _ ethcommon.Hash, _ *big.Int) ([]byte, error) {
	return s.storage, nil
}
func (s *stubEndorser) CodeAt(ctx context.Context, _ ethcommon.Address, _ *big.Int) ([]byte, error) {
	return s.code, nil
}
func (s *stubEndorser) NonceAt(ctx context.Context, _ ethcommon.Address, _ *big.Int) (uint64, error) {
	return s.nonce, s.nonceErr
}

func newClient(stub *stubEndorser) *EndorsementClient {
	return &EndorsementClient{endorsers: []api.Service{stub}}
}

// stubSigner is a gateway Signer that returns fixed bytes, enough for
// NewInvocation to build a proposal without real crypto.
type stubSigner struct{}

func (stubSigner) Sign([]byte) ([]byte, error) { return []byte("sig"), nil }
func (stubSigner) Serialize() ([]byte, error)  { return []byte("creator"), nil }

// signingClient is a client wired with a signer so ExecuteTransaction can build
// an invocation.
func signingClient(stub *stubEndorser) *EndorsementClient {
	return &EndorsementClient{
		endorsers: []api.Service{stub},
		signer:    stubSigner{},
		channel:   "ch",
		namespace: "ns",
		nsVersion: "1.0",
	}
}

func TestCallContract_Status201ReturnsRevertError(t *testing.T) {
	payload := []byte{0x08, 0xc3, 0x79, 0xa0, 0xde, 0xad, 0xbe, 0xef}
	c := newClient(&stubEndorser{
		callPayload: payload,
		callErr: &common.CallError{
			Status:  common.StatusEVMRevert,
			Message: "execution reverted: out of stock",
			Data:    payload,
		},
	})

	_, err := c.CallContract(context.Background(), ethereum.CallMsg{}, nil)

	var revert *domain.RevertError
	if !errors.As(err, &revert) {
		t.Fatalf("expected *RevertError, got %T (%v)", err, err)
	}
	if revert.Reason != "execution reverted: out of stock" {
		t.Errorf("Reason = %q", revert.Reason)
	}
	if !bytes.Equal(revert.Data, payload) {
		t.Errorf("Data = %x, want %x", revert.Data, payload)
	}
	if !errors.Is(err, domain.ErrExecutionReverted) {
		t.Error("errors.Is(err, ErrExecutionReverted) = false")
	}
}

func TestCallContract_Status500IsGenericError(t *testing.T) {
	c := newClient(&stubEndorser{
		callErr: &common.CallError{Status: common.StatusServerError, Message: "endorser dead"},
	})

	_, err := c.CallContract(context.Background(), ethereum.CallMsg{}, nil)

	var revert *domain.RevertError
	if errors.As(err, &revert) {
		t.Errorf("non-revert error must not be *RevertError, got %v", revert)
	}
	if err == nil {
		t.Fatal("expected error")
	}
	var exec *domain.ExecutionError
	if errors.As(err, &exec) {
		t.Errorf("backend fault must not be *ExecutionError, got %v", exec)
	}
}

func TestCallContract_Status400ReturnsExecutionError(t *testing.T) {
	c := newClient(&stubEndorser{
		callErr: &common.CallError{Status: common.StatusExecFailure, Message: "out of gas"},
	})

	_, err := c.CallContract(context.Background(), ethereum.CallMsg{}, nil)

	var exec *domain.ExecutionError
	if !errors.As(err, &exec) {
		t.Fatalf("expected *ExecutionError, got %T (%v)", err, err)
	}
	if exec.Message != "out of gas" {
		t.Errorf("Message = %q, want %q", exec.Message, "out of gas")
	}
}

func TestCallContract_Status200ReturnsPayload(t *testing.T) {
	want := []byte{0xde, 0xad}
	c := newClient(&stubEndorser{callPayload: want})

	got, err := c.CallContract(context.Background(), ethereum.CallMsg{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("payload = %x, want %x", got, want)
	}
}

// An empty-data revert on eth_call is not an error: many Ethereum tools probe
// contracts this way (e.g. checking whether a function exists).
func TestCallContract_EmptyRevertIsNotAnError(t *testing.T) {
	c := newClient(&stubEndorser{
		callErr: &common.CallError{Status: common.StatusEVMRevert, Message: "execution reverted"},
	})

	got, err := c.CallContract(context.Background(), ethereum.CallMsg{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("payload = %x, want nil", got)
	}
}

// The loop's first try is always padded by the sentry buffer, even when the
// raw maxUsedGas alone would already have verified: we can't tell in advance
// whether a call touches storage, and paying that fixed, tiny padding
// unconditionally is cheaper than risking a wasted probe on every write.
func TestEstimateGas_PadsFirstGuessWithSentryBuffer(t *testing.T) {
	c := newClient(&stubEndorser{callPayload: []byte{0x01}, callGas: 42123})

	got, err := c.EstimateGas(context.Background(), ethereum.CallMsg{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := uint64(42123) + sstoreSentryBuffer; got != want {
		t.Errorf("gas = %d, want %d", got, want)
	}
}

// A real submission needs more than the estimate (reentrancy-sentry). The stub
// only succeeds once actually given at least threshold gas, regardless of
// what usedGas it reports, so a correct estimate must be verified to
// actually work at >= threshold, not just usedGas*2 if that's still short.
func TestEstimateGas_EscalatesPastFailingGuess(t *testing.T) {
	const (
		reportedUsedGas = uint64(10_000) // 2x = 20,000, still short
		threshold       = uint64(25_000) // real minimum working gas limit
	)
	c := newClient(&stubEndorser{
		callFunc: func(gas uint64) ([]byte, uint64, error) {
			if gas < threshold {
				return nil, reportedUsedGas, &common.CallError{
					Status:  common.StatusExecFailure,
					Message: "out of gas: not enough gas for reentrancy sentry",
				}
			}
			return []byte{0x01}, reportedUsedGas, nil
		},
	})

	got, err := c.EstimateGas(context.Background(), ethereum.CallMsg{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got < threshold {
		t.Fatalf("gas = %d, want >= %d (the verified working minimum)", got, threshold)
	}
}

// If nothing below the ceiling verifies, EstimateGas falls back to the
// ceiling -- which the initial probe already proved works.
func TestEstimateGas_FallsBackToCeilingWhenNothingSmallerVerifies(t *testing.T) {
	c := newClient(&stubEndorser{
		callFunc: func(gas uint64) ([]byte, uint64, error) {
			if gas < estimateGasCeiling {
				return nil, 100, &common.CallError{Status: common.StatusExecFailure, Message: "out of gas"}
			}
			return []byte{0x01}, 100, nil
		},
	})

	got, err := c.EstimateGas(context.Background(), ethereum.CallMsg{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != estimateGasCeiling {
		t.Errorf("gas = %d, want %d (the ceiling)", got, estimateGasCeiling)
	}
}

// Reproduces the actual bug this fix exists for: a call through an EIP-1167
// minimal proxy (delegatecall) that runs out of the gas forwarded to it
// bubbles up as an empty-data revert, indistinguishable at this layer from a
// genuine zero-data revert. Escalating (not hard-stopping) is what finds a
// gas limit that actually works.
func TestEstimateGas_EmptyRevertKeepsEscalating(t *testing.T) {
	const (
		reportedUsedGas = uint64(10_000)
		threshold       = uint64(25_000) // real minimum working gas limit
	)
	c := newClient(&stubEndorser{
		callFunc: func(gas uint64) ([]byte, uint64, error) {
			if gas < threshold {
				// Empty Data: the delegatecall ran out of gas, no revert reason.
				return nil, reportedUsedGas, &common.CallError{Status: common.StatusEVMRevert, Message: "execution reverted"}
			}
			return []byte{0x01}, reportedUsedGas, nil
		},
	})

	got, err := c.EstimateGas(context.Background(), ethereum.CallMsg{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got < threshold {
		t.Fatalf("gas = %d, want >= %d (the verified working minimum)", got, threshold)
	}
}

// If even the ceiling probe hits an empty revert, EstimateGas must not
// silently treat that as success -- it should report the same allowance
// error as any other unrecoverable failure at the ceiling.
func TestEstimateGas_EmptyRevertAtCeilingIsAllowanceError(t *testing.T) {
	c := newClient(&stubEndorser{
		callErr: &common.CallError{Status: common.StatusEVMRevert, Message: "execution reverted"},
	})

	_, err := c.EstimateGas(context.Background(), ethereum.CallMsg{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var rev *domain.RevertError
	if errors.As(err, &rev) {
		t.Errorf("an empty revert must not be treated as a hard *RevertError, got %v", rev)
	}
	if !strings.Contains(err.Error(), "gas required exceeds allowance") {
		t.Errorf("error = %q, want it to mention the allowance ceiling", err.Error())
	}
}

func TestEstimateGas_RevertPropagates(t *testing.T) {
	c := newClient(&stubEndorser{
		callErr: &common.CallError{Status: common.StatusEVMRevert, Message: "execution reverted", Data: []byte{0x08, 0xc3}},
		callGas: 12000,
	})

	_, err := c.EstimateGas(context.Background(), ethereum.CallMsg{}, nil)
	var rev *domain.RevertError
	if !errors.As(err, &rev) {
		t.Fatalf("expected *RevertError, got %T (%v)", err, err)
	}
}

// A non-revert failure (out of gas, ...) maps to an allowance error, not a
// *domain.RevertError, since raising the ceiling further could still help.
func TestEstimateGas_ExecFailurePropagatesAsAllowanceError(t *testing.T) {
	c := newClient(&stubEndorser{
		callErr: &common.CallError{Status: common.StatusExecFailure, Message: "out of gas"},
	})

	_, err := c.EstimateGas(context.Background(), ethereum.CallMsg{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var rev *domain.RevertError
	if errors.As(err, &rev) {
		t.Errorf("a non-revert failure at the ceiling must not be *RevertError, got %v", rev)
	}
	if !strings.Contains(err.Error(), "gas required exceeds allowance") {
		t.Errorf("error = %q, want it to mention the allowance ceiling", err.Error())
	}
}

// A rejected tx (400) also maps to *ExecutionError, like a failed execution.
func TestCallContract_TxRejectedReturnsExecutionError(t *testing.T) {
	c := newClient(&stubEndorser{
		callErr: &common.CallError{Status: common.StatusTxRejected, Message: "nonce too low"},
	})

	_, err := c.CallContract(context.Background(), ethereum.CallMsg{}, nil)

	var exec *domain.ExecutionError
	if !errors.As(err, &exec) {
		t.Fatalf("expected *ExecutionError, got %T (%v)", err, err)
	}
	if exec.Message != "nonce too low" {
		t.Errorf("Message = %q, want %q", exec.Message, "nonce too low")
	}
}

// A plain (non-CallError) error is a transport failure: it is wrapped, not
// turned into a revert or execution error.
func TestCallContract_TransportErrorIsWrapped(t *testing.T) {
	c := newClient(&stubEndorser{callErr: errors.New("connection refused")})

	_, err := c.CallContract(context.Background(), ethereum.CallMsg{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}

	var revert *domain.RevertError
	if errors.As(err, &revert) {
		t.Errorf("transport error must not be *RevertError, got %v", revert)
	}
	var exec *domain.ExecutionError
	if errors.As(err, &exec) {
		t.Errorf("transport error must not be *ExecutionError, got %v", exec)
	}
	if err.Error() != "process call: connection refused" {
		t.Errorf("error = %q, want %q", err.Error(), "process call: connection refused")
	}
}

// The state readers forward straight to the endorser.
func TestEndorsementClient_StateReadersDelegate(t *testing.T) {
	stub := &stubEndorser{
		balance: big.NewInt(99),
		storage: []byte{0xaa},
		code:    []byte{0xbb},
		nonce:   5,
	}
	c := newClient(stub)
	ctx := context.Background()
	addr := ethcommon.Address{}

	if bal, _ := c.BalanceAt(ctx, addr, nil); bal.Cmp(stub.balance) != 0 {
		t.Errorf("BalanceAt = %v, want %v", bal, stub.balance)
	}
	if got, _ := c.StorageAt(ctx, addr, ethcommon.Hash{}, nil); !bytes.Equal(got, stub.storage) {
		t.Errorf("StorageAt = %x, want %x", got, stub.storage)
	}
	if got, _ := c.CodeAt(ctx, addr, nil); !bytes.Equal(got, stub.code) {
		t.Errorf("CodeAt = %x, want %x", got, stub.code)
	}
	if got, _ := c.NonceAt(ctx, addr, nil); got != stub.nonce {
		t.Errorf("NonceAt = %d, want %d", got, stub.nonce)
	}
}

// An endorsable result is assembled into the endorsement (proposal + responses).
func TestExecuteTransaction_Success(t *testing.T) {
	pResp := &peer.ProposalResponse{Response: &peer.Response{Status: common.StatusOK}}
	c := signingClient(&stubEndorser{execResp: pResp})
	tx := types.NewTx(&types.LegacyTx{Gas: 21000, GasPrice: big.NewInt(0)})

	end, err := c.ExecuteTransaction(context.Background(), tx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(end.Responses) != 1 || end.Responses[0] != pResp {
		t.Errorf("Responses = %v, want [%v]", end.Responses, pResp)
	}
	if end.Proposal == nil {
		t.Error("Proposal = nil, want non-nil")
	}
}

// Only classic Fabric signs over the proposal, so only it gets a whole one.
func TestExecuteTransaction_ProposalMatchesProtocol(t *testing.T) {
	tests := []struct {
		protocol    string
		wantPayload bool
	}{
		{protocol: "", wantPayload: false},
		{protocol: common.ProtocolFabricX, wantPayload: false},
		{protocol: common.ProtocolFabric, wantPayload: true},
	}

	for _, tt := range tests {
		t.Run("protocol="+tt.protocol, func(t *testing.T) {
			pResp := &peer.ProposalResponse{Response: &peer.Response{Status: common.StatusOK}}
			c, err := NewEndorsementClient([]api.Service{&stubEndorser{execResp: pResp}}, stubSigner{}, "ch", "ns", "1.0", tt.protocol)
			if err != nil {
				t.Fatal(err)
			}

			end, err := c.ExecuteTransaction(context.Background(), types.NewTx(&types.LegacyTx{Gas: 21000, GasPrice: big.NewInt(0)}))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if end.Proposal == nil {
				t.Fatal("Proposal = nil, want at least a header")
			}
			if len(end.Proposal.Header) == 0 {
				t.Error("Proposal.Header is empty")
			}
			if got := len(end.Proposal.Payload) > 0; got != tt.wantPayload {
				t.Errorf("proposal carries a payload = %v, want %v", got, tt.wantPayload)
			}
		})
	}
}

// An unrecognized protocol fails at construction, not at submission.
func TestNewEndorsementClient_RejectsUnknownProtocol(t *testing.T) {
	if _, err := NewEndorsementClient(nil, stubSigner{}, "ch", "ns", "1.0", "bogus"); err == nil {
		t.Fatal("expected an error for an unknown protocol")
	}
}

// Every endorser must see the same gateway-stamped timestamp.
func TestExecuteTransaction_SameTimestampForAllEndorsers(t *testing.T) {
	pResp := &peer.ProposalResponse{Response: &peer.Response{Status: common.StatusOK}}
	a := &stubEndorser{execResp: pResp}
	b := &stubEndorser{execResp: pResp}
	c := &EndorsementClient{
		endorsers: []api.Service{a, b},
		signer:    stubSigner{},
		channel:   "ch",
		namespace: "ns",
		nsVersion: "1.0",
	}
	tx := types.NewTx(&types.LegacyTx{Gas: 21000, GasPrice: big.NewInt(0)})

	before := time.Now()
	if _, err := c.ExecuteTransaction(context.Background(), tx); err != nil {
		t.Fatalf("ExecuteTransaction: %v", err)
	}
	after := time.Now()

	if a.lastTS.IsZero() || b.lastTS.IsZero() {
		t.Fatal("expected both endorsers to receive a timestamp")
	}
	if !a.lastTS.Equal(b.lastTS) {
		t.Fatalf("endorsers got different timestamps: %v vs %v", a.lastTS, b.lastTS)
	}
	if a.lastTS.Before(before.Add(-time.Second)) || a.lastTS.After(after.Add(time.Second)) {
		t.Fatalf("timestamp %v outside [before, after] window", a.lastTS)
	}
}

// A rejected tx surfaces as a Go error (the caller must fix and resubmit).
func TestExecuteTransaction_RejectedStatusErrors(t *testing.T) {
	pResp := &peer.ProposalResponse{Response: &peer.Response{Status: common.StatusTxRejected, Message: "nonce too low"}}
	c := signingClient(&stubEndorser{execResp: pResp})
	tx := types.NewTx(&types.LegacyTx{Gas: 21000, GasPrice: big.NewInt(0)})

	if _, err := c.ExecuteTransaction(context.Background(), tx); err == nil {
		t.Fatal("expected error for rejected status")
	}
}

// blockingEndorser rejects the transaction only once released, and reports
// whatever the context says if it is cancelled first. It stands in for a remote
// endorser whose in-flight call is interruptible, which an in-process one is not.
type blockingEndorser struct {
	stubEndorser
	release <-chan struct{}
}

func (b *blockingEndorser) Execute(ctx context.Context, inv endorsement.Invocation, ethTx *types.Transaction, _ time.Time) (*peer.ProposalResponse, error) {
	select {
	case <-b.release:
		return b.execResp, b.execErr
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// With several endorsers the first rejection cancels the others, so their calls
// come back cancelled. The caller must still be told why the transaction was
// rejected rather than that something was cancelled.
func TestExecuteTransaction_RejectionSurvivesCancellationOfOtherEndorsers(t *testing.T) {
	rejected := &peer.ProposalResponse{Response: &peer.Response{Status: common.StatusTxRejected, Message: "nonce too low"}}

	// Never released, so this one only ever returns the cancellation. It sits at
	// index 0, ahead of the endorser that produces the real error.
	blocked := &blockingEndorser{release: make(chan struct{})}
	c := &EndorsementClient{
		endorsers: []api.Service{blocked, &stubEndorser{execResp: rejected}},
		signer:    stubSigner{},
		channel:   "ch",
		namespace: "ns",
		nsVersion: "1.0",
	}

	tx := types.NewTx(&types.LegacyTx{Gas: 21000, GasPrice: big.NewInt(0)})
	_, err := c.ExecuteTransaction(context.Background(), tx)
	if err == nil {
		t.Fatal("expected an error for a rejected transaction")
	}
	if !strings.Contains(err.Error(), "nonce too low") {
		t.Errorf("error = %q, want the rejection reason", err.Error())
	}
}
