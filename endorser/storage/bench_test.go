/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage_test

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"text/tabwriter"
	"time"

	"github.com/hyperledger/fabric-x-evm/endorser/storage"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/state"
)

const (
	benchChannel   = "bench"
	benchNamespace = "evm"
)

// benchValue is a fixed 32-byte storage word reused across all writes to
// avoid allocation overhead polluting the DB benchmarks.
var benchValue = [32]byte{
	1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
	17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32,
}

// hexKey formats a uint64 as a 64-character hex string.
func hexKey(i uint64) string {
	return fmt.Sprintf("%064x", i)
}

// keyGen generates key indices using Zipf distribution (s=1.2), modelling
// access patterns where a small number of keys dominate reads.
type keyGen struct {
	rng     *rand.Rand
	zipf    *rand.Zipf
	numKeys uint64
}

func newKeyGen(numKeys uint64) *keyGen {
	rng := rand.New(rand.NewSource(42))
	return &keyGen{rng: rng, zipf: rand.NewZipf(rng, 1.2, 1.0, numKeys-1), numKeys: numKeys}
}

func (kg *keyGen) next() uint64 {
	return kg.zipf.Uint64()
}

// --- Backend table ---

type benchBackend struct {
	name   string
	newKVS func(b *testing.B) storage.KVS
}

func allBackends() []benchBackend {
	return []benchBackend{
		{
			name: "memory",
			newKVS: func(b *testing.B) storage.KVS {
				b.Helper()
				kvs := storage.NewLightKVS(128)
				b.Cleanup(func() { _ = kvs.Close() })
				return kvs
			},
		},
		{
			name: "pebble",
			newKVS: func(b *testing.B) storage.KVS {
				b.Helper()
				kvs, err := storage.NewPebbleKVS(b.TempDir(), 128)
				if err != nil {
					b.Fatalf("new pebble kvs: %v", err)
				}
				b.Cleanup(func() { _ = kvs.Close() })
				return kvs
			},
		},
		{
			name: "sqlite:file",
			newKVS: func(b *testing.B) storage.KVS {
				b.Helper()
				connStr := fmt.Sprintf("file:%s/bench.db", b.TempDir())
				db, err := state.NewWriteDB(benchChannel, connStr)
				if err != nil {
					b.Skipf("sqlite unavailable (likely missing cgo): %v", err)
					return nil
				}
				kvs := storage.NewVersionedDBWrapper(db)
				b.Cleanup(func() { _ = kvs.Close() })
				return kvs
			},
		},
	}
}

// --- Data helpers ---

// makeBlock builds a block with txCount valid transactions each containing writesPerTx writes.
// Keys are drawn from kg and deduplicated within each transaction to satisfy the
// PRIMARY KEY constraint (channel, namespace, key, version_block, version_tx).
func makeBlock(blockNum uint64, txCount, writesPerTx int, kg *keyGen) blocks.Block {
	txs := make([]blocks.Transaction, txCount)
	for i := range txs {
		seen := make(map[uint64]struct{}, writesPerTx)
		writes := make([]blocks.KVWrite, 0, writesPerTx)
		for len(writes) < writesPerTx {
			idx := kg.next()
			if _, dup := seen[idx]; dup {
				continue
			}
			seen[idx] = struct{}{}
			writes = append(writes, blocks.KVWrite{Key: hexKey(idx), Value: benchValue[:]})
		}
		txs[i] = blocks.Transaction{
			ID:     fmt.Sprintf("%d-%d", blockNum, i),
			Number: int64(i),
			Valid:  true,
			NsRWS: []blocks.NsReadWriteSet{{
				Namespace: benchNamespace,
				RWS:       blocks.ReadWriteSet{Writes: writes},
			}},
		}
	}
	return blocks.Block{Number: blockNum, Transactions: txs}
}

// preloadDB inserts numKeys unique keys into kvs in batches of writesPerBlock per block.
// Call before b.ResetTimer() to exclude setup from measurements.
func preloadDB(b *testing.B, kvs storage.KVS, numKeys, writesPerBlock int) {
	b.Helper()
	ctx := context.Background()
	blockNum := uint64(0)
	for offset := 0; offset < numKeys; offset += writesPerBlock {
		count := writesPerBlock
		if offset+count > numKeys {
			count = numKeys - offset
		}
		writes := make([]blocks.KVWrite, count)
		for j := range writes {
			writes[j] = blocks.KVWrite{Key: hexKey(uint64(offset + j)), Value: benchValue[:]}
		}
		bl := blocks.Block{
			Number: blockNum,
			Transactions: []blocks.Transaction{{
				ID:     fmt.Sprintf("preload-%d", blockNum),
				Number: 0,
				Valid:  true,
				NsRWS: []blocks.NsReadWriteSet{{
					Namespace: benchNamespace,
					RWS:       blocks.ReadWriteSet{Writes: writes},
				}},
			}},
		}
		if err := kvs.Handle(ctx, bl); err != nil {
			b.Fatalf("preload block %d: %v", blockNum, err)
		}
		blockNum++
	}
}

