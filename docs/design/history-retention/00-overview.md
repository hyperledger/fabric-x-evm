# Endorser state history: one retention model for all stores

Status: plan, 2026-09-24. Step 0 is in flight (RevertibleLightKVS forgets the oldest snapshot instead of panicking).

## Why

`history_size` is supposed to stop state history from growing without limit, but every store handles it
differently: different unit, different default, and one store ignores it entirely. Nobody has
decided what a good default is, and there is no deliberate way to ask for "keep everything".

## What history is for

The endorser never needs history to endorse: `EVMEngine.Execute` always simulates against the latest state
(`endorser/execution/executor.go`). History serves only:

1. **Reads at an older block**: `Call`, `BalanceAt`, `StorageAt`, `CodeAt`, `NonceAt` with an
   explicit block number (`eth_call`, `eth_getBalance`, … at a height).
2. **Testnode reverts**: `evm_revert` → `RevertibleLightKVS.RevertToBlock`.

**What every store must guarantee:** keep state for at least the last `N` block heights
`[head-N, head]`. A read or revert below that range returns an error. It never silently returns a
newer value. `TestParityEvictedHistoryErrors` already checks this for the memory and Pebble stores.

## Current state

| Store | Counts history in | Effective default | Cost of keeping history | Cost of pruning |
|---|---|---|---|---|
| LightKVS | commits (ring) | 2 | a full copy of the key→value map per snapshot | free (ring overwrite) |
| RevertibleLightKVS | commits (block 0 appears twice after funding) | 16384 | same | shifts every entry down one slot per block once full |
| PebbleKVS | block heights | 2 (factory), 128 (constructor) | proportional to the number of writes | proportional to one old block's writes, in the same batch as the commit |
| VersionedDB (SQLite, in fabric-x-sdk) | — | ignored | append-only; grows forever | none |

Where defaults live: `endorser/app/factory.go` (0 → 2, or 16384 for the testnode),
`endorser/storage/pebblekvs.go` (`defaultHistorySize = 128`), `gateway/app/testnode.go`
(hard-coded 16384), and the integration helpers (1).

## Target

- **One unit:** block heights, for every store.
- **One config meaning** (`endorser.database.history_size`):
  - omitted → the default (one constant, in one place)
  - `N > 0` → keep `N` blocks
  - `0` → unbounded (archive). This has to be set explicitly.
  - negative → error at config validation
- **Defaults:**
  - Production: **128** blocks. This matches Pebble's existing default. Geth also keeps in-memory state for its last 128 blocks, and 128 covers clients that read `eth_blockNumber` and query a few blocks later.
  - Testnode: **unbounded**, like Hardhat and Anvil. Reverts must reach any fixture, and test state is small.
  - Step 1 confirms these numbers (see the memory-store caveat below).
- **Pruning must never cost more per block than the commit itself.** Any per-block pruning work
  should be proportional to that block's writes, or to a small constant.
- **One error.** A shared `ErrHistoryPruned` error that carries the retained range (Pebble's message already does:
  `retained history starts at X, head is Y`). The gateway turns it into a clear JSON-RPC error.
- **Visibility.** Log the effective retention at startup ("history: 128 blocks" / "history: unbounded").

## Steps (one PR each)

### 0. RevertibleLightKVS forgets the oldest snapshot (in flight)
It no longer panics when full. `TestParityEvictedHistoryErrors` now covers it (`boundedHistory: true`).

### 1. Benchmarks: measure before changing defaults
- Add a history-size dimension (`1 / 128 / 16384 / unbounded`) to `BenchmarkUpdateWorldState` and
  `BenchmarkGetState` in `endorser/storage/bench_test.go` (currently fixed at 128).
  - Shows whether Pebble pruning costs anything on commit.
  - Gives the SQLite baseline for how reads degrade as history grows.
- Add a retained-memory measurement for the memory stores: run M blocks over K keys, then force a GC and report heap bytes via
  `b.ReportMetric`. Every LightKVS snapshot copies the entire map, so memory is roughly
  `N × keys × ~50 B`. That's fine for a testnode, and possibly GBs for large state at N=128.
- Output: a table in this doc, and a decision on the memory store's default.

