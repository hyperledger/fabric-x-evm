# Endorsement API Design - Client, Configuration, and Testing

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
  - [Co-located and Remote Endorsers](#co-located-and-remote-endorsers)
- [Testing](#testing)

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

> **Resolved (with @arner):** `api.Service` is refactored from its current
> `ProcessEVMTransaction` / `ProcessCall` / `ProcessStateQuery` /
> `peer.ProposalResponse` shape to the six per-function methods above. It is not
> consumed elsewhere yet, so we have the freedom to shape it to fit the API
> cleanly; the in-process `core.Endorser` and the gRPC client then share one
> contract.

## Calling Our Own Endorser

There are no standalone endorsers today, so **every gateway also runs an
endorser**, and the common case is multi-org endorsement - e.g. Org A collecting
endorsements from Org A, Org B, and Org C. The gateway holds one gRPC
`api.Service` client per org it endorses with:

- **Other orgs' endorsers** are dialed remotely over mTLS - straightforward.
- **The gateway's own co-located endorser** (same org, same process) is reached
  over the **same gRPC path on localhost**, not through a special in-process
  shortcut. Only the address differs.

Routing the self-call over a localhost loopback is slightly awkward - it
serializes a call to the same process - but it keeps **one uniform code path**
for every endorser, which is the simpler thing to build for v1. This may be
revisited later: a local first-endorsement pass could determine the dirty
read/write set (Ethereum AccessList-style) to warm the storage cache, seed a
bulk committer query, and feed the dependency graph (#59). If we add that, the
self-call may become a direct in-process step.

## Configuration

### Endorser (Server) Config

The endorser today has no network listener - it is embedded
([`endorser/config`](../../../endorser/config/config.go)). The API adds a gRPC
server whose config carries:

- **endpoint** (listen address),
- **TLS** (`mode: mtls`, `cert-path`, `key-path`, `ca-cert-paths`) - the exact
  committer format from part 2, with a TLS 1.2 floor,
- **keep-alive**, **max-concurrent-streams**, and **rate-limit** - the
  backpressure knobs part 2 calls for.

The server bootstrap (listen, TLS, interceptors, health, config) comes from the
`serve` package, which is being **published into fabric-x-common** (committer
[#675](https://github.com/hyperledger/fabric-x-committer/issues/675), agreed by
the committer maintainers). fabric-x-evm already depends on fabric-x-common, so
this reuses battle-tested serving code **without any fabric-x-committer
dependency** and in the right dependency direction. The config shape above is
`serve`'s own config.

### Gateway (Client) Config

The gateway config already lists `Orderers []common.ClientConfig` and a
`Committer common.ClientConfig`
([`gateway/config/config.go`](../../../gateway/config/config.go)). The endorser
client fits the same mold: a list of endorser endpoints, each a
`common.ClientConfig` (endpoint + TLS). This reuses `Endpoint.Address()`,
`Validate()`, and the existing TLS wiring rather than inventing new config.

### Co-located and Remote Endorsers

- **Co-located endorser:** built in-process as today
  (`Endorsers []endorser.Endorser`), but it now also runs the gRPC server, and
  the gateway reaches it as a localhost endpoint rather than an in-process call.
- **Remote endorsers:** other orgs' endorsers, dialed from configured endpoints
  (`[]common.ClientConfig`) over mTLS.

Both are the same `api.Service` gRPC client; only the address differs. Each
endorser process runs the gRPC server with its own config (endpoint + mTLS +
keep-alive/limits).

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