// --- Summary table helper ---

// report collects (row, col) → value pairs during a benchmark run and prints a
// formatted table to the benchmark log after all sub-benchmarks complete.
type report struct {
	title string
	rows  []string
	cols  []string
	cells map[string]map[string]string
}

func newReport(title string) *report {
	return &report{title: title, cells: make(map[string]map[string]string)}
}

func (r *report) set(row, col, val string) {
	if _, ok := r.cells[row]; !ok {
		r.cells[row] = make(map[string]string)
		r.rows = append(r.rows, row)
	}
	if !slices.Contains(r.cols, col) {
		r.cols = append(r.cols, col)
	}
	r.cells[row][col] = val
}

func (r *report) log() {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 3, ' ', 0)

	// header
	fmt.Fprintf(w, "\nbackend\t")
	for _, col := range r.cols {
		fmt.Fprintf(w, "%s\t", col)
	}
	fmt.Fprintln(w)

	// rows
	for _, row := range r.rows {
		fmt.Fprintf(w, "%s\t", row)
		for _, col := range r.cols {
			fmt.Fprintf(w, "%s\t", r.cells[row][col])
		}
		fmt.Fprintln(w)
	}
	w.Flush()

	fmt.Fprintln(os.Stdout, "\n── "+r.title+" "+strings.Repeat("─", max(0, 60-len(r.title)))+buf.String())
}

// --- Benchmarks ---

// BenchmarkUpdateWorldState measures write throughput through Handle.
// b.N counts blocks processed. Reports tx/s and writes/s per backend.
//
// Run: go test -bench=BenchmarkUpdateWorldState -benchtime=5s ./endorser/storage/
func BenchmarkUpdateWorldState(b *testing.B) {
	type cfg struct{ txs, writes int }
	configs := []cfg{
		{50, 3},  // ERC-20-realistic: 3 writes/tx (nonce + 2 token slots)
		{100, 3}, // ERC-20-realistic, larger block
		{50, 5},
		{100, 5},
		{100, 20}, // heavier contracts
	}

	backends := allBackends()
	rep := newReport("UpdateWorldState  (tx/s | writes/s)")
	for _, be := range backends {
		b.Run(be.name, func(b *testing.B) {
			for _, c := range configs {
				col := fmt.Sprintf("txs=%d/w=%d", c.txs, c.writes)
				var txPerS, writesPerS float64
				b.Run(col, func(b *testing.B) {
					kvs := be.newKVS(b)
					if kvs == nil {
						return // skipped
					}
					kg := newKeyGen(100_000)
					ctx := context.Background()

					b.ResetTimer()
					b.ReportAllocs()
					for i := range b.N {
						if err := kvs.Handle(ctx, makeBlock(uint64(i), c.txs, c.writes, kg)); err != nil {
							b.Fatal(err)
						}
					}
					elapsed := b.Elapsed().Seconds()
					txPerS = float64(b.N*c.txs) / elapsed
					writesPerS = float64(b.N*c.txs*c.writes) / elapsed
					b.ReportMetric(txPerS, "tx/s")
					b.ReportMetric(writesPerS, "writes/s")
				})
				rep.set(be.name, col, fmt.Sprintf("%s | %s", fmtRate(txPerS), fmtRate(writesPerS)))
			}
		})
	}
	rep.log()
}

