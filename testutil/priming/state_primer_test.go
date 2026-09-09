/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package priming

import (
	"math/big"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	estorage "github.com/hyperledger/fabric-x-evm/endorser/storage"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

const testNS = "basic"

var primerTestAddr = ethcommon.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")

// stubSigner is enough identity for endorsement.NewInvocation to build a proposal;
// nothing in these tests verifies a signature.
type stubSigner struct{}

func (stubSigner) Sign([]byte) ([]byte, error) { return []byte("sig"), nil }
func (stubSigner) Serialize() ([]byte, error)  { return []byte("creator"), nil }

// countingBuilder stands in for one endorser's signing key: it records what it was
// asked to endorse instead of signing, and counts how many times.
type countingBuilder struct {
	name    string
	calls   int
	lastRWS blocks.ReadWriteSet
}

func (b *countingBuilder) Endorse(_ endorsement.Invocation, res endorsement.ExecutionResult) (*peer.ProposalResponse, error) {
	b.calls++
	b.lastRWS = res.RWS
	return &peer.ProposalResponse{
		Response: &peer.Response{Status: 200, Message: b.name},
	}, nil
}

// balFromRWS returns the balance a read-write set writes for addr, or nil when the
// set contains no balance write for it.
func balFromRWS(rws blocks.ReadWriteSet, addr ethcommon.Address) *big.Int {
	k := "acc:" + addr.Hex() + ":bal"
	for _, w := range rws.Writes {
		if w.Key == k {
			return new(big.Int).SetBytes(w.Value)
		}
	}
	return nil
}

// newTestPrimer builds a primer over an in-memory KVS with no gateway or submitter:
// enough to exercise the state-building and endorsement-collecting halves, which is
// where the multi-endorser behaviour lives.
func newTestPrimer(t *testing.T, builders ...endorsement.Builder) *StatePrimer {
	t.Helper()
	kvs := estorage.NewLightKVS(8)
	sp, err := NewStatePrimer(nil, nil, kvs, testNS, stubSigner{}, builders, "mychannel", "1.0", true)
	if err != nil {
		t.Fatalf("NewStatePrimer: %v", err)
	}
	return sp
}

func TestForceSetBalance_RaiseFromZero(t *testing.T) {
	sp := newTestPrimer(t)
	sp.ForceSetBalance(primerTestAddr, big.NewInt(1_000))

	got := balFromRWS(sp.Writes(), primerTestAddr)
	if got == nil {
		t.Fatal("no balance write in the read-write set")
	}
	if got.Cmp(big.NewInt(1_000)) != 0 {
		t.Fatalf("balance write = %s, want 1000", got)
	}
}

// TestForceSetBalance_LowerExisting is the case plain SetBalance gets wrong: it only
// ever adds, so lowering a balance needs the subtract branch.
func TestForceSetBalance_LowerExisting(t *testing.T) {
	sp := newTestPrimer(t)

	// Seed a balance, then drive it down.
	sp.ForceSetBalance(primerTestAddr, big.NewInt(1_000))
	seeded := balFromRWS(sp.Writes(), primerTestAddr)
	if seeded == nil || seeded.Cmp(big.NewInt(1_000)) != 0 {
		t.Fatalf("seed balance = %v, want 1000", seeded)
	}

	sp.ForceSetBalance(primerTestAddr, big.NewInt(250))
	got := balFromRWS(sp.Writes(), primerTestAddr)
	if got == nil {
		t.Fatal("no balance write after lowering")
	}
	if got.Cmp(big.NewInt(250)) != 0 {
		t.Fatalf("balance after lowering = %s, want 250", got)
	}
}

func TestForceSetBalance_NilAmountIsNoOp(t *testing.T) {
	sp := newTestPrimer(t)
	sp.ForceSetBalance(primerTestAddr, nil)
	if got := balFromRWS(sp.Writes(), primerTestAddr); got != nil {
		t.Fatalf("nil amount wrote balance %s, want no write", got)
	}
}

// TestEndorsesWithEveryBuilder is the guarantee that makes remote endorsers work:
// the priming transaction is endorsed locally once per builder, and the endorsers
// themselves are never contacted. A network needing three signatures is satisfied by
// supplying three builders.
func TestEndorsesWithEveryBuilder(t *testing.T) {
	b1 := &countingBuilder{name: "org1"}
	b2 := &countingBuilder{name: "org2"}
	b3 := &countingBuilder{name: "org3"}
	sp := newTestPrimer(t, b1, b2, b3)

	sp.ForceSetBalance(primerTestAddr, big.NewInt(42))

	// Collect endorsements exactly as Commit does, without needing a gateway or
	// submitter to carry the result anywhere.
	inv, err := endorsement.NewInvocation(sp.signer, "mychannel", testNS, "1.0", [][]byte{{0xfb}, {0x01}})
	if err != nil {
		t.Fatalf("NewInvocation: %v", err)
	}
	rws := sp.Writes()
	var responses []*peer.ProposalResponse
	for _, b := range sp.builders {
		presp, err := b.Endorse(inv, endorsement.Success(rws, nil, nil))
		if err != nil {
			t.Fatalf("Endorse: %v", err)
		}
		responses = append(responses, presp)
	}

	if len(responses) != 3 {
		t.Fatalf("collected %d endorsements, want 3", len(responses))
	}
	for _, b := range []*countingBuilder{b1, b2, b3} {
		if b.calls != 1 {
			t.Fatalf("builder %s endorsed %d times, want 1", b.name, b.calls)
		}
		// Every endorser must sign the identical read-write set, or the committer
		// sees divergent endorsements and rejects the transaction.
		if got := balFromRWS(b.lastRWS, primerTestAddr); got == nil || got.Cmp(big.NewInt(42)) != 0 {
			t.Fatalf("builder %s endorsed balance %v, want 42", b.name, got)
		}
	}
}

// TestResetDiscardsQueuedWrites pins why each hardhat RPC resets first: without it a
// primer accumulates earlier writes and re-commits them.
func TestResetDiscardsQueuedWrites(t *testing.T) {
	sp := newTestPrimer(t)
	sp.ForceSetBalance(primerTestAddr, big.NewInt(500))
	if balFromRWS(sp.Writes(), primerTestAddr) == nil {
		t.Fatal("expected a queued balance write before reset")
	}

	if _, err := sp.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if got := balFromRWS(sp.Writes(), primerTestAddr); got != nil {
		t.Fatalf("balance write %s survived Reset, want none", got)
	}
}
