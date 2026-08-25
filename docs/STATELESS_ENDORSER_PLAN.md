# Plan: A Stateless Endorser Backed by the Query Service

## Table of Contents

- [What this document is](#what-this-document-is)
- [Background from first principles](#background-from-first-principles)
  - [The system in one paragraph](#the-system-in-one-paragraph)
  - [What the ledger actually stores](#what-the-ledger-actually-stores)
  - [The life of a transaction](#the-life-of-a-transaction)
  - [Why the endorser needs a copy of the state](#why-the-endorser-needs-a-copy-of-the-state)
- [The problem](#the-problem)
- [The idea](#the-idea)
- [The challenge](#the-challenge)
- [The solution: two passes over a batch](#the-solution-two-passes-over-a-batch)
  - [A worked example](#a-worked-example)
  - [Why the merged result is correct](#why-the-merged-result-is-correct)
  - [Why a stale cached read is safe](#why-a-stale-cached-read-is-safe)
- [What this costs elsewhere](#what-this-costs-elsewhere)
- [Traps already discovered](#traps-already-discovered)
- [The plan: pull request by pull request](#the-plan-pull-request-by-pull-request)
  - [The PR roadmap](#the-pr-roadmap)
  - [How the PRs are sequenced](#how-the-prs-are-sequenced)
  - [Group A — the query-service read path](#group-a--the-query-service-read-path)
  - [Group B — batch execution](#group-b--batch-execution)
  - [Group C — removing the commit wait](#group-c--removing-the-commit-wait)
- [Deliberate non-goals](#deliberate-non-goals)
- [Open questions](#open-questions)
- [References](#references)

## What this document is

A plan to stop the endorser from keeping its own copy of world state, and to read
state from the committer's query service instead. It assumes no prior knowledge of
this codebase, Fabric-X, or Ethereum, and builds up what it needs.

An earlier prototype of this design exists. It is not the thing we will ship — it
carries a lot of unrelated work, and it removes more than we want to remove — but
it has already paid for several expensive lessons, which are recorded in
[Traps already discovered](#traps-already-discovered).

## Background from first principles

### The system in one paragraph

This project runs Ethereum smart contracts on Fabric-X, a permissioned
distributed ledger. To an Ethereum wallet it looks like an ordinary Ethereum
node. Internally it is Fabric-X, which is a different animal: instead of every
node re-running every transaction, transactions are *executed* by some nodes,
*ordered* by others, and *validated and committed* by others. The three roles that
matter here:

- **Gateway** — speaks Ethereum JSON-RPC to clients, and drives everything below.
- **Endorser** — runs the EVM. Given a transaction, it executes it and reports
  what the transaction read and what it would write, signed.
- **Committer** — receives ordered transactions, checks them, and applies them to
  the durable ledger (a SQL database). It also runs a **query service**, a gRPC
  API that lets anyone read committed state.

### What the ledger actually stores

Fabric-X state is a flat key-value map. There are no accounts, no balances, no
objects — just:

```
(namespace, key)  ->  (value, version)
```

`namespace` groups keys (all EVM state lives in one namespace). `version` is a
counter that increments every time that key is written. That version is the whole
basis of conflict detection, so keep it in mind.

Ethereum concepts are encoded into this flat map one field per key, not one record
per account (see `accKey` and `storeKey` in `endorser/execution/statedb.go`):

```
acc:<address>:bal      an account's balance
acc:<address>:nonce    its nonce
acc:<address>:code     its bytecode
str:<address>:<slot>   one 32-byte storage slot of a contract
```

So "read account 0xAB's balance" is a read of `acc:0xAB…:bal`, and "read slot 7 of
contract 0xCD" is a read of `str:0xCD…:0x…07`.

Two consequences matter later. Balance, nonce and code are **independent keys with
independent versions**, so writing one does not bump the versions of the others —
conflict detection is finer-grained than an account-shaped record would give. And
there is no stored code hash: `GetCodeHash` keccaks the stored code on demand, so
it is derived, never a key of its own.

### The life of a transaction

The default flow is **execute, then order** (in Fabric terms, EOV):

1. A client sends a signed Ethereum transaction to the gateway.
2. The gateway asks an endorser to execute it. The endorser runs the EVM against
   the current state and records two things:
   - the **read set** — every key it read, *and the version it saw*;
   - the **write set** — every key it would write, and the new value.
   Together these are the **read-write set**. The endorser signs it.
3. The signed transaction goes to the **orderer**, which decides the final order
   of transactions but does not execute anything.
4. The committer takes them in that order and, for each one, performs **MVCC
   validation**: for every key in the read set, is the version recorded still the
   version currently committed? If yes, apply the write set. If any read version
   is stale — someone else wrote that key in the meantime — **abort** the
   transaction and apply nothing.

MVCC is what makes this fast: two transactions touching unrelated keys never
conflict and commit in parallel. It is also the thing that punishes contention:
if many transactions read and write the same key, only the first to commit wins
and the rest abort.

Note carefully what step 4 means: **execution happens against state that might be
out of date by commit time, and that is fine.** Being wrong about a version is not
a correctness problem, it is an abort. This fact is load-bearing later.

### Why the endorser needs a copy of the state

Step 2 needs to read state — a lot of it. A single contract call can read
hundreds of storage slots, and each read must come back with its version so the
read set is valid. Today, every endorser therefore maintains its own full replica
of world state, kept current by following committed blocks from the committer:

```
committer --blocks--> endorser's local state DB --> EVM
```

In code, that replica sits behind one small interface, which is the seam this
whole plan turns on:

```go
// endorser/execution/executor.go
type KVSSnapshotter interface { NewSnapshot(blockNumber *uint64) (ReadStore, error) }

// endorser/execution/statedb.go
type ReadStore interface {
    Get(namespace, key string) (*blocks.WriteRecord, error)
    Close() error
}
```

`NewSnapshot` gives a consistent point-in-time view; `Get` reads one key from it
and returns the value plus its version. Three implementations exist on `main`,
selected by `database.database` in the endorser config: `memory`, `sqlite`, and
`pebble`. A fourth, `checkpoint`, is in flight on an unmerged branch (`#218`) and
is referred to below because it is the most recent attempt at the problem this
plan is about; everything this plan builds is independent of whether it lands.

## The problem

That local replica has to survive a restart. If it does not, a restarting
endorser has no state and must replay the entire ledger from block 0 before it
can endorse anything. Making it durable has proven consistently expensive:

- **`memory`** is fast but lost on restart — hence the replay-from-genesis
  problem.
- **`sqlite`** and **`pebble`** persist every write as it happens. Two costs
  follow. First, *write amplification*: a key-value store on disk rewrites more
  bytes than you logically changed, because it maintains its own internal
  structure. Second, *fsync*: to genuinely survive a crash, a write must be
  forced to physical disk, and that flush is slow and cannot be parallelized
  away. This is the overhead that made `pebble` disappointing in practice.
- **`checkpoint`** (in flight, `#218`) is the response to that: keep state in
  memory and flush a snapshot to disk only every N blocks, so the fsync cost is
  amortized. It works, and it costs a flush cadence to tune plus a way for the
  gateway to start block delivery no later than the lowest height among the
  stores it feeds — otherwise a store that trails the ledger never receives the
  blocks it is missing. Notably that machinery deliberately avoids inventing a
  separate "durable height" concept, expressing the constraint through the
  ordinary `BlockNumber` instead.

Step back and look at what all of that machinery is *for*. It exists to
laboriously re-derive, on the endorser's disk, state that the committer already
holds — durably, transactionally, in Postgres.

## The idea

Stop duplicating it. Have the endorser keep no state at all and read committed
state from the committer's query service on demand.

```
query service <--reads-- endorser (no local state) --> EVM
```

Everything in [The problem](#the-problem) then evaporates. There is no flush
interval, no durability tuning, no delivery-height constraint, no replay from
genesis, and no cold start — a restarted endorser is immediately ready, because
the state it reads was never its to lose.

The query service already exposes what a read set needs
(`committerpb.QueryServiceClient`):

```go
GetRows(ctx, *Query) (*Rows, error)   // Query: namespace + a list of keys
BeginView(ctx, *ViewParameters) (*View, error)   // pin a consistent snapshot
EndView(ctx, *View) (*emptypb.Empty, error)
```

and each returned `Row` is `{Key, Value, Version}` — exactly the value plus
version that MVCC validation requires. Because it supplies only `Version`, and
not the block and transaction coordinates classic Fabric's MVCC expects, this
backend will be Fabric-X only. Note that `pebble` is *also* Fabric-X only but for
a different reason — its version-assignment scheme (`MAX(version)+1` per key,
deletes kept as tombstones) mirrors the Fabric-X committer's own and does not
match classic Fabric's — so this is a second, independent reason landing on the
same restriction, not the same reason twice.

## The challenge

We have traded a local disk read for a network round trip, and there are a great
many reads.

An EVM execution reads *one key at a time*, and it cannot be told in advance
which keys it needs, because each read depends on the result of the previous one:
you learn a contract's storage layout only by running it. So a transaction that
touches 200 keys means 200 sequential round trips to the query service. At even a
modest 0.5 ms each, that transaction takes 100 ms of pure waiting, and endorsing
becomes hopelessly slow. This is the entire difficulty, and everything below
exists to solve it.

## The solution: two passes over a batch

Handle transactions in **batches** rather than one at a time, and execute each
batch twice.

**Pass 1 — warm.** Run every transaction in the batch *concurrently*, pretending
they are all independent, against current committed state. Throw the results
away. The point is not the results; the point is that every read pulls a key into
a local cache. Because the transactions run concurrently, their round trips
overlap: the batch's total wait is roughly the latency of one transaction's chain
of reads, not the sum of all of them. This is why it is called warming the cache.

**Pass 2 — authoritative.** Now run the same transactions *in order*, one at a
time. This pass produces the real, signed results. Its reads are served from the
cache pass 1 filled, plus an **overlay** holding the writes of earlier
transactions in this same batch, so that a later transaction sees an earlier one's
effects. The query service is consulted only for a key nobody touched in pass 1.

Finally the whole batch is folded into **one** merged read-write set, signed
**once**, and submitted as **one** transaction.

Two properties make this work. The reads are the expensive part and pass 1 pays
for them in parallel; the ordered pass is then almost pure CPU. And batching
transactions from the same sender into one submitted transaction removes the
contention they would otherwise have with each other — every transaction from an
account reads and bumps that account's `nonce` key (and its `bal` key, if it moves
any value), so submitted separately they conflict on those keys by construction and
all but the first would abort.

### A worked example

Three transactions in a batch. `T1` writes key `A`. `T2` reads `A` and writes
`B`. `T3` reads `C`. Committed state has `A=1 (version 4)`, `B=1 (version 9)`,
`C=1 (version 2)`.

*Pass 1 (concurrent, results discarded).* All three run at once against committed
state. `T1` reads `A`, `T2` reads `A` — reading the pre-batch value `1`, which is
"wrong", and that does not matter — `T3` reads `C`. The cache now holds
`A=1@v4`, `C=1@v2`, and whatever else they touched.

*Pass 2 (in order, authoritative).* `T1` runs, reads `A` from cache with no
network call, writes `A=2`. That write goes into the overlay. `T2` runs and reads
`A`: the overlay answers `2` — the value `T1` just wrote — so `T2` computes on
correct data. `T3` runs and reads `C` from cache.

*Merge.* Read set `{A@v4, C@v2}`, write set `{A=2, B=...}`. Note that `A`'s
recorded read version is `4`, the version committed *before* the batch — not
anything invented mid-batch.

### Why the merged result is correct

Three rules, and they are the core of the whole design:

1. **Reads merge first-seen-version-wins.** A key read by several transactions in
   the batch appears once in the merged read set, at the version committed before
   the batch ran.
2. **Writes merge last-write-wins**, in batch order — matching what the ordered
   pass actually did.
3. **The overlay never invents a version.** When a later transaction reads a key
   an earlier one wrote, it gets the earlier one's *value* but the *version* from
   committed state (and, for a key with no committed value at all, a nil version
   meaning "absent").

Rule 3 is the subtle one. The whole batch commits as a single transaction, so the
committer validates it against the state the batch started from. If we recorded a
version that only ever existed inside our own batch, validation would be checking
against a version the ledger has never seen. Recording the pre-batch version
instead asks exactly the right question: *did anything else touch this key while
we were working?*

### Why a stale cached read is safe

Pass 1 reads committed state at some instant; by the time the batch is submitted,
that state may have moved. This is not a correctness problem, and the reason is
the one flagged earlier: MVCC validates the read set at commit time. A read that
has gone stale fails validation and the batch aborts and is retried. It never
commits a wrong result.

That gives a genuine engineering dial. Caching more aggressively, or for longer,
trades a higher abort rate for fewer round trips. It is a performance decision,
not a safety one. The opposite is also true and worth stating: a cache that is
*perfectly* invalidated requires following committed blocks — which is precisely
the local replica we set out to delete. So the cache must stay deliberately
imperfect.

## What this costs elsewhere

Merging N Ethereum transactions into one Fabric-X transaction is invisible to
MVCC but very visible to Ethereum clients, which expect every transaction to have
its own hash, its own receipt, and a position within a block. Wallets and tooling
look transactions up by exactly those.

So per-transaction outcomes must survive into the committed block and be
recoverable from it, and each Ethereum transaction needs a three-level coordinate
— `(block number, transaction index, sub-index)` — where the sub-index says which
transaction inside the merged batch it was. Without this,
`eth_getTransactionReceipt` and `eth_getTransactionByHash` break.

This is not part of the execution change at all, and it is the easiest part of
the work to under-budget.

## Traps already discovered

The prototype paid for these. We should inherit the conclusions rather than
rediscover them.

**Query-service snapshots are stale by design.** `BeginView` looks like the
obvious way to get a consistent snapshot, but the query service *aggregates*
concurrently-created views within a window (`view-aggregation-window`, default
100 ms) and serves them from a shared transaction. A freshly created view can
therefore observe state as of up to 100 ms ago, so a just-committed hot key reads
back at its pre-commit version. In the prototype this produced a cascade of MVCC
aborts. The resolution: do not use views. Read current committed state on each
call, and get cross-read consistency from our own cache instead. Keep it
configurable, and document that different keys may then come from slightly
different instants.

**One gRPC connection serializes the warm pass.** A single `grpc.ClientConn`
multiplexes everything over one HTTP/2 connection with a single writer goroutine.
Firing a batch's reads concurrently down one connection re-serializes them.
Use a small pool of connections and round-robin reads across it. Views are
server-side handles keyed by id, so this is safe even in view mode.

**The query service must be retuned, and it is tuned for the opposite workload.**
It batches incoming reads server-side, flushing when it has `min-batch-keys`
(default **1024**) or after `max-batch-wait` (default **100 ms**). Those defaults
suit bulk readers. A single endorser's warm wave never reaches 1024 keys, so it
waits out the full 100 ms tail on *every* pass. The prototype moved these to
`32` / `2 ms`. Two related points: this server-side batching is also why we do
*not* need to batch keys client-side, which is what lets `ReadStore.Get` keep its
one-key-at-a-time signature and spares us an interface change. And the query
service rate-limits requests **on by default** — 5000 per second, burst 1000 —
which per-key reads from a single endorser can reach; set `requests-per-second`
to 0 to disable it when benchmarking, so the ceiling we measure is the system and
not the limiter.

**Do not overlap the two passes.** It is tempting to run pass 1 of batch N+1
while pass 2 of batch N is still going. The prototype did, and on contended keys
it came out roughly 30× *slower* than not overlapping, until cold reads were
inherited across snapshot reopens; even then it needs invalidation sets and two
further tuning knobs, and a bisect environment variable is still sitting in the
code. Out of scope.

## The plan: pull request by pull request

### The PR roadmap

Every PR in this plan carries this table in its description, with its own row
marked, so a reviewer can see where the change sits without reading the rest.

| PR | What it does | Status |
|----|--------------|--------|
| A0 | This plan document | — |
| A1 | `endorser/query`: the `QueryClient` interface + an in-memory implementation | — |
| A2 | `endorser/query`: the gRPC client, with a connection pool | — |
| A3 | `endorser/query`: `ReadStore` adapter with a per-snapshot read cache | — |
| A4 | Factory: make the block-following synchronizer conditional (pure refactor) | — |
| A5 | Wire `database: query-service` (first behaviour change, opt-in) | — |
| A6 | Query-service tuning, integration run, perf figure vs `checkpoint` | — |
| B1 | Merge a batch's results into one read-write set | — |
| B2 | The batch-local write overlay | — |
| B3 | Two-pass batch execution (concurrent warm, serial authoritative) | — |
| B4 | Exclude rejected transactions instead of failing the batch | — |
| B5 | The batch endorsement API (proto + handler + client) | — |
| B6 | Per-transaction receipts and `(block, index, sub-index)` | — |
| B7 | The gateway batches, behind a flag | — |
| C1 | The in-flight write cache (only if B7 is commit-latency-bound) | — |
| C2 | Submit without waiting; in-flight window and rollback | — |

### How the PRs are sequenced

The work splits into three groups. Group A makes the endorser able to read from
the query service; group B makes it endorse batches; group C is conditional and
may not be needed at all.

Two rules keep every PR mergeable to `main` on its own:

- **Nothing changes behaviour until the PR that switches it on.** Most PRs add
  code that nothing calls yet. That is deliberate: a new component arrives with
  its tests, gets reviewed on its own merits, and is wired up later in a small
  PR that contains no new logic. A1–A4, B1–B4 and B6 are all inert on merge.
- **Every PR leaves the tree green**, with tests for what it adds.

Sizes below are rough: **S** is a couple of hundred lines including tests, **M**
is up to a few hundred, **L** is bigger and should be watched for further
splitting.

Not everything is a chain. **A1, A4, B1 and B2 depend on nothing and can be
opened in parallel.** The ordering constraints that do exist:

```
A1 ─┬─> A2 ─┐
    └─> A3 ─┼─> A5 ─> A6            B1 ─┐
       A4 ──┘                       B2 ─┴─> B3 ─> B4 ─> B5 ─> B6 ─> B7 ─> (C)
```

Group B needs nothing from group A: batch execution works over any backend, and
is easiest to test over `memory`. The two groups only meet in production
configuration, so they can proceed concurrently.

### Group A — the query-service read path

**A1 — `endorser/query`: client interface and in-memory implementation.** *(S)*
The `QueryClient` interface (`BeginView`/`GetRows`/`EndView`/`Close`), the `Row`
type, and an implementation backed by the existing in-process `LightKVS`.
*Mergeable alone:* nothing imports the package yet. *Review focus:* the interface
shape, since everything later depends on it. The in-memory implementation is not
just a test double — it lets tests and the embedded testnode exercise the real
read path with no committer running.

**A2 — `endorser/query`: gRPC client.** *(M)* The real client: dialling with the
TLS modes the query service supports, the proto mapping to and from
`committerpb`, and the connection pool with round-robin reads. *Mergeable alone:*
still nothing imports it. *Review focus:* TLS handling, and the reason for the
pool — one connection would re-serialize concurrent reads behind a single HTTP/2
writer.

**A3 — `endorser/query`: the `ReadStore` adapter.** *(M)* `Store` and `View`,
implementing the existing `KVSSnapshotter` and `ReadStore` so the query service
plugs into the seam the EVM already reads through, with a per-snapshot read cache
that also remembers *absent* keys. Tested entirely over A1, so this PR needs no
gRPC and no committer. *Review focus:* cache semantics and snapshot lifecycle —
who opens a view, who closes it. Also the default for whether we use query-service
views at all (see [Traps already discovered](#traps-already-discovered)).

**A4 — factory: separate the store the EVM reads from the store that follows
blocks.** *(M, pure refactor)* Today `NewEndorser` always builds a block-following
`Synchronizer` over the returned store, because every backend has one. Make that
conditional, so a backend that follows no blocks is expressible. Touches the
gateway wiring, testnode, and test helpers. *Mergeable alone:* no new backend, no
behaviour change, all four existing backends work exactly as before. *Review
focus:* blast radius — this is the only cross-cutting PR in the group, which is
exactly why it is not mixed in with a new feature.

**A5 — wire `database: query-service`.** *(S)* The config value, the
query-service client settings, validation, the Fabric-X-only guard (same
reasoning as `pebble`), and the factory case assembling A2 and A3. Small, because
A1–A4 did the work. *This is the first PR in the group that changes behaviour,*
and only for a deployment that opts in by config.

**A6 — make it work end to end, and measure it.** *(M)* Query-service tuning in
`testdata/config` — `min-batch-keys` and `max-batch-wait` are set for bulk
readers and must come down, and the rate limiter wants disabling for benchmarks,
each with a comment saying why — plus an integration run of the existing suite
against `database: query-service`, and a throughput figure from
`integration/perf` beside the backends on `main`. If `checkpoint` (`#218`) has
landed by then it is the most interesting comparison; if not, `pebble` is the
persistent baseline.

At A6 we can answer the question the whole plan rests on: **is reading from the
query service viable at all?** Nothing in group A commits us to batching.

### Group B — batch execution

**B1 — merge a batch's results into one read-write set.** *(S)* The pure fold:
reads unioned first-seen-version-wins, writes last-write-wins, per-transaction
outcomes carried alongside. *Review focus:* this is the correctness core of the
whole design — the three rules in [Why the merged result is
correct](#why-the-merged-result-is-correct) — and a plausible-looking mistake here
produces read sets the committer silently mis-validates. It deserves the heaviest
unit tests in the plan, and it is a pure function, so it can have them.

**B2 — the batch-local write overlay.** *(S)* A `ReadStore` that layers the
current batch's writes over an underlying one, so a later transaction sees an
earlier one's effects. Small but not trivial: the version it reports always comes
from committed state, never from the in-batch write, and a key absent from
committed state must read back as absent. *Review focus:* exactly that rule.

**B3 — two-pass batch execution.** *(M)* `ExecuteBatch` on the EVM engine: the
concurrent warm pass (one worker per transaction, results discarded, a panic in
one transaction contained), then the serial authoritative pass over B2's overlay,
folded by B1. A single-transaction batch shares the existing execution path so
the two cannot drift. Any pre-execution error aborts the batch, which B4 then
refines. Tested over `memory`. *Mergeable alone:* nothing calls it yet.

**B4 — exclude rejected transactions instead of failing the batch.** *(S)* A
transaction the EVM rejects before it runs — wrong nonce, bad signature, not
enough funds — gets an empty-read-write-set placeholder in its slot and the batch
continues. A rejection a later transaction could fix (nonce too high) stays
pending; one that never can (nonce too low) is evicted. *Why separate:* B3 is
correct but brittle, and this is the PR that makes one bad transaction stop
killing 99 good ones. Worth its own review.

**B5 — the batch endorsement API.** *(M)* `ExecuteBatch` on `core.Endorser` and
the `Service` interface, one new RPC in `api/endorsementpb`, server handler and
client. Defines how per-transaction outcomes travel inside a single signed
response. *Review focus:* the proto change and the outcome payload format, since
B6 depends on it and it is awkward to change later.

**B6 — per-transaction receipts and sub-indexing.** *(L)* Recover
per-transaction outcomes from a committed block and give each Ethereum
transaction a `(block, index, sub-index)` coordinate, so
`eth_getTransactionReceipt` and `eth_getTransactionByHash` keep working when one
Fabric-X transaction carries many. *Mergeable alone, and deliberately landed
before anything produces such a transaction* — it is inert but tested on merge,
which de-risks B7. See [What this costs
elsewhere](#what-this-costs-elsewhere). If this comes out too big, split it:
parsing and indexing first, then the JSON-RPC lookups.

**B7 — the gateway batches, behind a flag.** *(M)* Collect up to N transactions,
endorse once, submit one Fabric-X transaction, and wait for it to commit before
starting the next batch. Off by default. *Done when* the hardhat and integration
suites are green with it on, a run of consecutive transactions from one sender
commits as a single Fabric-X transaction, and we have a throughput figure against
group A. Flipping the default is a separate one-line PR once those numbers are in.

### Group C — removing the commit wait

Only if B7 turns out to be limited by commit latency rather than by execution.
Waiting for each batch caps throughput at one batch per commit round trip.

**C1 — the in-flight write cache.** *(M)* A standalone structure holding the
writes of submitted-but-uncommitted batches, with its own tests. Nothing calls
it. *Review focus:* concurrency — it is read by request handlers while being
mutated at batch boundaries.

**C2 — submit without waiting.** *(L)* Read through C1 so a batch can build on
its in-flight predecessor, bound how many batches may be outstanding, and unwind
the batches that read an aborted batch's writes. *Review focus:* the rollback
path. This is the riskiest PR in the plan and the one most likely to need
splitting again once C1 exists.

Explicitly **not** overlapping the two passes — see
[Traps already discovered](#traps-already-discovered).

## Deliberate non-goals

Keeping the change small enough to review is a goal in itself.

- **Nothing is deleted.** The new backend is added alongside `memory`, `sqlite`
  and `pebble`, and the block-following synchronizer stays for them. The
  prototype removed all of it; we keep it, both for a smaller diff and because
  those backends are the baseline we measure against.
- **`NewSnapshot` keeps its signature.** The prototype changed it, which touches
  every existing backend for no benefit here.
- **No EVM execution fast path.** Reusing state objects between transactions
  instead of rebuilding them is a real optimization and accounts for most of the
  prototype's changes to `statedb.go`. Separate change, separate measurement.
- **No pipelining, tracing, or dashboards.**

## Open questions

1. **Does anything here collide with `#218`?** Group A branches off `main`, so it
   neither depends on nor conflicts with the `checkpoint` work as written — but if
   that work is close to landing, we should confirm the two do not fight
   over `factory.go` (A4 touches it).
2. **Can B7 ship enabled while it still waits for each commit?** It is safe to
   *merge* either way, since it lands behind a flag. The question is whether it
   can become the default on its own, or whether the default flip has to wait for
   group C. If the point of the exercise is throughput rather than deleting the
   persistence machinery, it waits.
3. **Is B6 one PR or two?** It is the largest item in the plan and the only one
   whose scope is not really about execution at all.

## References

Interfaces this plan turns on:

- `endorser/execution/executor.go` — `KVSSnapshotter`
- `endorser/execution/statedb.go` — `ReadStore`, and `getStateFromStore` for how a
  read is journalled and how an absent key records a nil version
- `endorser/storage/lightkvs.go` — the `KVS` interface, which welds the read side
  to block handling
- `endorser/app/factory.go` — backend selection and synchronizer wiring
- On the unmerged `#218` branch: `endorser/storage/checkpointkvs.go`, and the
  `DurableKVS` comment in `endorser/storage/lightkvs.go` explaining why a lazily
  flushed store constrains where block delivery may resume

External API:

- `committerpb.QueryServiceClient` in `fabric-x-common` — `GetRows`, `BeginView`,
  `EndView`
- `service/query/config.go` in `fabric-x-committer` — `min-batch-keys`,
  `max-batch-wait`, `view-aggregation-window` and their defaults

The findings in [Traps already discovered](#traps-already-discovered) come from an
earlier prototype of this design, which is not a base to build on: it carries a
great deal of unrelated work and removes more than this plan does. Each of those
findings is independently checkable against the committer sources cited above, and
group A should confirm them rather than take them on trust.
