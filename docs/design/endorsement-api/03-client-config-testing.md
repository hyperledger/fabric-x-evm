# Endorsement API Design — Client, Configuration, and Testing

> Part 3 of the endorsement API design. It covers the gateway-side endorsement
> client, the configuration on both ends, and the testing strategy. See
> [00-overview.md](00-overview.md) for framing, [01-api-and-proto.md](01-api-and-proto.md)
> for the service, and [02-errors-and-security.md](02-errors-and-security.md)
> for errors and security.

## Table of Contents

- [Scope](#scope)
- [Endorsement Client](#endorsement-client)
- [Calling Our Own Endorser](#calling-our-own-endorser)
- [Configuration](#configuration)
  - [Endorser (Server) Config](#endorser-server-config)
  - [Gateway (Client) Config](#gateway-client-config)
  - [Embedded vs Split Deployment](#embedded-vs-split-deployment)
- [Testing](#testing)
- [Decisions and Alternatives](#decisions-and-alternatives)

## Scope

How the gateway talks to the endorser over the new API, what configuration each
side needs, and how we test the boundary. Builds directly on the service and
error/security decisions from parts 1 and 2.

## Endorsement Client

The endorser publishes its contract as the
[`endorser/api.Service`](../../../endorser/api/service.go) interface, and the
gateway's `EndorsementClient` fans out to a slice of `api.Service` values (wired
in [`gateway/app/wiring.go`](../../../gateway/app/wiring.go)). The interface's
own doc comment already names "a future gRPC client/server pair" as an intended
implementation, so the gRPC client is **a new implementation of `api.Service`** -
it does not change `EndorsementClient` or anything above it.

- A `grpcEndorser` type implements `api.Service` by marshaling each request,
  invoking the matching gRPC method, and returning the typed result. Per the
  part-1 design the methods are the six per-function calls (`Execute`, `Call`,
  `BalanceAt`, `StorageAt`, `CodeAt`, `NonceAt`); only `Execute` returns a
  signed result, the reads return plain typed values.
- The existing fan-out, deterministic error ordering, and multi-endorser
  parallelism stay exactly as they are; each element of the `endorsers` slice is
  simply a gRPC-backed `api.Service` instead of an in-process one.
- Connection lifecycle (dial, keepalive, pooling, close) lives inside the
  `grpcEndorser`, created once at startup from config and reused across calls.

> **Open question (maintainer):** the #229 restructure introduced `api.Service`
> with the pre-redesign shape (`ProcessEVMTransaction` / `ProcessCall` /
> `ProcessStateQuery` returning `peer.ProposalResponse`). Adopting the part-1
> design means evolving `api.Service` to the six per-function methods above, so
> the in-process `core.Endorser` and the gRPC client share one clean contract.
> Confirm we should refactor `api.Service` this way - its comment currently says
> the gRPC pair would implement it "without changing this contract".

## Calling Our Own Endorser

"How do we call our own endorser?" - the gateway constructs its `endorsers`
slice from configured endpoints at startup, the same way it already builds
orderer and committer clients:

- In the **embedded** deployment, the gateway keeps constructing in-process
  endorsers via `endorser/app.NewEndorser` (no gRPC); the in-process
  `core.Endorser` satisfies `api.Service` directly, preserving today's
  single-binary path.
- In the **split** deployment, the gateway builds `grpcEndorser` values from a
  list of endorser endpoints and dials them over mTLS.

Selection is a configuration concern (see below), not a code-path the caller
has to know about - both satisfy `api.Service`.

## Configuration

### Endorser (Server) Config

The endorser today has no network listener - it is embedded
([`endorser/config`](../../../endorser/config/config.go)). The API adds a gRPC
server, and per the part-1 code-reuse decision its config comes from the
committer's `serve` package rather than a hand-rolled struct. `serve.Config` /
`ServerConfig` already provides what the server needs:

- **endpoint** (listen address),
- **TLS** (`mode: mtls`, `cert-path`, `key-path`, `ca-cert-paths`) - the exact
  committer format from part 2, with the TLS 1.2 floor built in,
- **keep-alive**, **max-concurrent-streams**, and **rate-limit** - the
  backpressure knobs part 2 calls for.

So the endorser's server config is a `serve.Config` block alongside the existing
identity/database fields, not a new bespoke struct.

### Gateway (Client) Config

The gateway config already lists `Orderers []common.ClientConfig` and a
`Committer common.ClientConfig`
([`gateway/config/config.go`](../../../gateway/config/config.go)). The endorser
client fits the same mold: a list of endorser endpoints, each a
`common.ClientConfig` (endpoint + TLS). This reuses `Endpoint.Address()`,
`Validate()`, and the existing TLS wiring rather than inventing new config.

### Embedded vs Split Deployment

- **Embedded (current):** top-level config carries `Endorsers []endorser.Endorser`
  built in-process. Unchanged; the default.
- **Split (new):** the gateway carries endorser **client** endpoints
  (`[]common.ClientConfig`) and dials them; each endorser process runs the gRPC
  server with its own `serve.Config` (endpoint + mTLS + keep-alive/limits).

The two are mutually exclusive per gateway and chosen by which config block is
present, so no existing deployment changes behavior.

## Testing

- **Unit - client:** table tests for `grpcEndorser` request marshaling and
  response/error translation, using a mock gRPC server. Assert that the response
  status plus the gRPC/Go error map back to the same values the in-process path
  returns (per the mapping table in part 2).
- **Unit - server:** the gRPC handler wraps the in-process `core.Endorser`; test
  that it forwards each per-function call and preserves the status codes
  (200 / 201 / 400 / 460 / 500).
- **Interface parity:** run the same `EndorsementClient` tests against both an
  in-process `core.Endorser` and a gRPC-backed `api.Service` to prove behavioral
  equivalence.
- **Integration - across the boundary:** stand up an endorser gRPC server and a
  gateway client over real mTLS in the integration suite; exercise the
  transaction, call, and state-read paths end to end.
- **Security:** negative tests - missing/untrusted client cert is rejected,
  plaintext connection is refused.
- **Resilience:** endorser-unavailable and deadline paths surface as the
  expected retryable gRPC errors; confirm no double-retry against the mempool
  layer (#50).
- **Backward compatibility:** the embedded path keeps passing the existing
  endorser and gateway suites unchanged.

## Decisions and Alternatives

**D3.1 - Implement the gRPC client as another `api.Service`.** Keeps
`EndorsementClient` and the whole gateway above it untouched; `api.Service` is
the published contract the #229 restructure created for exactly this.
*Alternative:* add a gRPC-aware layer in the gateway - rejected, needless churn
behind an interface that already exists for this.

**D3.2 - Reuse `common.ClientConfig` (client) and the committer `serve.Config`
(server).** The client endpoints match orderers/committer; the server config
comes from `fabric-x-committer/utils/serve`, giving mTLS, keep-alive, and
backpressure for free (part-1 code-reuse decision). *Alternative:* a bespoke
endorser config on both ends - rejected, duplicates existing plumbing.

**D3.3 - Keep embedded and split deployments mutually exclusive per gateway.**
The embedded path stays the default and unchanged; split is opt-in by config.
*Alternative:* always route through gRPC (loopback for embedded) - rejected for
now, it adds serialization cost to the single-binary path with no benefit.

**D3.4 - Prove parity by running shared tests against both `api.Service`
implementations.** Behavioral equivalence is the core correctness property of
this change. *Alternative:* test only the gRPC path - rejected, parity with the
in-process baseline is exactly what must be guaranteed.

**D3.5 - Evolve `api.Service` to the six per-function methods.** So the
in-process `core.Endorser` and the gRPC client share one clean contract that
matches the part-1 API. *Alternative:* keep the three-method / `ProposalResponse`
`api.Service` and translate at the gRPC layer - rejected, it keeps two shapes and
re-introduces the proposal framing part 1 removed. (Flagged for maintainer
confirmation - see the open question above.)
