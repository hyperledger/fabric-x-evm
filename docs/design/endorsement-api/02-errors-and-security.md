# Endorsement API Design — Errors and Security

> Part 2 of the endorsement API design. It defines how errors travel across the
> gRPC boundary and the security and resilience properties the boundary must
> provide. See [00-overview.md](00-overview.md) for framing and
> [01-api-and-proto.md](01-api-and-proto.md) for the service and messages.

## Table of Contents

- [Scope](#scope)
- [Error Model Today](#error-model-today)
- [Three Error Classes](#three-error-classes)
- [Two Channels: gRPC Status vs In-Band Response](#two-channels-grpc-status-vs-in-band-response)
- [Error Mapping](#error-mapping)
- [Transport Security: mTLS](#transport-security-mtls)
- [Authentication and Authorization](#authentication-and-authorization)
- [Resilience](#resilience)
- [Decisions and Alternatives](#decisions-and-alternatives)

## Scope

How the API represents and transports failures, and how the boundary is secured
and kept resilient. Configuration mechanics (where certs and timeouts are set)
are in part 3; this part defines *what* the semantics must be.

## Error Model Today

The endorser already distinguishes outcomes by status code in
[`endorser/endorser.go`](../../../endorser/endorser.go):

- **200 OK** - success, payload carries the result.
- **201** - the EVM **reverted**; still in the success range so a transaction is
  cut and the receipt records `status=0`, but distinguishable from 200.
- **500** - an **execution error** occurred; returned in the `ProposalResponse`.
- **Go error return** (`nil, err`) — a **pre-execution validation error**
  (nonce, gas, signer, EIP-3860, blob checks); the transaction is rejected
  before any envelope is cut.

The gateway consumes these in
[`gateway/core/endorse.go`](../../../gateway/core/endorse.go): status `201`
becomes a `*domain.RevertError` (mapped to JSON-RPC `-32000` with reason and
data), any status outside `200–399` becomes a generic error.

## Three Error Classes

Mapping to the classes the design must preserve:

1. **Revert** - deterministic EVM revert. Carries a reason string and revert
   data. Not a failure of the infrastructure; the transaction is valid and gets
   committed with `status=0`. Status `201`.
2. **Execution error** - the transaction executed but failed (out of gas,
   invalid opcode). Status `500`, message in the response.
3. **Server / transport error** - the endorser could not produce a result:
   pre-execution validation rejection, endorser unavailable, timeout, internal
   panic. These are *not* normal endorsement outcomes.

## Two Channels: gRPC Status vs In-Band Response

gRPC gives us two independent channels; using both preserves the current
two-outcome contract (`(response, nil)` vs `(nil, err)`):

- **In-band `peer.ProposalResponse`** (gRPC status `OK`): normal endorsement
  outcomes that the gateway must inspect — success (200), revert (201), and
  execution error (500). The RPC succeeded; the *result* carries the status.
- **gRPC error status**: the endorser could not deliver a valid endorsement —
  pre-execution rejection (`INVALID_ARGUMENT` / `FAILED_PRECONDITION`),
  unavailable (`UNAVAILABLE`), deadline (`DEADLINE_EXCEEDED`), internal
  (`INTERNAL`). This maps to the current `(nil, err)` return.

To preserve the exact go-ethereum error for JSON-RPC mapping, pre-execution
rejections attach structured details (`google.rpc.Status` details or a typed
error message) rather than collapsing to an opaque string.

## Error Mapping

| Outcome | Endorser today | gRPC channel | Gateway result |
|---------|----------------|--------------|----------------|
| Success | status 200 | in-band, gRPC OK | payload |
| Revert | status 201 | in-band, gRPC OK | `RevertError` → JSON-RPC `-32000` |
| Execution error | status 500 | in-band, gRPC OK | generic error |
| Pre-execution reject | `nil, err` | `INVALID_ARGUMENT` + details | typed error (nonce/gas/etc.) |
| Endorser down | n/a | `UNAVAILABLE` | retryable error |
| Timeout | n/a | `DEADLINE_EXCEEDED` | retryable error |
| Server panic / bug | n/a | `INTERNAL` | non-retryable error |

The gateway's client translates gRPC status + in-band status back into the same
Go errors and `ProposalResponse` values it produces today, so the JSON-RPC
layer above is unchanged.

## Transport Security: mTLS

- **mTLS is required.** Both sides present certificates: the gateway
  authenticates the endorser, and the endorser authenticates the gateway. No
  plaintext or server-only-TLS fallback.
- Reuse the existing TLS material and conventions already used for the endorser
  ↔ committer connection (`PeerConf.TLSPath` in
  [`endorser/config`](../../../endorser/config/config.go)); the new API adds its
  own server-side listener credentials plus client credentials on the gateway.
- Pin a modern TLS floor (TLS 1.2+/1.3) and support certificate rotation
  without dropping in-flight requests.

## Authentication and Authorization

- **Authentication:** the client identity is the mTLS peer certificate. The
  endorser only accepts connections presenting a trusted client cert.
- **Authorization:** restrict callers to the trusted gateway(s). Options: a CA
  trust anchor scoped to gateway certs, or an explicit allowlist of client
  identities / MSP IDs. The endorser is not a public endpoint.
- **Identity vs signing:** this is orthogonal to the MSP identity the endorser
  uses to *sign* endorsements — that behavior is unchanged. mTLS governs *who
  may call*; the endorsement signature governs *who endorsed*.

## Resilience

- **Deadlines:** the RPC honors the caller's context deadline; the current code
  already threads `context.Context` through every operation, so this is a
  transport-level pass-through.
- **Retries:** transient failures (`UNAVAILABLE`, `DEADLINE_EXCEEDED`) are
  retryable. Retry policy lives with the caller, and must be **coordinated with
  the mempool retry work (#50)** to avoid double-retrying the same transaction
  at two layers. Non-transient errors (`INVALID_ARGUMENT`, revert, execution
  error) are not retried.
- **Connection management:** long-lived, pooled connections with keepalive
  between gateway and endorser rather than per-call dial cost.
- **Backpressure:** bound in-flight requests / max concurrent streams so a
  surge does not exhaust the endorser; surface overload as `RESOURCE_EXHAUSTED`
  so the caller can back off.

