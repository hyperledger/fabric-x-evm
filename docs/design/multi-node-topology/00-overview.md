# Multi-Node Topology — Design and Implementation Plan

> How `cmd/fxevm` runs a real, multi-process deployment (one org's gateway with
> its own embedded endorser, another org running only a standalone endorser),
> reached over the gRPC endorsement API from
> [docs/ENDORSEMENT_API.md](../ENDORSEMENT_API.md). Builds on PR #306
> (`gateway/config.Config.Endorser` singular, `endorser/{server,client}` gRPC
> boundary), which proved the shape in integration tests only.

## Table of Contents

- [Multi-Node Topology — Design and Implementation Plan](#multi-node-topology--design-and-implementation-plan)
  - [Table of Contents](#table-of-contents)
  - [Motivation](#motivation)
  - [Current State](#current-state)
    - [Constraints discovered while planning](#constraints-discovered-while-planning)
  - [Target Design](#target-design)
  - [Decisions](#decisions)
    - [One `start` command, config-driven](#one-start-command-config-driven)
    - [Unify the co-located endorser onto one synchronizer](#unify-the-co-located-endorser-onto-one-synchronizer)
    - [No loopback gRPC for the co-located endorser](#no-loopback-grpc-for-the-co-located-endorser)
    - [Local endorser is a distinct field, not slice position 0](#local-endorser-is-a-distinct-field-not-slice-position-0)
    - [One committer per process, configured at the top level](#one-committer-per-process-configured-at-the-top-level)
    - [Shape of the endorser's gRPC server config](#shape-of-the-endorsers-grpc-server-config)
    - [Package boundaries: gateway, endorser, and shared synchronizer wiring](#package-boundaries-gateway-endorser-and-shared-synchronizer-wiring)
  - [PR Sequence](#pr-sequence)

## Motivation

PR #306 proved that a gateway can reach an endorser over real mTLS gRPC —
either its own co-located one, or another org's — end to end against a real
Fabric-X backend. But that proof lives entirely in `integration/` test code
(`integration/split_endorsers.go`), built and served in-process inside the
test binary. There is no way today to run this topology for real: start one
process that is org1's gateway with its own embedded endorser, and a second,
separate process that is only org2's endorser, with org1's gateway dialing
org2 over gRPC.

The goal is to make that topology available through `cmd/fxevm`, and in doing
so, delete the duplication between the production wiring in `gateway/app` and
the test-only wiring in `integration/`, so the two stay in sync by
construction rather than by discipline.

## Current State

As of this doc, confirmed by reading the code:

- `gateway/config.Config.Endorser` is `*endorser.Endorser` — **already
  singular**. A gateway cannot embed more than one endorser today; nothing to
  change there.
- `gateway/config.Config.Gateway` is a mandatory, non-pointer `Gateway` struct
  — there is no way to omit the `gateway:` section. A config always describes
  a gateway.
- `Gateway.Endorsers []common.ClientConfig` (remote, dialed over gRPC) and
  `Endorser` (embedded, called in-process) are **mutually exclusive**
  (`gateway/config/config.go:114-142`). A gateway either embeds its own
  endorser, or dials remote ones — never both — and a "split mode" gateway can
  run with **zero** embedded endorsers.
- `endorser/config.Endorser` has no gRPC server / listen configuration at all.
  The only code that serves an endorser over gRPC is test-only
  (`integration/split_endorsers.go`), and it derives TLS material by directory
  convention (`filepath.Dir(MSPDir)/tls`) rather than from explicit config.
- `cmd/fxevm` has `start` (combined gateway + embedded endorser, single
  process), `testnode`, and `healthcheck`. There is no way to run a standalone
  endorser process. `healthcheck` only opens a TCP connection to the gateway's
  HTTP port (`cmd/fxevm/healthcheck_cmd.go:33`, default `localhost:8545`), and
  `Dockerfile:15` uses it as the container health probe.
- `gateway/core.EndorsementClient` fans out over a plain `[]api.Service` — it
  does not care whether an element is a function-call endorser or a gRPC
  client. It is **N-of-N**: `ExecuteTransaction` fails the whole transaction if
  any endorser returns an error (`gateway/core/endorse.go:72-124`). State reads
  (`BalanceAt`, `CallContract`, …) always use `endorsers[0]`
  (`gateway/core/endorse.go:156-240`), so the co-located endorser must stay
  first in that slice once local and remote endorsers are mixed.
- Production (`gateway/app.newApp`, embedded case) builds the co-located
  endorser with `eapp.NewEndorser`, which is **self-syncing**: it creates and
  owns its own `*sdknet.Synchronizer` against the committer. The gateway
  separately builds its own synchronizer (`NewGatewaySynchronizer`) feeding
  only `[chain, gateway]`. That is two independent delivery connections to the
  same committer, in the same process.
- `App.Run` starts both synchronizers but only waits on `endorserSyncs`
  (`gateway/app/app.go:240-252`). **Nothing waits on `gwSync` today.** In split
  mode (`endorserSyncs` empty) the gateway therefore begins serving before any
  initial sync at all.
- The integration test harness already avoids the duplication:
  `integration/test_helpers.go:161` (`defaultHandlerChain`) builds the endorser
  with the sync-less `eapp.NewEndorserCore` and registers its KVS directly as a
  `blocks.BlockHandler` (`estorage.KVS` embeds `blocks.BlockHandler` —
  `endorser/storage/lightkvs.go:30`) on **one shared synchronizer**, ordered
  `[endorserKVS, chain, gateway]`. So does production's own testnode path
  (`gateway/app/testnode.go:74,104`). `newApp` is the last remaining caller of
  the self-syncing construction inside `gateway/app`.

### Constraints discovered while planning

Four properties of the current code shape the sequence below. Each is a
correctness trap, not a style preference.

1. **Resume point, and block handlers that are not replay-safe.** The
   endorser's own synchronizer resumes from the endorser's KVS
   (`eapp.NewEndorser` passes `kvs` as *both* the `network.BlockHeightReader`
   and the handler, `endorser/app/factory.go:131-133`), so the endorser
   resumes exactly at its own height and never re-applies a block.
   `NewGatewaySynchronizer` resumes from the gateway's SQLite chain instead
   (`gateway/app/app.go:179`, `gateway/storage/store.go:194`).

   That difference matters more than a changed resume offset, because the
   endorser's stores are **not idempotent**. `LightKVS.Update` derives
   `nextVersion = existing.Version + 1` from current state rather than from the
   block (`endorser/storage/lightkvs.go:294-297`), so re-applying a block bumps
   every touched key's version twice; `PebbleKVS.commitBlock` assigns
   `MAX(version)+1` per key, the same class. Those versions go into the MVCC
   read-sets the endorser produces, so once they drift the committer rejects
   every later transaction touching those keys — silently. `PebbleKVS`'s own
   doc comment states the assumption outright: it keeps its checkpoint equal to
   ledger height precisely so it can "serve as the synchronizer's height
   reader" (`endorser/storage/pebblekvs.go:330-339`).

   One synchronizer means one resume point feeding two stores, and with
   non-idempotent handlers **neither store is safe to resume from**: from the
   chain, the endorser KVS replays and corrupts its versions; from the KVS, the
   chain permanently misses a block. Nor is the mismatch exotic — the handler
   chain `[endorserKVS, chain, gateway]` is not atomic across stores, so an
   ordinary crash between the first two handlers leaves the KVS at `N` and the
   chain at `N-1`. A durability config check would only cover cold start, not
   this.

   The shared-synchronizer pattern has never hit any of it because today it
   exists only in the test harness and `testnode`, both fully in-memory and
   never restarted. Making handlers idempotent is therefore a prerequisite for
   the unification, not a refinement of it — see PR 1 below.

2. **Silent block loss in hybridx.** A handler error propagates out of
   `notifGate.HandleBatch` to `streamer.Stream`, which the Start goroutine logs
   at `Warnf` and restarts — resuming at the live tip, with the skipped block
   never re-delivered (`gateway/app/hybridx/hybrid.go:160-168`). After the
   switch to notification mode, delivery is cancelled and `h.delivery` set to
   nil (`hybrid.go:214-219`), so there is no catch-up path. Registering the
   endorser's KVS on this chain makes a single endorser write failure desync
   the endorser's state permanently. This is a pre-existing bug affecting
   `chain` and `gateway` today; the plan is what makes it dangerous.

3. **There are two committer configs because there are two synchronizers.**
   `endorser.committer` is read only by `eapp.NewEndorser` when it builds the
   endorser's own synchronizer (`endorser/app/factory.go:131-133`);
   `gateway.committer` is read only by `NewGatewaySynchronizer`
   (`gateway/app/app.go:179`). Both are required by validation
   (`endorser/config/config.go:110`, `gateway/config/config.go:102`). In every
   config that has both — `fabx.yaml`, `fabx-full.yaml`,
   `fablo.hardhat.yaml` — the two blocks are **byte-identical**: same endpoint,
   same certificate, same CA. The duplication has no meaning; it is an artifact
   of the second synchronizer.

4. **`serve.Config` does not decode the documented endpoint form.**
   `connection.Endpoint` carries only `host` / `port` mapstructure tags, and
   `config.Load` uses `viperutil.New()`, whose decode hooks are StringSlice,
   ByteSize, stringFromFile, pemBlocks and OrdererEndpoint — none of which
   handle a string endpoint. The `endpoint: :4001` form used throughout the
   committer's own sample configs will fail to decode here; only the
   `endpoint: {host, port}` mapping form works. Separately, `serve.Serve`
   returns `nil` when its config is nil, so an absent `server:` block silently
   serves nothing instead of failing.

## Target Design

- `gateway:` becomes optional. No `gateway:` header means an endorser-only
  process (e.g. org2 in a two-org topology).
- `endorser:` becomes **mandatory whenever `gateway:` is present** — a gateway
  always embeds its own org's endorser now. The current "pure split, zero
  embedded endorsers" mode goes away.
- `gateway.endorsers` keeps its shape but narrows its meaning: it is now
  strictly *additional remote* endorsers (other orgs), never a substitute for
  the always-present own one. Because endorsement is N-of-N, each entry added
  here makes every transaction depend on that org's liveness.
- The gateway calls its co-located endorser through a **direct in-process
  function call** — `*core.Endorser` already satisfies `eapi.Service`, which is
  how `newApp` and `testnode` call it today. Only other orgs' endorsers go over
  gRPC.
- The gateway *may* **serve** its own endorser over gRPC, from the same
  `endorser.server` config a standalone endorser uses — set it when another
  org needs to reach this endorser, leave it unset when the endorser stays
  purely internal to this gateway. Serving is config-gated; the in-process
  call is always direct, regardless. Serving and calling are separate
  concerns.
- The co-located endorser's *state* stays current via the gateway's single
  synchronizer (KVS registered as a handler), not its own separate one.
- `committer` moves to the top level, a sibling of `network:`. One synchronizer
  per process means one committer connection per process, for both shapes.
  `orderers` stays under `gateway:` — every process syncs, only a gateway
  submits.

## Decisions

### One `start` command, config-driven

Considered adding a separate `fxevm endorser start` subcommand (mirroring an
earlier, now-stale plan for this CLI). Rejected in favor of extending the
existing `fxevm start` to branch on `cfg.Gateway == nil`.

Now that `Config.Validate()` only allows two shapes — `gateway:` + `endorser:`
combined, or `endorser:` alone — a second command would have to independently
agree with the config about what it's allowed to contain, which is exactly the
kind of drift this plan is trying to eliminate. There is already a precedent
for config-presence-driven branching in this file: `newTestNodeCmd` branches
on `configPath == ""` to pick self-contained vs. real-backend mode.

`healthcheck` does need to reach an endorser-only process too, since it has no
HTTP listener to probe on the default `:8545` — but not a second *mode*: it
stays the same bare TCP connect against whatever `--addr` names, pointed at
the endorser's gRPC port instead. `endorser/server/server.go:42` already
registers the standard gRPC health service, but nothing calls
`SetServingStatus`, so a real health RPC would answer a hardcoded `SERVING`
today regardless of actual readiness — not worth a per-call mTLS handshake
for no extra signal over a bare connect. Wiring `SetServingStatus` into the
standalone endorser's `Run`/`Shutdown` lifecycle so that RPC becomes
meaningful is a real improvement, left for later rather than folded into
this PR.

### Unify the co-located endorser onto one synchronizer

Every gateway process embeds exactly one endorser, and already did in the
embedded branch — `cfg.Endorser` is singular and that branch only runs when
`len(cfg.Gateway.Endorsers) == 0`. So there is no reason to keep two
synchronizers per process, and no dependency on any config change to fix it.
Production should build the co-located endorser with `eapp.NewEndorserCore`
(sync-less) and register its KVS as a handler on the same synchronizer call —
exactly matching `defaultHandlerChain` and `testnode.go`. `App.endorserSyncs`
and the loop in `Run()` that starts and waits on it separately go away.

That construction call is also renamed `NewSynchronizer` (from
`NewGatewaySynchronizer`), and the `App.gwSync` field to `App.synchronizer`:
now that it is the one synchronizer for the whole process — feeding chain,
the embedded endorser's KVS, and the gateway, not just the gateway — a name
scoped to "gateway" undersells it. The function itself was always generic
(protocol dispatch over an arbitrary handler list, no `core.Gateway`
dependency); only the name was tied to when it had a sibling synchronizer to
be "the gateway's" as opposed to.

Two things must land *with* that change rather than after it:

- **A synchronizer wait.** `Run()` waits only on `endorserSyncs` today, so
  deleting that loop leaves nothing waiting for initial sync before the HTTP
  listener opens. `WaitUntilSynced(gctx, a.synchronizer, …)` replaces it.
- **Idempotent handlers**, which per constraint 1 are what make a shared
  synchronizer safe at all. They land first, as their own PR (1 below),
  because that is a correctness fix to block delivery rather than part of
  this refactor, and it closes the same latent hole in `testnode` and the
  test harness, which run this pattern today without ever restarting.
  Handler order stays `[kvs, chain, gateway]`, so within one
  continuously-running process chain's height can never get ahead of kvs's —
  resuming the synchronizer from `chain` alone (unchanged) is enough; no
  min-height reader across kvs and chain is needed. (A durability mismatch —
  e.g. an in-memory endorser KVS losing its height across a restart while a
  persistent chain DB keeps its own — could still break that invariant, but
  that risk predates this change and isn't newly introduced or fixed by it.)
  This change needs no config restriction either way.

This only applies to the co-located endorser. A remote org's standalone
endorser process still owns its own synchronizer, same as today — there is
nothing else to feed it in that process.

### No loopback gRPC for the co-located endorser

An earlier draft routed the gateway's own endorser through a gRPC server and a
localhost dial, so local and remote used one code path, then added a follow-up
to swap it back to a direct call for performance. That is work followed by its
own reversal, and the reversal was already planned before the first half
landed.

It is dropped. The local endorser is a direct in-process call from the start:

- Every state read goes through `endorsers[0]`
  (`gateway/core/endorse.go:156-240`), so `eth_call`, `eth_getBalance`,
  `eth_estimateGas`, `eth_getCode` and `eth_getTransactionCount` would each pay
  serialization on the read path for no functional gain.
- It removes a question with no good answer — which client certificate the
  gateway presents to its own endorser, and which CA that endorser trusts for
  it — that exists only to satisfy a hop to itself.
- The gRPC path is not left untested: the two-org integration topology
  exercises it against org2 on every run.
- The design commitment this was justified by does not exist.
  `docs/ENDORSEMENT_API.md` contains no such statement, and the
  `docs/design/endorsement-api/` tree the earlier draft linked to is not in the
  repo.

The gateway may still start the gRPC **server** for its own endorser — gated
on `endorser.server` being set, same as a standalone endorser — since other
orgs' gateways dial it. Only the local *call* is unconditional and direct.

### Local endorser is a distinct field, not slice position 0

Today `gateway/core.EndorsementClient` holds one flat `endorsers []api.Service`
(`gateway/core/endorse.go:32`), and every read method hardcodes `endorsers[0]`
(`gateway/core/endorse.go:162,201,231,236,241,246`). `buildApp` and
`BuildGateway` thread the same flat, pre-ordered slice through. Nothing in the
type system says index 0 is special — it's an invariant enforced only by a
comment and by every caller happening to build the slice in the right order.

That's fine while there's at most one caller convention to get right (`newApp`
puts the sole embedded endorser first because it's the only element). PR 5 is
where it stops being fine: `[own, remotes…]` becomes a real multi-element
slice assembled at the top of the call chain, and every layer between there
and `endorsers[0]` — `buildApp`, `BuildGateway`, `NewEndorsementClient` — has
to keep passing it through *in order* with nothing checking that. A future
reordering (e.g. sorting remotes, or a refactor that builds the slice
differently) would silently misroute every read to a remote endorser instead
of failing to compile.

Fixed by making "local" a distinct parameter/field instead of a position:

- `BuildGateway` and `buildApp` take `local eapi.Service, remotes
  []eapi.Service` instead of one `endorsers []eapi.Service`.
- `EndorsementClient` stores `local api.Service` and `remotes []api.Service`
  as separate fields instead of one `endorsers []api.Service`. Every read
  method calls `e.local` directly — no indexing, so there's nothing to get
  wrong.
- `ExecuteTransaction`'s N-of-N fan-out (`gateway/core/endorse.go:52-131`) is
  the one place that's genuinely symmetric — it doesn't care which endorser is
  local — so it iterates `local` plus `remotes` together there, the only spot
  that still needs to.

This lands as part of PR 5, which is already where the `[own, remotes…]`
assembly is introduced for the first time in production — there's no reason
to introduce the flat-slice version there and immediately replace it.

### One committer per process, configured at the top level

An earlier draft kept `gateway.committer` and `endorser.committer` and added a
rule about which one wins when — required for a standalone endorser, rejected
when a gateway is present. That is a conditional-validation rule invented to
manage a duplication that should not exist.

`committer` moves up one level instead, a sibling of `network:`:

```yaml
network:
  protocol: fabric-x
  channel: mychannel

committer:
  endpoint: {host: committer.org1.example.com, port: 4001}
  tls: {mode: mtls, cert-path: …, key-path: …, ca-cert-paths: [ …]}

gateway:   # optional
endorser:  # required when gateway is present
```

One synchronizer per process means one committer connection per process, so it
is a property of the process, not of the component. Both shapes need exactly
one: a gateway process syncs, and a standalone endorser process syncs. There is
no conditional rule left to write — `committer` is simply always required — and
constraint 3's dead-but-required field disappears rather than being managed.

Two details this does not change:

- **Which identity signs the deliver request** still follows the process shape:
  the gateway's signer where `gateway:` is present, the endorser's where it is
  not, exactly as today. It needs no config of its own, because TLS material
  lives in the `committer` block while the signing identity comes from the
  section that is present. `integration/fabx-2of2-org2.yaml` — org1 TLS
  material carrying an Org2MSP identity — stays expressible unchanged.
- **`orderers` stays under `gateway:`.** The asymmetry is deliberate and worth
  keeping: every process syncs from a committer, but only a gateway submits.

`docs/ENDORSEMENT_API.md:211` describes `endorser.committer` as the endorser's
own sync connection and needs updating with this change.

### Shape of the endorser's gRPC server config

The field holds `serve.**ServerConfig**`, not `serve.Config`, so `server:` sits
directly under `endorser:`:

```go
Server *serve.ServerConfig `mapstructure:"server" yaml:"server"`
```

```yaml
endorser:
  name: org1
  server:
    endpoint:
      host: 0.0.0.0
      port: 9001
    tls:
      mode: mtls
      cert-path: /crypto/.../server.crt
      key-path: /crypto/.../server.key
      ca-cert-paths: [/crypto/.../tlsca.org1.example.com-cert.pem,
                      /crypto/.../tlsca.org2.example.com-cert.pem]
```

`serve.Serve` wants a `*serve.Config`, so the call site wraps it:
`&serve.Config{GRPC: *cfg.Server}`. One line, and it buys the flat layout —
`serve.Config`'s own `GRPC` field carries the mapstructure tag `server`, so
holding a `serve.Config` here would produce `endorser.server.server.endpoint`.

Everything the endorser needs lives on `ServerConfig`: endpoint, TLS,
keep-alive, rate-limit, max-concurrent-streams. What `serve.Config` adds is
`monitoring` (a Prometheus/HTTP listener) and `startup-timeout`. Neither is
wanted yet, and neither is lost — each can arrive later as a plain sibling
under `endorser:`, mapped into the same wrapper, which is how the committer's
own services lay them out at their file root.

Embedding `serve.Config` with `mapstructure:",squash"` would also flatten it,
and was rejected: `viperutil`'s key walker matches YAML keys against **Go field
names**, ignoring mapstructure tags entirely, with no handling for anonymous
fields (`getKeysRecursively`, `config_util.go:153-172`). Combined with
`ErrorUnused: true`, that is a decode failure waiting to happen for a layout the
explicit field gives us with no cleverness.

Note the `endpoint` mapping form: per constraint 4, the `endpoint: :9001`
string form found in committer sample configs does not decode through this
repo's loader. Documenting the working form matters more than it looks, because
operators will copy from those samples.

`Validate()` must require this block wherever the endorser has to be reachable,
since `serve.Serve` treats a nil config as "serve nothing, successfully."

### Package boundaries: gateway, endorser, and shared synchronizer wiring

PR 4 is where a second process shape (standalone endorser) first exists in
production, and it needs the same two things a gateway does: an embedded
endorser, and a synchronizer feeding it. Today those are built two different
ways — `gateway/app.newApp` builds the sync-less `NewEndorserCore` and feeds
its KVS into `gateway/app/wiring.go`'s `NewSynchronizer` (protocol dispatch
over a handler list; `hybridx.New` for fabric-x), while `eapp.NewEndorser`
builds the same endorser core and then a synchronizer of its own, always
calling `nfab`/`nfabx.NewSynchronizer` directly — bypassing hybridx even for
fabric-x. That split reflects a real design point at the time (endorsers
plain-delivery-only, `integration/split_endorsers.go` has a comment
explaining why), but it's exactly the kind of second-way-to-build-the-same-
thing this plan keeps flagging as risk, not a distinction worth preserving:
hybridx is simply how fabric-x delivery works, gateway or endorser. PR 4
unifies on the one path rather than reusing `eapp.NewEndorser` for the
standalone case and inheriting its now-pointless plain-delivery special case
— building it that way only to unify it later would be work followed by its
own reversal, the same shape already rejected in [No loopback
gRPC](#no-loopback-grpc-for-the-co-located-endorser). `eapp.NewEndorser`
becomes unused once PR 4 rewires `integration/split_endorsers.go`'s
`startEndorserGRPCServer` — its one remaining caller — onto the new
standalone path, and is deleted in the same PR rather than left as a second,
now-pointless way to do the same thing.

Concretely:

- **A new top-level `synchronizer` package** takes `NewSynchronizer`, the
  `Synchronizer` interface, and `WaitUntilSynced` out of `gateway/app`
  (`wiring.go:29,71`, `app.go:316`). Nothing about them is gateway-shaped —
  the handler list is the only thing that varies per shape (`[kvs]` for
  standalone, `[kvs, chain, gateway]` for a gateway) — only their address
  was. Both `gateway/app` and the new `endorser/app` entrypoint import it;
  neither imports the other's package for it.

  `gateway/app/hybridx` moves with it, as `synchronizer/hybridx` — it's the
  dispatcher's sole fabric-x implementation, so leaving it under `gateway/`
  would just recreate the same "address doesn't match what it is" problem
  one level down. `common/notification.go` (`BlockHandler`, `AllTxBatchDispatcher`)
  moves into `synchronizer/hybridx` with it: despite living in `common`
  today, it has exactly one real caller — `hybridx` already imports it
  (aliased `evmcommon`) purely for these types, and nothing else in the repo
  uses `common.BlockHandler` or `common.AllTxBatchDispatcher` outside
  `hybridx` and its test. It was never actually common; it was hybridx's,
  parked one package over.
- **`NewEndorserCore` absorbs `HistorySize` defaulting**
  (`gateway/app.newApp`'s inline `if enableTestRPC { 16384 } else if
  HistorySize == 0 { 2 }`) from the `testImpl` parameter it already takes,
  instead of every caller re-deriving the same policy.
- **`endorser/app` gains the standalone entrypoint** — resolve its own
  signer from MSP (one line, same as the gateway resolves its own inline —
  too small to wrap), `NewEndorserCore`, `synchronizer.New(...,
  handlers=[kvs])`, then `Run`/`Shutdown` with the errgroup/wait/close-on-
  signal shape `gateway/app.App.Run` already has, scoped to `endorser`, its
  `kvs` (closed on shutdown), and the gRPC server — no `core.Gateway`, no
  chain, no HTTP.
- **Serving the endorser's gRPC stays inline in PR 4**, not a standalone
  helper: `endorser/app.App.Run` calls `server.New(a.endorser).Serve(gctx,
  &server.Config{GRPC: *a.cfg.Endorser.Server})` directly inside its errgroup
  goroutine, since the standalone entrypoint is the only caller today. It was
  briefly factored out as an exported `ServeGRPC` for PR 5 to reuse, then
  inlined back — with one real caller, the extraction wasn't earning its
  keep, and re-extracting later costs nothing (it's one line). **PR 5 should
  pull this back out into a shared function once the gateway side actually
  needs it** — that's the point at which a second real caller exists, not
  before.

This resolves the open question PR 4 originally left for whenever its code
existed (where does the standalone-endorser path live): **`endorser/app`**,
symmetric with `gateway/app` — not a widened `*App` with gateway-only fields
left nil for the endorser-only case. `gateway/app` keeps everything that
actually is gateway-specific (`App`, `newApp`/`newSplitApp`/`buildApp`,
`BuildGateway`, `NewNetworkSubmitters`) and imports `synchronizer` and
`endorser/app` instead of owning either's concerns.

## PR Sequence

**0. Make hybridx block loss non-silent.** Pre-existing bug, no dependency on
   anything else here: a handler error restarts the notification stream at the
   tip and drops a block with only a `Warnf`
   (`gateway/app/hybridx/hybrid.go:160-168`). Either surface the dispatch error
   through the errgroup so the process fails loudly, or record the failed block
   and resume there. **Merged as
   [PR #343](https://github.com/hyperledger/fabric-x-evm/pull/343).**

**1. Idempotent block handlers.** Prerequisite for every later PR that shares
   one synchronizer across stores, and a correctness fix in its own right — see
   constraint 1. Each block handler skips blocks at or below its own
   last-applied height, so re-delivery is harmless.

   Height alone cannot express "nothing applied yet", since a fresh store and
   one holding only block 0 both report 0, so each handler needs an explicit
   has-applied signal. `PebbleKVS` already had one (`hasCheckpoint`) and is the
   reference the others follow.

   It closes the same hole in `testnode` and the integration harness, which run
   this pattern today and are only safe because they are in-memory and never
   restart.

   **Merged as [PR #355](https://github.com/hyperledger/fabric-x-evm/pull/355)
   ("Idempotency for world state stores, with parity tests").** Landed on top of
   the `fabric-x-sdk` bump that made `VersionedDB` replay-safe
   ([fabric-x-sdk#57](https://github.com/hyperledger/fabric-x-sdk/pull/57)):
   `LightKVS` and `PebbleKVS` now match its stricter per-write behavior too — a
   redelivered block is a no-op only if its content is identical to what's
   already stored, and errors otherwise, rather than skipping on block number
   alone. `PebbleKVS` checks this at any replay depth (it retains full history);
   `LightKVS` only at the current tip, which is also the only case a resuming
   synchronizer actually produces.

**2. Unify the co-located endorser onto one synchronizer.** Switch the
   embedded-endorser construction in `gateway/app` from `eapp.NewEndorser`
   (self-syncing) to `eapp.NewEndorserCore` + registering its KVS as a handler
   on the process's one synchronizer, matching `defaultHandlerChain` and
   `testnode.go`. Removes `App.endorserSyncs` and its start/wait loop. Adds the
   missing `WaitUntilSynced` on `App.synchronizer`. No config restriction
   needed.

   Renames `NewGatewaySynchronizer` to `NewSynchronizer` (and `App.gwSync` to
   `App.synchronizer`) in the same PR: the function was always generic
   (protocol dispatch over an arbitrary handler list, no `core.Gateway`
   dependency) and only ever named after its one caller's use of it. Now that
   it is the one synchronizer for the whole process rather than "the
   gateway's" as opposed to the endorser's own, the old name undersells it.

   No min-height `BlockHeightReader` is needed even though the synchronizer
   now feeds two stores: handler order stays `[kvs, chain, gateway]`, so
   within one continuously-running process chain's height can never get ahead
   of kvs's, and PR 1's idempotency guards make resuming from `chain` alone
   (unchanged) safe — the redelivery it causes to kvs is a no-op once kvs is
   already caught up. `eapp.NewEndorser` (self-syncing) keeps its one
   remaining caller, `integration/split_endorsers.go`'s
   `startEndorserGRPCServer`, until PR 4 — which rewires that caller onto the
   unified `NewEndorserCore` + `synchronizer.New` construction instead (see
   [Package boundaries](#package-boundaries-gateway-endorser-and-shared-synchronizer-wiring)),
   deleting `eapp.NewEndorser` rather than reusing it.

   **Merged as
   [PR #360](https://github.com/hyperledger/fabric-x-evm/pull/360)
   ("unify synchronizer, update config structure").** Landed as planned: no
   divergence from this section.

   Hoists `committer` to the top level in the same PR, per
   [One committer per process](#one-committer-per-process-configured-at-the-top-level)
   — a bare field, not nested under a component-shaped wrapper, so it keeps
   describing "how this process reaches the committer" independent of
   whatever internal component (a `Synchronizer` today) consumes it. It
   belongs here and not in its own PR: this is the change that makes "one
   committer per process" true, so splitting them would leave
   `endorser.committer` dead-but-required for the gap. Touches both
   `Validate()` bodies, every construction/test-fixture site that set
   `gateway.committer` or `endorser.committer`, the nine `integration/*.yaml`
   configs, and `docs/ENDORSEMENT_API.md`. Three of those configs (plus one
   Go test fixture) lose a verbatim-duplicated block outright.

   Rides along on the same reshape: `gateway.sync-timeout`, which nothing
   read, is replaced by a new top-level `synchronizer.timeout` (component
   *behavior*, deliberately kept separate from the committer *connection*
   info) and is actually wired into the new `WaitUntilSynced(a.synchronizer,
   …)` call above via `Synchronizer.SyncTimeout()` (falls back to
   `DefaultSyncTimeout` when unset, mirroring the `Endorser.TimestampFutureSkew()`
   default-getter pattern).

   No dependency on gRPC serving.

**3. Endorser gRPC serving config — and its first consumer.** Add the
   `server` block to `endorser/config.Endorser` (type `serve.ServerConfig`, see
   [Shape of the endorser's gRPC server config](#shape-of-the-endorsers-grpc-server-config))
   with `Validate()` coverage, *and* switch
   `integration/split_endorsers.go`'s `serveEndorser` to read it instead of
   deriving TLS paths from `filepath.Dir(MSPDir)/tls`. Update
   `fabx-2of2-org1.yaml` / `fabx-2of2-org2.yaml` in the same PR — `config.Load`
   uses `EnhancedExactUnmarshal`, so a YAML key with no matching struct field
   is an error, and the two cannot land separately. Having a real consumer from
   day one proves the schema rather than shipping a field nothing reads.

   **Open as [PR #366](https://github.com/hyperledger/fabric-x-evm/pull/366)
   ("add grpc server config to endorser").** Landed as planned: no divergence
   from this section.

**4. Endorser-only process.** `Config.Gateway` becomes `*Gateway`; rewrite
   `Validate()` so gateway-specific checks run only when `Gateway != nil` and at
   least one of the two sections is set. `fxevm start` branches on
   `cfg.Gateway == nil` — gateway + embedded endorser if `gateway:` is present,
   endorser-only otherwise, dispatching to the new `endorser/app` entrypoint
   below. `healthcheck` needs no new mode — its existing bare TCP connect
   against `--addr` works unchanged pointed at an endorser's gRPC port instead
   of a gateway's HTTP one; see [One `start` command,
   config-driven](#one-start-command-config-driven) for why a real gRPC health
   RPC isn't worth adding yet. Fixes the `config.Gateway{…}`
   literal at `gateway/app/testnode.go:111`. Rewires
   `integration/split_endorsers.go`'s `startEndorserGRPCServer` onto that
   production path — which is what tests this PR.

   Package boundaries and what moves where — the new top-level
   `synchronizer` package, `endorser/app` gaining the standalone entrypoint,
   `NewEndorserCore` absorbing `HistorySize` defaulting, and `eapp.NewEndorser`
   being deleted rather than reused — are settled in [Package boundaries:
   gateway, endorser, and shared synchronizer
   wiring](#package-boundaries-gateway-endorser-and-shared-synchronizer-wiring)
   above. The standalone entrypoint builds its endorser with
   `NewEndorserCore` + `synchronizer.New(..., kvs)`, the same construction a
   gateway uses for its own embedded endorser, then serves it over gRPC
   **inline, inside the errgroup** (no standalone helper — see that section
   for why, and why PR 5 should extract one instead of duplicating it) so a
   listen failure takes the process down instead of being logged, and rejects
   a missing `server:` block.

   **Not** in this PR: the "`Gateway` requires `Endorser`" rule. That rule
   outlaws a mode `newSplitApp` still implements and the integration suite
   still exercises (it bypasses `Validate()`), so adding it here would create
   exactly the config/code drift this plan exists to remove. It belongs in 5.

**5. One wiring path: mandatory local endorser plus optional remotes.** Delete
   `newSplitApp` and the `newApp` dispatch; build the co-located endorser
   (called directly in-process) and dial the configured remotes, then pass
   them as distinct `local`/`remotes` parameters through `buildApp` →
   `BuildGateway` → `NewEndorsementClient`, per [Local endorser is a distinct
   field, not slice position
   0](#local-endorser-is-a-distinct-field-not-slice-position-0) — not a single
   pre-ordered `[]api.Service`. Drop the `Endorser` / `Gateway.Endorsers`
   mutual exclusion and add the "`Gateway` requires `Endorser`" rule now, in
   the PR where the code makes it true. Start the gRPC server for the
   gateway's own endorser when `endorser.server` is configured, so other orgs
   can reach it — extract the inline `server.New(...).Serve(...)` call
   `endorser/app.App.Run` already has (PR 4) into a shared function both sides
   call, now that a second caller actually exists. Update `fabx-2of2.yaml`
   to embed org1's own `endorser:` directly, matching real topology, instead of
   the test spinning up org1 as a wholly separate process. Drop the
   test-RPC-with-remote-endorsers rejection (`gateway/app/app.go:88-90`): the
   embedded endorser and its KVS are now always present, which is the only
   reason that restriction existed.

   Test coverage worth adding here: the two-org topology no longer exercises a
   gateway *serving* its own endorser to anyone, so add a check that dials the
   gateway's served endorser directly.

   This is also the point where a single process can have two listeners for
   the first time — a gateway's HTTP port and, when `endorser.server` is set,
   its own endorser's gRPC port. `healthcheck` (PR 4) stays a single-address
   check rather than growing an array: composing two separate probes at the
   deployment layer (`CMD fxevm healthcheck --addr :8545 && fxevm healthcheck
   --addr :9001` for Docker, or two `tcpSocket` probes for Kubernetes) already
   covers it, so a container that serves both needs its `HEALTHCHECK` /
   probe spec updated when this PR lands, not a code change here.

**6. Audit the integration harness against the production constructors.** With
   one wiring path in production, check what `integration/` still builds by
   hand and re-point it where the production constructor is now equivalent.
   This is an audit that may end in a refactor, not a refactor with a
   predetermined shape — but the specific divergences to judge are known:

   - `buildTestHarness` (`test_helpers.go:197-291`) re-derives the gateway
     signer, converts orderer configs, and re-defaults `SubmitterCount` that
     `NewNetworkSubmitters` already defaults (`wiring.go:39-41`) — all mirroring
     `newApp`/`buildApp` rather than calling them. It already shares
     `NewNetworkSubmitters`, `BuildGateway` and `NewSynchronizer`, so the
     gap is assembly and lifecycle, not primitives.
   - `defaultHandlerChain` (`test_helpers.go:161-183`) duplicates production's
     handler ordering behind a 20-line comment that has to be kept true by
     hand, against production's one-line `append(extraHandlers, chain, gateway)`.
     After PR 1 these are the same list, and the comment is the only copy of the
     reasoning — it should end up on the production line.
   - Tests never call `Config.Validate()`, which is why several config rules in
     this plan can be broken by the suite without failing it. Harness configs
     going through the real entry point closes that hole.
   - Four constructors (`NewLocalTestHarness`, `newFileConfigHarness`,
     `newSplitFileConfigHarness`, `NewFabricXTestHarnessWithNotifications`) sit
     on one builder, distinguished mostly by where the config comes from.
   - Nothing in the harness exercises `App` itself — no HTTP surface, no
     `WaitUntilSynced`, no `Shutdown` — because tests drive `*core.Gateway`
     directly. `buildSplitGatewayApp` (`split_endorsers.go:150-189`) already
     shows the real `App` working under test; it is the model to widen.

   Genuinely test-only seams stay: `localSigner`, `bypass` mode's local
   submitter, injected `txQueue`, and extra block handlers for perf tests. The
   goal is that the harness differs from production only where a test needs it
   to, and that the difference is visible.

PRs 0-4 are small and independently useful. PR 5 is where runtime topology
actually changes and deserves the most scrutiny — lean on the existing
`TestGRPCEndorsement_Parity`-style checks there. PR 6 is cleanup that only
becomes possible once 5 has collapsed production onto one path.
