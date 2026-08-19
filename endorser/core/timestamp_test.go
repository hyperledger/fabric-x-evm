/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/config"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

// captureEngine records the blockTime passed to Execute for assertion.
type captureEngine struct {
	stubEngine
	mu        sync.Mutex
	blockTime uint64
	calls     int
}

func (c *captureEngine) Execute(_ context.Context, _ *types.Transaction, blockTime uint64) (endorsement.ExecutionResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blockTime = blockTime
	c.calls++
	return endorsement.ExecutionResult{Status: common.StatusOK}, nil
}

func TestNew_DefaultSkewBounds(t *testing.T) {
	f, err := New(nil, nil, config.Endorser{})
	if err != nil {
		t.Fatal(err)
	}
	if f.maxFuture != config.DefaultTimestampFutureSkew || f.maxPast != config.DefaultTimestampPastSkew {
		t.Fatalf("skew = %v/%v, want defaults %v/%v", f.maxFuture, f.maxPast,
			config.DefaultTimestampFutureSkew, config.DefaultTimestampPastSkew)
	}
	if f.now == nil {
		t.Fatal("now clock not set")
	}
}

func TestNew_CustomSkewBoundsFromConfig(t *testing.T) {
	f, err := New(nil, nil, config.Endorser{
		MaxTimestampFuture: 3 * time.Second,
		MaxTimestampPast:   9 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.maxFuture != 3*time.Second || f.maxPast != 9*time.Second {
		t.Fatalf("custom: skew = %v/%v", f.maxFuture, f.maxPast)
	}
}

func TestValidateRequestTimestamp_AcceptsWithinBounds(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	future := config.DefaultTimestampFutureSkew
	past := config.DefaultTimestampPastSkew

	cases := []time.Time{
		now,
		now.Add(future),
		now.Add(-past),
		now.Add(5 * time.Second),
		now.Add(-30 * time.Second),
	}
	for _, ts := range cases {
		if err := validateRequestTimestamp(ts, now, future, past); err != nil {
			t.Errorf("ts %v: unexpected error: %v", ts, err)
		}
	}
}

func TestValidateRequestTimestamp_RejectsOutsideBounds(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	future := 10 * time.Second
	past := 60 * time.Second

	if err := validateRequestTimestamp(time.Time{}, now, future, past); err == nil {
		t.Error("zero timestamp: want error")
	}
	if err := validateRequestTimestamp(now.Add(future+time.Second), now, future, past); err == nil {
		t.Error("too far future: want error")
	}
	if err := validateRequestTimestamp(now.Add(-(past + time.Second)), now, future, past); err == nil {
		t.Error("too far past: want error")
	}
	if err := validateRequestTimestamp(time.Unix(-1, 0), now, future, 1<<62); err == nil {
		t.Error("pre-epoch timestamp: want error")
	}
}

func TestValidateRequestTimestamp_NoClamping(t *testing.T) {
	// Accepted values must be used as-is by Execute; this only checks the
	// validator does not mutate (it returns error, not a clamped time).
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	ts := now.Add(3 * time.Second)
	if err := validateRequestTimestamp(ts, now, 10*time.Second, 60*time.Second); err != nil {
		t.Fatal(err)
	}
	if ts.Unix() != now.Add(3*time.Second).Unix() {
		t.Error("timestamp was mutated")
	}
}

func TestExecute_RejectsTimestampOutsideWindow(t *testing.T) {
	fixedNow := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	eng := &captureEngine{}
	f := &Endorser{
		Engine:    eng,
		builder:   &stubBuilder{resp: &peer.ProposalResponse{Response: &peer.Response{Status: common.StatusOK}}},
		maxFuture: 10 * time.Second,
		maxPast:   60 * time.Second,
		now:       func() time.Time { return fixedNow },
	}
	tx := types.NewTx(&types.LegacyTx{Gas: 21000, GasPrice: big.NewInt(0)})

	resp, err := f.Execute(context.Background(), endorsement.Invocation{}, tx, fixedNow.Add(30*time.Second))
	if err != nil {
		t.Fatalf("Go error: %v", err)
	}
	if resp.Response.Status != common.StatusTxRejected {
		t.Fatalf("status = %d, want StatusTxRejected (%d); msg=%q",
			resp.Response.Status, common.StatusTxRejected, resp.Response.Message)
	}
	if eng.calls != 0 {
		t.Error("engine should not run when timestamp is rejected")
	}
}

func TestExecute_PassesValidatedUnixTimeToEngine(t *testing.T) {
	fixedNow := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	reqTS := fixedNow.Add(2 * time.Second)
	eng := &captureEngine{}
	f := &Endorser{
		Engine:    eng,
		builder:   &stubBuilder{resp: &peer.ProposalResponse{Response: &peer.Response{Status: common.StatusOK}}},
		maxFuture: 10 * time.Second,
		maxPast:   60 * time.Second,
		now:       func() time.Time { return fixedNow },
	}
	tx := types.NewTx(&types.LegacyTx{Gas: 21000, GasPrice: big.NewInt(0)})

	resp, err := f.Execute(context.Background(), endorsement.Invocation{}, tx, reqTS)
	if err != nil {
		t.Fatalf("Go error: %v", err)
	}
	if resp.Response.Status != common.StatusOK {
		t.Fatalf("status = %d, want OK; msg=%q", resp.Response.Status, resp.Response.Message)
	}
	if eng.blockTime != uint64(reqTS.Unix()) {
		t.Fatalf("blockTime = %d, want %d", eng.blockTime, reqTS.Unix())
	}
}

// Two endorsers given the same request timestamp must pass the same blockTime
// to the engine (deterministic RWS for multi-org endorsement).
func TestExecute_IdenticalTimestampAcrossEndorsers(t *testing.T) {
	fixedNow := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	reqTS := fixedNow.Add(1 * time.Second)
	tx := types.NewTx(&types.LegacyTx{Gas: 21000, GasPrice: big.NewInt(0)})

	var got [2]uint64
	for i := 0; i < 2; i++ {
		eng := &captureEngine{}
		// Stagger the local clock slightly; only the request timestamp matters.
		localSkew := time.Duration(i) * 50 * time.Millisecond
		f := &Endorser{
			Engine:    eng,
			builder:   &stubBuilder{resp: &peer.ProposalResponse{Response: &peer.Response{Status: common.StatusOK}}},
			maxFuture: 10 * time.Second,
			maxPast:   60 * time.Second,
			now:       func() time.Time { return fixedNow.Add(localSkew) },
		}
		if _, err := f.Execute(context.Background(), endorsement.Invocation{}, tx, reqTS); err != nil {
			t.Fatalf("endorser %d: %v", i, err)
		}
		got[i] = eng.blockTime
	}
	if got[0] != got[1] {
		t.Fatalf("endorsers saw different blockTime: %d vs %d", got[0], got[1])
	}
	if got[0] != uint64(reqTS.Unix()) {
		t.Fatalf("blockTime = %d, want %d", got[0], reqTS.Unix())
	}
}

func TestExecute_ConcurrentRequestsNoSharedTimestampState(t *testing.T) {
	fixedNow := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	f := &Endorser{
		Engine:    &captureEngine{},
		builder:   &stubBuilder{resp: &peer.ProposalResponse{Response: &peer.Response{Status: common.StatusOK}}},
		maxFuture: 10 * time.Second,
		maxPast:   60 * time.Second,
		now:       func() time.Time { return fixedNow },
	}
	tx := types.NewTx(&types.LegacyTx{Gas: 21000, GasPrice: big.NewInt(0)})

	const n = 32
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Different request times, all valid; no requirement that they are monotonic.
			ts := fixedNow.Add(time.Duration(i%5) * time.Second)
			_, err := f.Execute(context.Background(), endorsement.Invocation{}, tx, ts)
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Execute: %v", err)
	}
}

// Ensure NewExecFailure path type is still available for classify tests.
var _ = execution.NewExecFailure
