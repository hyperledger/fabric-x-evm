/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/stretchr/testify/require"
)

// stubSubmitter always returns err from Submit, so tests can force a submission failure
// without needing a real packager/orderer (e.g. the ExecFailure case, where PackageTx
// rejects the response status before anything is ever broadcast).
type stubSubmitter struct{ err error }

func (s stubSubmitter) Submit(context.Context, sdk.Endorsement) error { return s.err }
func (s stubSubmitter) Close() error                                  { return nil }

// stubCompleter records which hashes were completed, guarded for concurrent worker access.
type stubCompleter struct {
	mu        sync.Mutex
	completed []common.Hash
}

func (c *stubCompleter) Complete(hash common.Hash) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.completed = append(c.completed, hash)
}

func (c *stubCompleter) all() []common.Hash {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]common.Hash(nil), c.completed...)
}

// TestBatchSubmitter_FailedSubmissionCompletesTx reproduces the leak: a transaction whose
// submission is rejected (e.g. ExecFailure's out-of-range status, or a real orderer/network
// failure) must be completed so it doesn't sit in TxQueue forever waiting for a commit that
// will never come — see docs/ISSUE_DRAFT_execfailure_commit.md.
func TestBatchSubmitter_FailedSubmissionCompletesTx(t *testing.T) {
	completer := &stubCompleter{}
	submitErr := errors.New("package proposal: proposal response was not successful, error code 460, msg invalid opcode: INVALID")
	bs := NewBatchSubmitter([]Submitter{stubSubmitter{err: submitErr}}, make(chan EndorsedTx, 1), 1, 0, completer)

	ctx := t.Context()
	bs.Start(ctx)
	defer bs.Stop()

	hash := common.HexToHash("0x1")
	bs.inputChan <- EndorsedTx{Hash: hash, End: sdk.Endorsement{}}

	require.Eventually(t, func() bool {
		return len(completer.all()) == 1
	}, time.Second, time.Millisecond, "failed submission should complete the tx instead of leaking it")
	require.Equal(t, hash, completer.all()[0])
}

// TestBatchSubmitter_SuccessfulSubmissionDoesNotComplete verifies the completer is only
// used for permanent failures. A successful submission still needs a real block commit
// before it's done (handled by TxQueue.Handle, not here) — completing it early would let a
// second, conflicting transaction reuse the same tracking slot before the first actually lands.
func TestBatchSubmitter_SuccessfulSubmissionDoesNotComplete(t *testing.T) {
	completer := &stubCompleter{}
	bs := NewBatchSubmitter([]Submitter{stubSubmitter{err: nil}}, make(chan EndorsedTx, 1), 1, 0, completer)

	ctx := t.Context()
	bs.Start(ctx)
	defer bs.Stop()

	bs.inputChan <- EndorsedTx{Hash: common.HexToHash("0x2"), End: sdk.Endorsement{}}

	// Give the worker a chance to process the (successful) submission, then confirm
	// nothing was completed as a side effect of it.
	require.Never(t, func() bool {
		return len(completer.all()) > 0
	}, 100*time.Millisecond, 10*time.Millisecond)
}

// TestBatchSubmitter_NilCompleterIsSafe: completer is optional (e.g. tests that don't care
// about failure tracking); a failed submission must not panic when it's nil.
func TestBatchSubmitter_NilCompleterIsSafe(t *testing.T) {
	bs := NewBatchSubmitter([]Submitter{stubSubmitter{err: errors.New("boom")}}, make(chan EndorsedTx, 1), 1, 0, nil)

	ctx := t.Context()
	bs.Start(ctx)
	defer bs.Stop()

	bs.inputChan <- EndorsedTx{Hash: common.HexToHash("0x3"), End: sdk.Endorsement{}}
	time.Sleep(50 * time.Millisecond) // let the worker process it; no assertion needed beyond "didn't panic"
}
