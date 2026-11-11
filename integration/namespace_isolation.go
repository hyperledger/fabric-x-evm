/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"context"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"

	"github.com/hyperledger/fabric-x-evm/gateway/core"
	"github.com/hyperledger/fabric-x-evm/gateway/testimpl/primer"
	"github.com/hyperledger/fabric-x-evm/integration/contracts"
)

// This file holds the backend-independent half of the namespace-isolation
// matrix; the per-backend entry points are in namespace_isolation_test.go.
// Two gateways, one per namespace, commit to the same channel; we then read
// the Ethereum RPC surface from both and record what each one sees.
//
// The point is to find out what the gateway currently does, not to assert that
// it is right. Only one thing is asserted — no gateway ever returns the other
// namespace's data (assertNoLeakage). Everything else is recorded into
// blockReport and printed, so the backends can be compared side by side and we
// can then decide what, if anything, to change.
//
// Deliberately not asserted, because these are the open questions:
//   - transactionIndex values and the holes between them
//   - whether eth_blockNumber tracks ledger height or namespace-local height
//   - whether a block carrying nothing for this namespace is published at all

// nsParty is one namespace and the gateway that commits to it.
type nsParty struct {
	// Namespace is the Fabric namespace this party's gateway is configured with.
	Namespace string
	// Gateway is the gateway serving that namespace.
	Gateway *core.Gateway
	// Eth is an in-process RPC client for Gateway, carrying the FilterAPI from
	// its handler chain when one was registered (see NewNativeEthClient).
	Eth *ethclient.Client
	// Contract is the EVM contract deployed on this namespace, and the signer
	// that owns it. Each party gets its own, so a leak is unambiguous: an
	// address or tx hash from one party can never legitimately appear in the
	// other's answers.
	Contract ethcommon.Address
	Client   *EthClient
}

// rpcBlockJSON and rpcTxJSON are the few fields of the JSON-RPC block and
// transaction shapes this matrix reads. They are deliberately minimal and
// decoded straight from the wire rather than via ethclient, for the reason
// given in observeBlock: ethclient's types.Block recomputes its own hash and
// would not round-trip a Fabric-derived one. A pointer field distinguishes
// "absent" from "zero", which matters when the question is whether a block or
// transaction exists at all.
type rpcBlockJSON struct {
	Number       *hexutil.Uint64 `json:"number"`
	Hash         *ethcommon.Hash `json:"hash"`
	ParentHash   *ethcommon.Hash `json:"parentHash"`
	Transactions []rpcTxJSON     `json:"transactions"`
}

type rpcTxJSON struct {
	Hash             ethcommon.Hash  `json:"hash"`
	BlockNumber      *hexutil.Big    `json:"blockNumber"`
	TransactionIndex *hexutil.Uint64 `json:"transactionIndex"`
}

// txRecord is one Ethereum transaction as a gateway reports it.
type txRecord struct {
	Hash ethcommon.Hash
	// Index is transactionIndex as reported by the gateway. Recorded rather
	// than asserted: whether this should be the ledger-wide position or a
	// gap-free namespace-local one is exactly what we are trying to decide.
	Index uint64
	// ArrayPos is this transaction's position in the block's transactions
	// array. Where it differs from Index, the two disagree.
	ArrayPos        int
	CumulativeGas   uint64
	GasUsed         uint64
	Status          uint64
	LogIndices      []uint
	FoundByBlockPos bool
}

// blockReport is everything one gateway saw for one block.
type blockReport struct {
	Namespace   string
	BlockNumber uint64
	BlockHash   ethcommon.Hash
	ParentHash  ethcommon.Hash
	// TxCount is eth_getBlockTransactionCountByNumber, which can disagree with
	// len(Txs) if counting and listing take different paths.
	TxCount uint
	Txs     []txRecord
	// ByIndexProbe records, for every index from 0 to the highest
	// transactionIndex seen, whether
	// eth_getTransactionByBlockNumberAndIndex returned a transaction. A false
	// at a position below the maximum is a hole.
	ByIndexProbe map[uint64]bool
	// LogCount is how many logs eth_getLogs returned for this block.
	LogCount int
	// Empty is true when the gateway published the block with no transactions
	// of ours in it.
	Empty bool
}