// BenchmarkUpdateWorldStateAtScale measures write throughput as a function of
// *pre-existing state size*, isolating the cost model difference: LightKVS clones
// the entire state map on every block — O(state size) per block — while PebbleKVS is
// O(1) per write and flat in state size. This finds the crossover where the
// in-memory clone cost overtakes pebble's per-write cost, bounding the
// "keep the LightKVS" approach.
//
// Blocks are ERC-20-realistic (100 tx × 3 writes). State is bulk-loaded in large
// blocks (numKeys/10 writes each) so the preload's own clone cost stays
// O(n²/blocks) rather than the O(n²) of small-block loading.
//
// Run: go test -bench=BenchmarkUpdateWorldStateAtScale -benchtime=20x ./endorser/storage/
func BenchmarkUpdateWorldStateAtScale(b *testing.B) {
	const txs, writes = 100, 3
	sizes := []struct {
		n     int
		label string
	}{
		{100_000, "100K"},
		{1_000_000, "1M"},
		{5_000_000, "5M"},
		{10_000_000, "10M"},
	}
	// memory and pebble are the two fundamentally different cost models.
	base := allBackends()
	backends := []benchBackend{base[0], base[1]}

	rep := newReport("UpdateWorldState @ scale  (blocks/s | writes/s)  —  100tx×3w")
	for _, be := range backends {
		b.Run(be.name, func(b *testing.B) {
			for _, sz := range sizes {
				var blocksPerS, writesPerS float64
				b.Run(sz.label, func(b *testing.B) {
					kvs := be.newKVS(b)
					if kvs == nil {
						return
					}
					writesPerBlock := sz.n / 10
					preloadDB(b, kvs, sz.n, writesPerBlock)
					// Continue block numbering contiguously after the preload.
					startBlock := uint64(sz.n / writesPerBlock)
					kg := newKeyGen(uint64(sz.n))
					ctx := context.Background()

					b.ResetTimer()
					for i := range b.N {
						if err := kvs.Handle(ctx, makeBlock(startBlock+uint64(i), txs, writes, kg)); err != nil {
							b.Fatal(err)
						}
					}
					elapsed := b.Elapsed().Seconds()
					blocksPerS = float64(b.N) / elapsed
					writesPerS = float64(b.N*txs*writes) / elapsed
					b.ReportMetric(blocksPerS, "blocks/s")
					b.ReportMetric(writesPerS, "writes/s")
				})
				rep.set(be.name, sz.label, fmt.Sprintf("%s | %s", fmtRate(blocksPerS), fmtRate(writesPerS)))
			}
		})
	}
	rep.log()
}

// BenchmarkGetState measures raw KVS.Get latency against a pre-populated DB.
//
// Run: go test -bench=BenchmarkGetState -benchtime=5s ./endorser/storage/
func BenchmarkGetState(b *testing.B) {
	const (
		numKeys          = 100_000
		writesPerBlock   = 100
		preloadLastBlock = uint64(numKeys/writesPerBlock - 1)
	)

	rep := newReport("GetState  (ns/op | reads/s)")
	for _, be := range allBackends() {
		var nsPerOp float64
		b.Run(be.name, func(b *testing.B) {
			kvs := be.newKVS(b)
			if kvs == nil {
				return // skipped
			}
			preloadDB(b, kvs, numKeys, writesPerBlock)
			kg := newKeyGen(numKeys)

			b.ResetTimer()
			b.ReportAllocs()
			for range b.N {
				if _, err := kvs.Get(benchNamespace, hexKey(kg.next()), preloadLastBlock); err != nil {
					b.Fatal(err)
				}
			}
			nsPerOp = float64(b.Elapsed().Nanoseconds()) / float64(b.N)
		})
		rep.set(be.name, "ns/op", fmt.Sprintf("%.0f ns", nsPerOp))
		rep.set(be.name, "reads/s", fmtRate(1e9/nsPerOp))
	}
	rep.log()
}

// BenchmarkSimulation benchmarks snapshot-based reads — the hot path for
// transaction endorsement. Each b.N iteration models one transaction: create a
// snapshot via NewSnapshot, perform readsPerSim Get calls, and close the reader.
//
// Note: The SDK's SimulationStore also performs 5 PutState writes per simulation,
// but the KVS interface applies writes at block granularity via Handle, not
// per-transaction. This benchmark focuses on the read path; writes are measured
// in BenchmarkUpdateWorldState.
//
// Run: go test -bench=BenchmarkSimulation -benchtime=5s ./endorser/storage/
func BenchmarkSimulation(b *testing.B) {
	const (
		numKeys          = 100_000
		writesPerBlock   = 100
		preloadLastBlock = uint64(numKeys/writesPerBlock - 1)
	)

	rep := newReport("Simulation  (sim/s | reads/s)  —  snapshot-based reads only")
	for _, be := range allBackends() {
		b.Run(be.name, func(b *testing.B) {
			kvs := be.newKVS(b)
			if kvs == nil {
				return // skipped
			}
			preloadDB(b, kvs, numKeys, writesPerBlock)

			for _, readsPerSim := range []int{20, 50, 100} {
				col := fmt.Sprintf("reads=%d", readsPerSim)
				var simPerS, readsPerS float64
				b.Run(col, func(b *testing.B) {
					kg := newKeyGen(numKeys)

					b.ResetTimer()
					b.ReportAllocs()
					for range b.N {
						reader, err := kvs.NewSnapshot(preloadLastBlock)
						if err != nil {
							b.Fatal(err)
						}
						for range readsPerSim {
							if _, err := reader.Get(benchNamespace, hexKey(kg.next())); err != nil {
								b.Fatal(err)
							}
						}
						reader.Close()
					}

					elapsed := b.Elapsed().Seconds()
					simPerS = float64(b.N) / elapsed
					readsPerS = float64(b.N*readsPerSim) / elapsed
					b.ReportMetric(simPerS, "sim/s")
					b.ReportMetric(readsPerS, "reads/s")
				})
				rep.set(be.name, col, fmt.Sprintf("%s | %s", fmtRate(simPerS), fmtRate(readsPerS)))
			}
		})
	}
	rep.log()
}

