/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"io"
	"math/big"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc/grpclog"

	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-sdk/fabrictest"
)

// The namespace-isolation matrix.
//
// Two namespaces on one channel, one gateway each, run against every backend we
// can reach. The shared body lives in namespace_isolation.go; these are the
// per-backend entry points.
//
// Backends and how to run them:
//
//	TestNamespaceIsolationLocal   — fabrictest, classic fabric (nfab delivery).
//	TestNamespaceIsolationLocalX  — fabrictest, fabric-x (hybridx). Both run
//	                                under `go test ./integration` with no
//	                                network, so they are the fast feedback loop.
//	TestNamespaceIsolationFabricX — the fabric-x test committer. Needs
//	                                `make start-x-batched` (block size > 1; the
//	                                default start-x cuts one tx per block, so
//	                                mixed blocks are unreachable there).
//	TestNamespaceIsolationFablo   — a running Fablo network with two chaincodes.
//
// These tests record behaviour and assert only that neither gateway can see the
// other namespace's data. Transaction indices, index holes and block numbering
// are printed, not checked — deciding what those should be is the point of
// gathering the output. Run with -v to see it.

// nsMatrixSupply is the token supply each party's contract is deployed with.
// Large enough that the matrix's transfers never exhaust it.
var nsMatrixSupply = big.NewInt(1_000_000)

// nsMatrixBatching batches up to 4 transactions into a block, waiting briefly
// for a partial batch to fill. Without this fabrictest cuts a block per
// transaction (BlockCutTime 0 = cut immediately) and the mixed-block case —
// the one that produces index holes — cannot occur.
var nsMatrixBatching = fabrictest.Config{
	MaxTxPerBlock: 4,
	BlockCutTime:  250 * time.Millisecond,
}

// namespaces used by the mock backends. fabrictest does not need them declared
// anywhere: it ignores Start's namespace argument and validates whatever
// arrives. The real backends need them created on the network first.
const (
	nsMatrixA = "basic"
	nsMatrixB = "basic2"
)

// TestNamespaceIsolationLocal runs the matrix on fabrictest with classic-fabric
// encoding, which is the only backend that exercises nfab.NewSynchronizer —
// the non-hybridx delivery path.
func TestNamespaceIsolationLocal(t *testing.T) {
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr))
	runLocalNamespaceMatrix(t, "fabric")
}

// TestNamespaceIsolationLocalX runs the matrix on fabrictest with fabric-x
// encoding, so the handler chain is fed by hybridx.
//
// Note on what this can and cannot show: fabrictest's StreamBlocks drops a
// block whose events all filter out, so on this backend the notification path
// never delivers an empty-after-filter block while its own delivery path does.
// The real committer is documented to send every block unconditionally (see
// synchronizer.NewDelivery's comment) — comparing this output with
// TestNamespaceIsolationFabricX is how we find out whether that holds.
func TestNamespaceIsolationLocalX(t *testing.T) {
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr))
	runLocalNamespaceMatrix(t, "fabric-x")
}

func runLocalNamespaceMatrix(t *testing.T, networkType string) {
	t.Helper()

	thA, thB := twoNamespaceLocalHarnesses(t, TestLogger{T: t}, execution.EVMConfig{},
		networkType, nsMatrixA, nsMatrixB, nsMatrixBatching)

	parties := []nsParty{
		newParty(t, nsMatrixA, thA.Gateways[0], thA.ethChainConfig, nsMatrixSupply),
		newParty(t, nsMatrixB, thB.Gateways[0], thB.ethChainConfig, nsMatrixSupply),
	}
	assertDistinctStores(t, parties)

	runNamespaceIsolationMatrix(t, "fabrictest/"+networkType, parties)
}

// TestNamespaceIsolationFabricX runs the matrix against the real fabric-x test
// committer, which is the backend whose answers we actually care about.
//
// Requires `make start-x-batched`: the default start-x runs the mock orderer
// with SC_ORDERER_BLOCK_SIZE=1, which makes mixed blocks — and therefore index
// holes — impossible. The matrix reports loudly when it detects that, so a run
// against the wrong fixture is obvious rather than silently degenerate.
//
// Both namespaces must exist on the network; start-x-batched creates them.
func TestNamespaceIsolationFabricX(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping FabricX namespace matrix in short mode")
	}
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr))

	runFileConfigNamespaceMatrix(t, "fabric-x/test-committer", "fabx.yaml", nsMatrixA, nsMatrixB)
}

