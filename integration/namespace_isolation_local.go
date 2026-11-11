/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"testing"

	sdk "github.com/hyperledger/fabric-x-sdk"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/fabrictest"

	"github.com/hyperledger/fabric-x-evm/endorser/execution"
)

// nsRecordGetter routes an MVCC read to the store that owns its namespace.
//
// fabrictest validates a block's reads against a single blocks.RecordGetter.
// With one namespace that is simply the endorser's KVS, which is what
// NewLocalTestHarness passes. With two, each namespace's KVS holds only its own
// namespace (the synchronizer's nsFilter sees to that), so handing the ledger
// either one alone would fail every read belonging to the other: the validator
// would find no record where the endorser reported a version, and reject the
// transaction as an MVCC conflict.
//
// Routing by namespace keeps validation against the real endorser stores — the
// faithful arrangement — rather than falling back to fabrictest's own world
// state.
//
// Entries are registered after fabrictest.Start, because the network has to
// exist before a harness can join it and produce the KVS. That is safe: nothing
// is validated until a transaction is submitted, which cannot happen until both
// harnesses are up. Not safe for concurrent registration, which is why
// registration happens on the test goroutine during setup only.
type nsRecordGetter map[string]blocks.RecordGetter

// Get implements blocks.RecordGetter. An unregistered namespace reads as empty
// rather than erroring: that is what the ledger would see for a namespace no
// endorser in this process tracks.
func (g nsRecordGetter) Get(namespace, key string) (*blocks.WriteRecord, error) {
	rg, ok := g[namespace]
	if !ok {
		return nil, nil
	}
	return rg.Get(namespace, key)
}

var _ blocks.RecordGetter = nsRecordGetter{}

// twoNamespaceLocalHarnesses brings up two fabrictest-backed harnesses, one per
// namespace, sharing a single network — the mock-backend half of the
// namespace-isolation matrix.
//
// Sharing one network is what makes the test meaningful: both namespaces commit
// to the same ledger, so a block can carry transactions from both.
// fabrictest is namespace-agnostic (Start ignores its namespace argument), so
// one network serves both.
//
// netCfg controls block composition. The zero value cuts a block per
// transaction, which makes the mixed-block case unreachable; pass
// MaxTxPerBlock/BlockCutTime to batch.
//
// networkType is "fabric" or "fabric-x", selecting the classic nfab delivery
// synchronizer or hybridx respectively.
func twoNamespaceLocalHarnesses(
	t *testing.T,
	logger sdk.Logger,
	evmConfig execution.EVMConfig,
	networkType string,
	nsA, nsB string,
	netCfg fabrictest.Config,
) (*TestHarness, *TestHarness) {
	t.Helper()

	reg := nsRecordGetter{}

	// Start the network before either harness, so both can join it. The
	// namespace argument is ignored by fabrictest; it is passed for parity with
	// the single-namespace path.
	nw, err := fabrictest.Start(t.Context(), nsA, networkType, netCfg, reg)
	if err != nil {
		t.Fatalf("fabrictest.Start: %v", err)
	}

	build := func(ns string) *TestHarness {
		// Only the namespace is overridden. Each newLocalHarness call takes its
		// own t.TempDir(), so the gateway and endorser databases are already
		// distinct files — no shared-cache collision of the kind the committed
		// YAML configs would have.
		th, _, err := newLocalHarness(t, logger, evmConfig, "", networkType,
			map[string]any{"Network.Namespace": ns},
			defaultEndorserFactory,
			localHarnessOpts{Network: nw},
		)
		if err != nil {
			t.Fatalf("ns=%s: harness: %v", ns, err)
		}
		t.Cleanup(func() { _ = th.Stop() })

		if len(th.DBs) != 1 {
			t.Fatalf("ns=%s: want 1 endorser KVS, got %d", ns, len(th.DBs))
		}
		reg[ns] = th.DBs[0]
		return th
	}

	return build(nsA), build(nsB)
}

// assertDistinctStores guards the premise of the whole matrix: if two parties
// shared a block store, "isolation" would be untestable and a pass would mean
// nothing. Cheap to check, and the committed YAML configs all use the same
// named shared-cache DSN (`file:gateway.db?mode=memory&cache=shared`), so this
// is a live hazard rather than a theoretical one.
func assertDistinctStores(t *testing.T, parties []nsParty) {
	t.Helper()

	// Two gateways over one store would answer identically for every block.
	// Comparing the stores directly is not possible through the public API, so
	// compare what they report: having already committed different transactions
	// to different namespaces, identical answers everywhere would be the
	// symptom.
	if len(parties) < 2 {
		return
	}
	if parties[0].Gateway == parties[1].Gateway {
		t.Fatal("both parties share one gateway; the matrix cannot measure isolation")
	}
	if parties[0].Namespace == parties[1].Namespace {
		t.Fatalf("both parties use namespace %q; the matrix needs two", parties[0].Namespace)
	}
}
