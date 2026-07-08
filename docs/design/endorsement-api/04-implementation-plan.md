# Endorsement API Design — Implementation Plan

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
- [Decisions and Alternatives](#decisions-and-alternatives)

## Scope

The order in which the change lands, sized so each PR is independently
reviewable and mergeable, and so `main` stays green and behavior-compatible at
every step.

## Principles

- **Behind the interface.** Every step slots behind the existing `Endorser`
  interface; the gateway above it does not change.
- **Parity at each step.** The embedded (in-process) path keeps passing its
  current tests throughout; the split path is added, not substituted.
- **Additive config.** New config blocks are opt-in; absent them, deployments
  behave exactly as today.
- **Small PRs.** Schema, server, client, wiring, and tests land separately.

## PR Sequence

1. **Proto + generated code.**
   Add the `.proto` for the `EvmEndorsement` service and its generated Go. No
   wiring yet. Reviewable purely as a schema. Locks the contract from part 1.

2. **Server: gRPC endorser.**
   A gRPC server that wraps the existing `endorser.Endorser`, forwarding
   `ProcessTransaction` / `ProcessCall` / `ProcessStateQuery` and preserving
   status codes and pre-execution rejections (part 2). Adds the endorser
   listen + server-TLS config. No gateway change.

3. **Client: gRPC-backed `Endorser`.**
   A `grpcEndorser` implementing the three-method interface, with request
   marshaling and gRPC-status/in-band translation, plus connection lifecycle.
   Unit-tested against a mock server, including the error-mapping table.

4. **Config + app wiring.**
   Gateway builds `grpcEndorser` values from `[]common.ClientConfig` endorser
   endpoints; embedded vs split chosen by which config block is present. The
   in-process path is untouched.

5. **Integration across the boundary.**
   Stand up an endorser gRPC server and a gateway client over mTLS in the
   integration suite; exercise transaction, call, and state-query end to end,
   plus the security and resilience negatives.

6. **Deployment surface (maintainer-gated).**
   Example split-deployment wiring (compose files / run scripts) and operator
   docs. These touch project-level deployment config, so they are proposed
   separately and only after maintainer sign-off.

## Rollout and Migration

- The embedded single-binary deployment remains the default and is never
  removed by this work.
- Split deployment is opt-in via config and can be adopted per environment.
- Because both paths share the `Endorser` interface and the parity tests, a
  deployment can move from embedded to split (or back) without gateway code
  changes.

## Dependencies and Open Items

Carried forward from earlier parts; these should be resolved as the relevant PR
lands, not all up front:

- **Invocation fields** the server-side builder needs (part 1) - confirmed
  before PR 1 finalizes the `TransactionRequest` shape.
- **Code reuse** - duplicate vs depend vs upstream to fabric-x-common
  (the @senthil question) - decided before PR 2, since it determines where the
  server plumbing comes from.
- **Streaming** - unary ships first (PRs 1-5); a streaming variant is a later,
  additive PR only if the mempool throughput work (#50) needs it.

## Risks and Rollback

- Each PR is behind the interface and additive, so reverting any single step
  leaves the embedded path intact.
- The highest-risk step is PR 3 (client translation): a mapping mistake would
  surface as an error/receipt regression. The parity tests against the
  in-process baseline are the guard.
- No schema is exposed to external clients, so there is no public-API
  compatibility surface to manage.