// BenchmarkMixed measures read latency under concurrent write pressure — the closest
// approximation to production load. A background goroutine writes blocks at ~2K tx/sec
// while the benchmark loop drives snapshot reads.
//
// 500 extra blocks are written synchronously during setup (before b.ResetTimer) so that
// every calibration iteration sees the same large DB. Without this pre-population the
// first calibration iteration is fast (small DB), Go estimates a large b.N, and then all
// those iterations run against a much larger DB — causing runtimes up to 10× longer than
// -benchtime.
//
// Run: go test -bench=BenchmarkMixed -benchtime=5s ./endorser/storage/
func BenchmarkMixed(b *testing.B) {
	const (
		numKeys            = 100_000
		writesPerBlock     = 100
		readsPerSim        = 50
		extraBlocks        = 500                   // written during setup: 500 × 50 tx × 5 writes = 125K rows
		writerTickInterval = 50 * time.Millisecond // 20 blocks/sec ≈ 1000 tps
	)

	rep := newReport("Mixed  (reads/s under concurrent writes at ~1K tps)")
	for _, be := range allBackends() {
		var readsPerS float64
		// Outer b.Run calls an inner b.Run, so the framework runs this function exactly
		// once (no calibration re-runs). This keeps exactly one background writer alive
		// and avoids repeated preload attempts against the same in-memory database.
		b.Run(be.name, func(b *testing.B) {
			kvs := be.newKVS(b)
			if kvs == nil {
				return // skipped
			}
			preloadDB(b, kvs, numKeys, writesPerBlock)

			// Write extra blocks during setup so calibration always starts against a
			// stable, production-sized DB (100K preloaded + 125K extra = 225K rows).
			setupCtx := context.Background()
			setupKg := newKeyGen(numKeys)
			baseBlock := uint64(numKeys / writesPerBlock)
			for i := range extraBlocks {
				if err := kvs.Handle(setupCtx, makeBlock(baseBlock+uint64(i), 50, 5, setupKg)); err != nil {
					b.Fatal(err)
				}
			}

			var lastBlock atomic.Uint64
			lastBlock.Store(baseBlock + extraBlocks - 1)

			// Force GC to collect allocations from setup before measurement starts.
			runtime.GC()

			ctx, cancel := context.WithCancel(context.Background())
			b.Cleanup(cancel)

			// Background writer keeps running during measurement to simulate live commits.
			go func() {
				ticker := time.NewTicker(writerTickInterval)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						n := lastBlock.Add(1)
						_ = kvs.Handle(ctx, makeBlock(n, 50, 5, setupKg))
					}
				}
			}()

			// Single-goroutine reader: each snapshot releases before the next one begins,
			// giving the writer time to commit between bursts.
			b.Run("sim", func(b *testing.B) {
				kg := newKeyGen(numKeys)
				b.ResetTimer()
				for range b.N {
					n := lastBlock.Load()
					reader, err := kvs.NewSnapshot(n)
					if err != nil {
						b.Fatal(err)
					}
					for range readsPerSim {
						if _, err := reader.Get(benchNamespace, hexKey(kg.next())); err != nil {
							b.Fatal(err)
						}
					}
					reader.Close()
				}
				readsPerS = float64(b.N*readsPerSim) / b.Elapsed().Seconds()
				b.ReportMetric(readsPerS, "reads/s")
			})
		})
		rep.set(be.name, "reads/s", fmtRate(readsPerS))
	}
	rep.log()
}

// fmtRate formats a rate (per second) as a human-readable string with K/M suffix.
func fmtRate(v float64) string {
	switch {
	case v >= 1_000_000:
		return fmt.Sprintf("%.1fM", v/1_000_000)
	case v >= 1_000:
		return fmt.Sprintf("%.1fK", v/1_000)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}