// TestNamespaceIsolationFablo runs the matrix against a running Fablo network,
// covering classic fabric end to end — the only real backend that exercises
// nfab.NewSynchronizer rather than hybridx.
//
// A namespace on classic fabric is a chaincode, so the second one is declared
// in testdata/fablo/fablo-config.json (as "basic2", identical to "basic" but
// for the name) and the network has to be brought up with it present.
//
// fablo.yaml is a split-deployment config: org1 and org2 each run as a
// standalone endorser reached over gRPC, and the chaincode's
// AND('Org1MSP.member','Org2MSP.member') policy means both must endorse. So
// this goes through newSplitFileConfigHarness, as TestFablo does, rather than
// the embedded-endorser path the fabric-x config uses.
func TestNamespaceIsolationFablo(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Fablo namespace matrix in short mode")
	}
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr))

	endorserConfigs := []string{"fablo-org1.yaml", "fablo-org2.yaml"}

	build := func(ns, dbName string) *TestHarness {
		// Overriding Network.Namespace here reaches the served endorsers too:
		// newSplitFileConfigHarness passes the gateway's namespace down to each
		// of them, since an endorser builds its read-write sets under the
		// namespace it was constructed with and has to agree with its gateway.
		th, err := newSplitFileConfigHarness(t, TestLogger{T: t}, execution.EVMConfig{}, "",
			"fablo.yaml", endorserConfigs,
			map[string]any{
				"Network.Namespace":           ns,
				"Gateway.Database.ConnString": "file:" + dbName + "?mode=memory&cache=shared",
			})
		if err != nil {
			t.Fatalf("ns=%s: fablo harness: %v", ns, err)
		}
		t.Cleanup(func() { _ = th.Stop() })
		return th
	}

	thA := build(nsMatrixA, "nsmatrix-fablo-"+nsMatrixA+".db")
	thB := build(nsMatrixB, "nsmatrix-fablo-"+nsMatrixB+".db")

	parties := []nsParty{
		newParty(t, nsMatrixA, thA.Gateways[0], thA.ethChainConfig, nsMatrixSupply),
		newParty(t, nsMatrixB, thB.Gateways[0], thB.ethChainConfig, nsMatrixSupply),
	}
	assertDistinctStores(t, parties)

	runNamespaceIsolationMatrix(t, "fabric/fablo", parties)
}

// runFileConfigNamespaceMatrix brings up two harnesses from one config file,
// overriding only the namespace and the per-gateway store, then runs the
// matrix.
//
// Overriding rather than adding a second YAML keeps the two gateways
// identical in every respect but namespace, which is what makes a difference in
// their answers attributable to namespace handling. It also avoids the
// shared-cache trap: every committed config uses
// `file:gateway.db?mode=memory&cache=shared`, a *named* cache, so two gateways
// loading the same file would share one SQLite database and isolation would be
// untestable.
func runFileConfigNamespaceMatrix(t *testing.T, backend, configFile, nsA, nsB string) {
	t.Helper()

	build := func(ns, dbName string) *TestHarness {
		th, err := newFileConfigHarness(t, TestLogger{T: t}, execution.EVMConfig{}, "", configFile,
			map[string]any{
				"Network.Namespace":           ns,
				"Gateway.Database.ConnString": "file:" + dbName + "?mode=memory&cache=shared",
			})
		if err != nil {
			t.Fatalf("ns=%s: harness from %s: %v", ns, configFile, err)
		}
		t.Cleanup(func() { _ = th.Stop() })
		return th
	}

	thA := build(nsA, "nsmatrix-"+nsA+".db")
	thB := build(nsB, "nsmatrix-"+nsB+".db")

	parties := []nsParty{
		newParty(t, nsA, thA.Gateways[0], thA.ethChainConfig, nsMatrixSupply),
		newParty(t, nsB, thB.Gateways[0], thB.ethChainConfig, nsMatrixSupply),
	}
	assertDistinctStores(t, parties)

	runNamespaceIsolationMatrix(t, backend, parties)
}

// bypass mode is deliberately absent from this matrix. It runs no
// synchronizer at all, and its local.LocalSubmitter builds each block with a
// single namespace by construction
// (NsRWS: []blocks.NsReadWriteSet{{Namespace: s.namespace, ...}}), so it can
// neither carry a foreign transaction nor produce a mixed block. There is
// nothing for nsFilter to do there and nothing for this matrix to measure.