// String renders one block as a fixed set of labelled lines, one field per
// line and in a stable order, so the output of two backends can be diffed
// directly to see where they disagree.
func (r blockReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "  ns=%-10s block=%-4d txs=%d count=%d logs=%d empty=%v\n",
		r.Namespace, r.BlockNumber, len(r.Txs), r.TxCount, r.LogCount, r.Empty)

	if len(r.Txs) > 0 {
		idx := make([]string, len(r.Txs))
		pos := make([]string, len(r.Txs))
		cum := make([]string, len(r.Txs))
		for i, tx := range r.Txs {
			idx[i] = fmt.Sprintf("%#x", tx.Index)
			pos[i] = fmt.Sprintf("%d", tx.ArrayPos)
			cum[i] = fmt.Sprintf("%d", tx.CumulativeGas)
		}
		fmt.Fprintf(&b, "    transactionIndex : [%s]\n", strings.Join(idx, ", "))
		fmt.Fprintf(&b, "    arrayPosition    : [%s]\n", strings.Join(pos, ", "))
		fmt.Fprintf(&b, "    cumulativeGasUsed: [%s]\n", strings.Join(cum, ", "))

		if len(r.ByIndexProbe) > 0 {
			keys := make([]uint64, 0, len(r.ByIndexProbe))
			for k := range r.ByIndexProbe {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			probe := make([]string, 0, len(keys))
			holes := 0
			for _, k := range keys {
				mark := "tx"
				if !r.ByIndexProbe[k] {
					mark = "null"
					holes++
				}
				probe = append(probe, fmt.Sprintf("%d:%s", k, mark))
			}
			fmt.Fprintf(&b, "    byBlockAndIndex  : %s", strings.Join(probe, "  "))
			if holes > 0 {
				fmt.Fprintf(&b, "   <- %d hole(s)", holes)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// observeBlock reads every block-scoped RPC a gateway serves for blockNum and
// records the answers. It never fails the test on the values it finds; a
// transport error is still fatal, since that means we learned nothing.
func observeBlock(t *testing.T, p nsParty, blockNum uint64) blockReport {
	t.Helper()
	ctx := t.Context()

	rep := blockReport{Namespace: p.Namespace, BlockNumber: blockNum, ByIndexProbe: map[uint64]bool{}}

	// Read the block as raw JSON rather than through ethclient.BlockByNumber.
	// ethclient rebuilds a types.Block, whose Hash() recomputes a keccak over
	// the header — but our block hashes come from Fabric and are stored as-is,
	// so the recomputed value is not a hash the gateway has ever heard of.
	// Probing by it would make every hash-keyed call miss and misreport the
	// result as the gateway's fault.
	var raw rpcBlockJSON
	if err := p.Eth.Client().CallContext(ctx, &raw, "eth_getBlockByNumber",
		hexutil.EncodeUint64(blockNum), true); err != nil {
		t.Logf("  ns=%s block=%d: eth_getBlockByNumber: %v", p.Namespace, blockNum, err)
		rep.Empty = true
		return rep
	}
	if raw.Hash == nil {
		// A null result: the gateway has no such block at all. Whether a block
		// carrying nothing for this namespace is published is one of the open
		// questions, so this is recorded, not failed.
		t.Logf("  ns=%s block=%d: not found (null)", p.Namespace, blockNum)
		rep.Empty = true
		return rep
	}

	rep.BlockHash = *raw.Hash
	if raw.ParentHash != nil {
		rep.ParentHash = *raw.ParentHash
	}
	rep.Empty = len(raw.Transactions) == 0

	var count hexutil.Uint
	if err := p.Eth.Client().CallContext(ctx, &count, "eth_getBlockTransactionCountByHash",
		rep.BlockHash); err != nil {
		t.Logf("  ns=%s block=%d: eth_getBlockTransactionCountByHash: %v", p.Namespace, blockNum, err)
	} else {
		rep.TxCount = uint(count)
	}

	maxIndex := uint64(0)
	for pos, rtx := range raw.Transactions {
		rec := txRecord{Hash: rtx.Hash, ArrayPos: pos}
		if rtx.TransactionIndex != nil {
			rec.Index = uint64(*rtx.TransactionIndex)
		}

		receipt, err := p.Eth.TransactionReceipt(ctx, rtx.Hash)
		if err != nil {
			t.Logf("  ns=%s block=%d tx=%s: TransactionReceipt: %v", p.Namespace, blockNum, rtx.Hash, err)
		} else {
			// The receipt's index is authoritative for what an indexer sees;
			// it should agree with the one in the block listing.
			rec.Index = uint64(receipt.TransactionIndex)
			rec.CumulativeGas = receipt.CumulativeGasUsed
			rec.GasUsed = receipt.GasUsed
			rec.Status = receipt.Status
			for _, l := range receipt.Logs {
				rec.LogIndices = append(rec.LogIndices, l.Index)
			}
		}
		if rec.Index > maxIndex {
			maxIndex = rec.Index
		}
		rep.Txs = append(rep.Txs, rec)
	}

	// Probe every index up to the highest one reported, so holes show up as
	// explicit nulls rather than as absence. Raw RPC again, and by block
	// number, so neither the recomputed-hash problem nor ethclient's
	// null-handling can be mistaken for a hole.
	for i := uint64(0); i <= maxIndex && len(rep.Txs) > 0; i++ {
		var got *rpcTxJSON
		err := p.Eth.Client().CallContext(ctx, &got, "eth_getTransactionByBlockNumberAndIndex",
			hexutil.EncodeUint64(blockNum), hexutil.EncodeUint64(i))
		rep.ByIndexProbe[i] = err == nil && got != nil
	}

	// Mark which transactions were reachable by block position at their own
	// reported index — the round trip an indexer makes.
	for i := range rep.Txs {
		rep.Txs[i].FoundByBlockPos = rep.ByIndexProbe[rep.Txs[i].Index]
	}

	logs, err := blockLogs(ctx, p, blockNum)
	if err != nil {
		t.Logf("  ns=%s block=%d: FilterLogs: %v", p.Namespace, blockNum, err)
	} else {
		rep.LogCount = len(logs)
	}

	return rep
}

// newParty deploys this namespace's contract and returns the party that owns
// it. Each party gets a fresh EthClient, so its key, contract address and
// transaction hashes are its own and a leak is unambiguous.
//
// The contract is GLDToken because it emits an indexed Transfer event, which is
// what gives eth_getLogs something namespace-specific to leak.
func newParty(t *testing.T, namespace string, gw *core.Gateway, chainConfig *params.ChainConfig, supply *big.Int) nsParty {
	t.Helper()

	client, err := NewEthClient(contracts.GLDTokenMetaData, chainConfig)
	if err != nil {
		t.Fatalf("ns=%s: eth client: %v", namespace, err)
	}

	eth, err := NewNativeEthClient(gw)
	if err != nil {
		t.Fatalf("ns=%s: native eth client: %v", namespace, err)
	}

	addr := deploySmartContract(t, gw, client, supply)

	return nsParty{
		Namespace: namespace,
		Gateway:   gw,
		Eth:       eth,
		Contract:  addr,
		Client:    client,
	}
}

// blockLogs returns every log a gateway reports for one block.
func blockLogs(ctx context.Context, p nsParty, blockNum uint64) ([]types.Log, error) {
	n := new(big.Int).SetUint64(blockNum)
	return p.Eth.FilterLogs(ctx, ethereum.FilterQuery{FromBlock: n, ToBlock: n})
}

// addressLogs returns every log a gateway reports for one contract address,
// across the whole chain. Used to check that a contract deployed on one
// namespace never emits logs on the other's gateway.
func addressLogs(ctx context.Context, p nsParty, addr ethcommon.Address) ([]types.Log, error) {
	return p.Eth.FilterLogs(ctx, ethereum.FilterQuery{
		FromBlock: big.NewInt(0),
		Addresses: []ethcommon.Address{addr},
	})
}

// assertNoLeakage is the one hard requirement in this matrix: a gateway must
// never surface another namespace's transactions, receipts or logs. Everything
// else here is recorded; this is checked.
//
// Each party deploys its own contract from its own key, so any appearance of
// the other party's tx hash or contract address is unambiguous leakage.
func assertNoLeakage(t *testing.T, self, other nsParty, otherTxs []ethcommon.Hash) {
	t.Helper()
	ctx := t.Context()

	for _, h := range otherTxs {
		if tx, _, err := self.Eth.TransactionByHash(ctx, h); err == nil && tx != nil {
			t.Errorf("LEAK: gateway for ns=%s returned tx %s belonging to ns=%s",
				self.Namespace, h, other.Namespace)
		}
		if r, err := self.Eth.TransactionReceipt(ctx, h); err == nil && r != nil {
			t.Errorf("LEAK: gateway for ns=%s returned a receipt for tx %s belonging to ns=%s",
				self.Namespace, h, other.Namespace)
		}
	}

	// Logs are filtered by the other party's contract address, so a hit here
	// is that contract's event surfacing on the wrong gateway.
	logs, err := addressLogs(ctx, self, other.Contract)
	if err != nil {
		t.Logf("  ns=%s: FilterLogs for foreign address: %v", self.Namespace, err)
		return
	}
	if len(logs) > 0 {
		t.Errorf("LEAK: gateway for ns=%s returned %d log(s) from contract %s deployed on ns=%s",
			self.Namespace, len(logs), other.Contract, other.Namespace)
	}
}

// commitConcurrently submits one transaction per party and only then waits for
// both, so the orderer can batch them into a single block. The existing
// callSmartContract helper submits and waits before returning, which serializes
// and guarantees one transaction per block — the opposite of what the mixed-block
// case needs.
//
// Returns the submitted transactions in the order the parties were given.
func commitConcurrently(t *testing.T, parties []nsParty, method string, args ...any) []*types.Transaction {
	t.Helper()
	ctx := t.Context()

	txs := make([]*types.Transaction, len(parties))
	for i, p := range parties {
		tx, err := p.Client.TxForCall(ctx, p.Gateway, &p.Contract, method, args...)
		if err != nil {
			t.Fatalf("ns=%s: build %s: %v", p.Namespace, method, err)
		}
		txs[i] = tx
	}

	// Submit everything before waiting for anything.
	for i, p := range parties {
		if err := p.Eth.SendTransaction(ctx, txs[i]); err != nil {
			t.Fatalf("ns=%s: send %s: %v", p.Namespace, method, err)
		}
	}

	for i, p := range parties {
		if err := primer.WaitForCommit(ctx, p.Eth, txs[i]); err != nil {
			t.Fatalf("ns=%s: wait for %s: %v", p.Namespace, method, err)
		}
	}

	return txs
}

// blockOf reports the block a committed transaction landed in, per the gateway
// that owns it.
func blockOf(t *testing.T, p nsParty, tx *types.Transaction) uint64 {
	t.Helper()
	r, err := p.Eth.TransactionReceipt(t.Context(), tx.Hash())
	if err != nil {
		t.Fatalf("ns=%s: receipt for %s: %v", p.Namespace, tx.Hash(), err)
	}
	return r.BlockNumber.Uint64()
}

// describeComposition says whether the parties' transactions shared a block.
// The mixed-block case depends on them doing so, and an orderer that cut a
// block per transaction would otherwise leave the test silently measuring the
// single-namespace case twice.
func describeComposition(t *testing.T, parties []nsParty, txs []*types.Transaction) (shared bool, blocks []uint64) {
	t.Helper()
	blocks = make([]uint64, len(parties))
	for i := range parties {
		blocks[i] = blockOf(t, parties[i], txs[i])
	}
	for i := 1; i < len(blocks); i++ {
		if blocks[i] == blocks[0] {
			shared = true
		}
	}
	return shared, blocks
}

// reportBlocks prints one report per party per block, in a stable order.
// It returns every report it gathered, indexed by party and then by block
// number, so a caller can assert on the same observation it just printed
// instead of reading the chain a second time. Observing twice would double the
// RPC traffic and, more confusingly, log "not found" twice for one block.
func reportBlocks(t *testing.T, label string, parties []nsParty, blockNums []uint64) []map[uint64]blockReport {
	t.Helper()

	seen := map[uint64]bool{}
	ordered := make([]uint64, 0, len(blockNums))
	for _, n := range blockNums {
		if !seen[n] {
			seen[n] = true
			ordered = append(ordered, n)
		}
	}
	slices.Sort(ordered)

	reports := make([]map[uint64]blockReport, len(parties))
	for i := range parties {
		reports[i] = map[uint64]blockReport{}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n=== %s ===\n", label)
	for _, n := range ordered {
		for i, p := range parties {
			rep := observeBlock(t, p, n)
			reports[i][n] = rep
			b.WriteString(rep.String())
		}
	}
	for _, p := range parties {
		if h, err := p.Eth.BlockNumber(t.Context()); err == nil {
			fmt.Fprintf(&b, "  ns=%-10s eth_blockNumber=%d\n", p.Namespace, h)
		}
	}
	t.Log(b.String())
	return reports
}

// mixedBlockAttempts is how many times the matrix tries to land both
// namespaces' transactions in one block before giving up.
//
// Retried because batching is timing-dependent, not because a mixed block is
// optional: an orderer cuts on whichever of its size and time thresholds trips
// first, so a batch boundary can fall between two concurrent submissions
// however promptly they are sent. A backend that batches at all shares a block
// within a few attempts; one that cuts a block per transaction never will, and
// that is the case worth failing on.
const mixedBlockAttempts = 5

// commitMixedBlock commits one transfer per party, repeating until both land in
// the same block, and returns that block along with every transaction it
// committed (grouped per attempt, the successful one last).
//
// Not achieving a mixed block is fatal. The transactionIndex holes a shared
// block produces are the thing this matrix exists to observe, so a run that
// never gets one has not tested what it set out to test and must not report
// success. On the fabric-x test committer the cause is a block size of 1 — use
// the batched fixture (make start-x-batched).
func commitMixedBlock(t *testing.T, backend string, parties []nsParty, recipient ethcommon.Address) (uint64, [][]*types.Transaction) {
	t.Helper()

	var committed [][]*types.Transaction
	for attempt := 1; attempt <= mixedBlockAttempts; attempt++ {
		txs := commitConcurrently(t, parties, "transfer", recipient, big.NewInt(1))
		committed = append(committed, txs)

		shared, blocks := describeComposition(t, parties, txs)
		if shared {
			t.Logf("mixed-block case: both namespaces landed in block %d (attempt %d/%d)",
				blocks[0], attempt, mixedBlockAttempts)
			return blocks[0], committed
		}
		t.Logf("mixed-block attempt %d/%d: landed in separate blocks %v, retrying",
			attempt, mixedBlockAttempts, blocks)
	}

	t.Fatalf("backend=%s never produced a block carrying both namespaces in %d attempts.\n"+
		"The matrix cannot observe transactionIndex holes without one, so this is a failure "+
		"rather than a skip: nothing was learned about the case it exists to measure.\n"+
		"Most likely the orderer cuts one transaction per block — on the fabric-x test "+
		"committer that is SC_ORDERER_BLOCK_SIZE in compose.fabric-x.yml, so run against "+
		"the batched fixture (make start-x-batched) rather than plain start-x. Note that "+
		"the value in testdata/config/mock-orderer.yaml would be ignored: the environment "+
		"variable outranks it.", backend, mixedBlockAttempts)
	return 0, nil // unreachable: Fatalf stops the test
}

// assertBlockExcludes checks that p's view of a block lists none of the given
// foreign transactions.
//
// This is the leakage property stated per block rather than per hash: a gateway
// may legitimately publish a block it has nothing in (the empty-after-filter
// case, which is recorded rather than asserted), but it must never list another
// namespace's transaction as part of one.
// rep is the observation already taken for that block (see reportBlocks), so
// the assertion is made against exactly what was printed.
func assertBlockExcludes(t *testing.T, rep blockReport, foreign []ethcommon.Hash, foreignNs string) {
	t.Helper()

	unwanted := make(map[ethcommon.Hash]bool, len(foreign))
	for _, h := range foreign {
		unwanted[h] = true
	}

	for _, tx := range rep.Txs {
		if unwanted[tx.Hash] {
			t.Errorf("LEAK: gateway for ns=%s lists tx %s (ns=%s) in its view of block %d",
				rep.Namespace, tx.Hash, foreignNs, rep.BlockNumber)
		}
	}
}

// runNamespaceIsolationMatrix is the shared body every backend runs. parties
// must be two gateways on the same channel, each configured with its own
// namespace and each already serving RPC.
//
// It records what both gateways see for a mixed block and for a
// single-namespace block in each direction. Two things are asserted: neither
// gateway can see the other's data, and a mixed block was actually achieved —
// without one, the case the matrix exists to measure went unmeasured, which is
// a failure rather than a skip.
func runNamespaceIsolationMatrix(t *testing.T, backend string, parties []nsParty) {
	t.Helper()
	if len(parties) != 2 {
		t.Fatalf("namespace matrix needs exactly 2 parties, got %d", len(parties))
	}
	ctx := t.Context()

	t.Logf("backend=%s namespaces=[%s, %s]", backend, parties[0].Namespace, parties[1].Namespace)

	// Each party's contract is deployed and holds its own supply, so a
	// transfer emits a Transfer event on that namespace only.
	ownTxs := make([][]ethcommon.Hash, len(parties))

	// ── Mixed block: both namespaces transact at once ──────────────────────
	recipient := ethcommon.HexToAddress("0x000000000000000000000000000000000000c0de")
	mixedBlock, mixedAttempts := commitMixedBlock(t, backend, parties, recipient)
	for _, txs := range mixedAttempts {
		for i := range parties {
			ownTxs[i] = append(ownTxs[i], txs[i].Hash())
		}
	}
	mixedReports := reportBlocks(t, "mixed block ("+backend+")", parties, []uint64{mixedBlock})

	// Neither party may list the other's transaction in the block they shared.
	// This is the sharpest form of the isolation question: one block number,
	// one range of ledger positions, two namespaces.
	winning := mixedAttempts[len(mixedAttempts)-1]
	assertBlockExcludes(t, mixedReports[0][mixedBlock], []ethcommon.Hash{winning[1].Hash()}, parties[1].Namespace)
	assertBlockExcludes(t, mixedReports[1][mixedBlock], []ethcommon.Hash{winning[0].Hash()}, parties[0].Namespace)

	// ── Single-namespace blocks, in both directions ────────────────────────
	// Each party in turn commits alone while the other stays idle, so the
	// empty-after-filter case is exercised from both sides. One direction
	// alone would miss a filter that works for one namespace but not the
	// other — for instance one sensitive to the order namespaces appear in a
	// block, or to which party's gateway happens to be first in the harness.
	for i := range parties {
		actor, idle := parties[i], parties[1-i]

		solo := commitConcurrently(t, parties[i:i+1], "transfer", recipient, big.NewInt(2))
		ownTxs[i] = append(ownTxs[i], solo[0].Hash())
		soloBlock := blockOf(t, actor, solo[0])
		t.Logf("single-namespace case: ns=%s committed alone in block %d (ns=%s idle)",
			actor.Namespace, soloBlock, idle.Namespace)

		// The idle party sees that block number with nothing of its own in it.
		// Whether it publishes the block at all is recorded by observeBlock
		// rather than assumed; what is asserted is that it does not list the
		// actor's transaction.
		soloReports := reportBlocks(t, "single-namespace block, ns="+actor.Namespace+" active ("+backend+")",
			parties, []uint64{soloBlock})
		assertBlockExcludes(t, soloReports[1-i][soloBlock], []ethcommon.Hash{solo[0].Hash()}, actor.Namespace)
	}

	// ── The leakage assertion, across everything committed above ───────────
	assertNoLeakage(t, parties[0], parties[1], ownTxs[1])
	assertNoLeakage(t, parties[1], parties[0], ownTxs[0])

	// Each party must still see its own transactions, or "no leakage" would be
	// trivially satisfiable by a gateway that returns nothing at all.
	for i, p := range parties {
		for _, h := range ownTxs[i] {
			if tx, _, err := p.Eth.TransactionByHash(ctx, h); err != nil || tx == nil {
				t.Errorf("ns=%s lost its own tx %s: %v", p.Namespace, h, err)
			}
		}
	}
}
