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

The gateway already depends on the `Endorser` interface, and the
`EndorsementClient` fans out to a slice of `Endorser` values
([`gateway/core/endorse.go`](../../../gateway/core/endorse.go)). The gRPC
client is **a new implementation of that same interface** - it does not change
`EndorsementClient` or anything above it.

- A `grpcEndorser` type implements `ProcessEVMTransaction`, `ProcessCall`, and
  `ProcessStateQuery` by marshaling the request, invoking the gRPC method, and
  returning the `peer.ProposalResponse`.
- The existing fan-out, deterministic error ordering, and multi-endorser
  parallelism in `ExecuteTransaction` stay exactly as they are; each element of
  the `endorsers` slice is simply a gRPC-backed `Endorser` instead of an
  in-process one.
- Connection lifecycle (dial, keepalive, pooling, close) lives inside the
  `grpcEndorser`, created once at startup from config and reused across calls.

## Calling Our Own Endorser

"How do we call our own endorser?" - the gateway constructs its `endorsers`
slice from configured endpoints at startup, the same way it already builds
orderer and committer clients:

- In the **embedded** deployment, the gateway keeps constructing in-process
  endorsers via `endorser/app.NewEndorser` (no gRPC), preserving today's
  single-binary path.
- In the **split** deployment, the gateway builds `grpcEndorser` values from a
  list of endorser endpoints and dials them over mTLS.

Selection is a configuration concern (see below), not a code-path the caller
has to know about - both satisfy the `Endorser` interface.

## Configuration

### Endorser (Server) Config

The endorser today has no network listener - it is embedded
([`endorser/config`](../../../endorser/config/config.go)). The API adds:

- a **listen address** for the gRPC server,
- **server TLS** credentials (cert, key, client-CA for mTLS),

reusing the existing TLS shape from `common.ClientConfig` /
`common.TLSConfig` (Mode, CertPath, KeyPath, CACertPaths, ServerName) so config
stays consistent with the orderer/committer connections.

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
  server with its own listen + server-TLS config.

The two are mutually exclusive per gateway and chosen by which config block is
present, so no existing deployment changes behavior.

## Testing

- **Unit - client:** table tests for `grpcEndorser` request marshaling and
  response/error translation, using a mock gRPC server. Assert that gRPC status
  + in-band `ProposalResponse` map back to the same Go values the in-process
  path returns (per the mapping table in part 2).
- **Unit - server:** the gRPC handler wraps the existing `Endorser`; test that
  it forwards to the underlying implementation and preserves status codes
  (200 / 201 / 500) and pre-execution rejections.
- **Interface parity:** run the same `EndorsementClient` tests against both an
  in-process `Endorser` and a gRPC-backed one to prove behavioral equivalence.
- **Integration - across the boundary:** stand up an endorser gRPC server and a
  gateway client over real (m)TLS in the integration suite; exercise
  transaction, call, and state-query paths end to end.
- **Security:** negative tests - missing/untrusted client cert is rejected,
  plaintext connection is refused.
- **Resilience:** endorser-unavailable and deadline paths surface as the
  expected retryable gRPC statuses; confirm no double-retry against the mempool
  layer (#50).
- **Backward compatibility:** the embedded path keeps passing the existing
  endorser and gateway suites unchanged.

## Decisions and Alternatives

**D3.1 - Implement the gRPC client as another `Endorser`.** Keeps
`EndorsementClient` and the whole gateway above it untouched. *Alternative:*
add a gRPC-aware layer in the gateway - rejected, needless churn behind an
interface that already exists for this.

**D3.2 - Reuse `common.ClientConfig` for endorser endpoints.** Same shape as
orderers/committer, so validation and TLS wiring are shared. *Alternative:* a
bespoke endorser-client config - rejected, duplicates existing config plumbing.

**D3.3 - Keep embedded and split deployments mutually exclusive per gateway.**
The embedded path stays the default and unchanged; split is opt-in by config.
*Alternative:* always route through gRPC (loopback for embedded) - rejected for
now, it adds serialization cost to the single-binary path with no benefit.

**D3.4 - Prove parity by running shared tests against both `Endorser`
implementations.** Behavioral equivalence is the core correctness property of
this change. *Alternative:* test only the gRPC path - rejected, parity with the
in-process baseline is exactly what must be guaranteed.
