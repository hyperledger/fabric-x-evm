# Endorsement API Design - Implementation Plan

> Part 4 of the endorsement API design. It sequences the implementation into
> small, low-disruption PRs. See [00-overview.md](00-overview.md) for framing,
> and parts [1](01-api-and-proto.md), [2](02-errors-and-security.md), and
> [3](03-client-config-testing.md) for the API, errors/security, and
> client/config/testing.

## Table of Contents

- [Scope](#scope)
- [Principles](#principles)
- [PR Sequence](#pr-sequence)
- [Rollout and Migration](#rollout-and-migration)
- [Dependencies and Open Items](#dependencies-and-open-items)
- [Risks and Rollback](#risks-and-rollback)

## Scope

The order in which the change lands, sized so each PR is independently
reviewable and mergeable, and so `main` stays green and behavior-compatible at
every step.

## Principles

- **Behind the interface.** Every step slots behind the `endorser/api.Service`
  interface; the gateway above it does not change.
- **Dormant until enabled.** The gRPC path is built piece by piece, but nothing
  reaches it until one explicit enable step near the end. Every PR merges with
  `main` green and behavior unchanged - the feature is invisible until switched
  on.
- **Parity at each step.** The in-process path keeps passing its current tests
  throughout; the gRPC path is added and proven, not substituted.
- **Additive config.** New config is opt-in; absent it, deployments behave
  exactly as today. Enabling the feature is a config choice, not a code fork.
- **Small, modular PRs.** Schema, server, client, tests, and the enable switch
  land separately and are each independently reviewable and revertible.

## PR Sequence

Steps 1-5 are all **dormant and additive** - each merges with `main` green while
nothing calls the new path. Step 6 is the **single switch** that turns the
feature on, and it lands only once 1-5 are proven green.

1. **Refactor `api.Service` to the per-function shape.**
   Evolve `endorser/api.Service` from the current three-method /
   `peer.ProposalResponse` contract to the six per-function methods (`Execute`,
   `Call`, `BalanceAt`, `StorageAt`, `CodeAt`, `NonceAt`) and update the
   in-process `core.Endorser` to match. Confirmed with @arner - the interface is
   not consumed elsewhere yet, so we are free to shape it. No gRPC yet; the
   embedded path keeps working.

2. **Proto + generated code.**
   Add the `.proto` for the six-method `EvmEndorsement` service and its
   generated Go. Reviewable purely as a schema; nothing calls it yet.

3. **Server: gRPC endorser.**
   A gRPC server that adapts the six RPCs onto `core.Endorser` via `api.Service`,
   built on the `serve` package from fabric-x-common (listen, mTLS, keep-alive,
   health). It compiles and starts, but nothing dials it yet. No gateway change.
   Gated on committer #675 landing `serve` in fabric-x-common (see Dependencies).

4. **Client: gRPC-backed `api.Service`.**
   A `grpcEndorser` implementing the six methods, with request marshaling and
   status/error translation, plus connection lifecycle. Unit-tested against a
   mock server (including the error-mapping table); not yet wired into the
   gateway.

5. **Integration across the boundary.**
   Stand up the server and a gRPC client over mTLS in the integration suite and
   prove the transaction, call, and state-read paths end to end, plus the
   security and resilience negatives - all without changing the gateway's
   default path.

6. **Enable: config + app wiring.**
   Wire the gateway to build `grpcEndorser` values from `[]common.ClientConfig`
   endorser endpoints, selected by config. This is the one PR that turns the
   feature on, and it lands only after 1-5 are green. Absent the config, behavior
   is unchanged.

7. **Deployment surface (maintainer-gated).**
   Example split-deployment wiring (compose files / run scripts) and operator
   docs. These touch project-level deployment config, so they are proposed
   separately and only after maintainer sign-off.

## Rollout and Migration

- The in-process endorser build is unchanged; what changes is that the gateway
  reaches even its co-located endorser over gRPC (localhost) once the server and
  client land.
- Remote (other-org) endorsers are added by configuring their endpoints - no
  gateway code change, since every endorser is the same `api.Service` client.
- The parity tests keep the migration honest: the gRPC path must match the
  in-process baseline before the switch is flipped.

## Dependencies and Open Items

Carried forward from earlier parts; these should be resolved as the relevant PR
lands, not all up front:

- **`serve` in fabric-x-common (sequencing).** The server bootstrap comes from
  the `serve` package (+ `connection`, `retry`), which @arner is publishing into
  fabric-x-common via committer
  [#675](https://github.com/hyperledger/fabric-x-committer/issues/675) - agreed
  by the committer maintainers. This gives us the bootstrap with **no
  fabric-x-committer dependency** and the right dependency direction. PR 3 is
  gated on #675 landing and evm bumping to that fabric-x-common version; it does
  not change the rest of the plan.
- **`fabricx.TxPackager`** - confirm it accepts a transaction without the
  proposal bytes (the Fabric-X "no proposal on the wire" decision from part 1)
  before PR 4 fixes the `Execute` request shape.
- **Streaming** - unary ships first; benchmark against the 15-20k tps target and
  add an `ExecuteStream` only if per-call overhead proves to be the bottleneck
  (additive, non-breaking).

## Risks and Rollback

- Steps 1-5 are dormant and additive, so reverting any of them leaves the
  in-process path intact; the enable step (PR 6) is a single config switch that
  can be turned off or reverted on its own.
- The highest-risk steps are the `api.Service` refactor (PR 1) and the client
  translation (PR 4): a mistake would surface as an error/receipt regression.
  The parity tests against the in-process baseline are the guard.
- No schema is exposed to external clients, so there is no public-API
  compatibility surface to manage.