### 2. One history implementation for LightKVS and RevertibleLightKVS
- `LightKVS` replaces its ring (`History []atomic.Pointer` + `NextIndex`) with an oldest-first
  `[]*Snapshot` guarded by a mutex:
  - Append on commit.
  - While `oldest.BlockNumber < head-N`, drop it. This is height-based, so the duplicate block-0 entries are handled for free.
  - `N == 0` → never drop.
- Lookups move into `LightKVS`: the last exact match, else the nearest older one. The ring currently does exact
  match only; nearest-older is harmless there, because every block is committed.
- `RevertibleLightKVS` shrinks to `RevertToBlock` (truncate history, then the existing merge).
  Its `Handle`, `applyBlockSequential`, `NewSnapshot` and `Get` overrides go away. The testnode and
  production then run the same commit and read code.
- Reads at the latest block stay lock-free (`Current` is still atomic). Only reads at an older height take the mutex.
- Dropping from the front of the slice is `slices.Delete(h, 0, k)`: O(N) pointer moves. That's trivial at 128;
  check it at 16384 using the step 1 benchmark. If it matters, compact only once the slice reaches 2N.
- Tests: the existing parity tests, the revertible tests, and a new parity test for "unbounded keeps block 1".

### 3. Config semantics and defaults
- `config.DB.HistorySize` becomes `*int`, so an omitted value (`nil`) can be told apart from `0`.
- Validation rejects negative values.
- A single `DefaultHistorySize = 128` in `endorser/config`. The factory resolves it; the stores take
  the resolved value, where 0 means unbounded. Remove Pebble's own `<= 0 → 128` fallback.
- Pebble with 0 skips `pruneLocked` entirely.
- The testnode sets `0` explicitly (`gateway/app/testnode.go`), replacing the hard-coded 16384.
- The integration helpers keep setting 1 explicitly (a deliberate small window).
- Add the shared `ErrHistoryPruned`, the startup log line, and an update to `docs/JSON_RPC_ERRORS.md`.
- Update the sample configs and the `history_size` comment in `endorser/config/config.go`.
- **Behaviour change:** default memory-store deployments go from 2 to 128 snapshots (or whatever step 1
  decides). Call this out in the PR.

### 4. SQLite pruning (fabric-x-sdk)
`state.VersionedDB` is append-only and lives in fabric-x-sdk, so the pruning belongs there:
- Every K blocks (not every block), delete rows superseded at or below the cutoff:
  `version_block < cutoff AND EXISTS (newer row for same key with version_block <= cutoff)`.
  This uses the existing indexes on `(channel, namespace, key, version)` and `(version_block, version_tx)`.
- Persist the retained-start height, so reads below it return `ErrHistoryPruned`. Until then, reads below it
  would silently return the wrong answer.
- Then set `boundedHistory: true` for VersionedDB in the parity table.
- Until this lands, the factory logs a warning that SQLite ignores `history_size` and keeps full history.

## Out of scope, noted for later
- **Gateway trie store** (`gateway/storage/trie/store.go`): currently off (`NewChain(..., false)`
  everywhere). Once it's turned on:
  - In memory mode it never releases old trie nodes.
  - In persistent mode it writes every root to disk, so it grows like an archive.

  It needs its own retention, e.g. geth's hashdb reference counting plus a cap, or pathdb.
- **Reads ahead of the store:** a read for a block past the store's tip silently returns the tip's
  state (`bn >= current → current`, in all stores). A lagging endorser therefore answers "block N" with
  older state. This is a separate correctness issue.
- **Diff-based LightKVS snapshots:** needed only if step 1 shows the memory store can't afford 128 full-map
  copies for realistic state.
- Chain data kept by the gateway's SQL store (blocks, receipts, logs) is unaffected. It is kept in full
  on purpose, like geth's chain history.

## Open decisions
1. **Config representation:** `*int` with `0 = unbounded` (recommended; matches the plan so far), or a plain int
   with `-1 = unbounded` (simpler decoding, less conventional).
2. **Memory-store default:** 128 like the others, or smaller. Decide from the step 1 numbers.
3. **SQLite:** implement pruning in fabric-x-sdk (step 4), or declare SQLite archive-only and document that.
