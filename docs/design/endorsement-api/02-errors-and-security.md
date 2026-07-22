# Endorsement API Design - Errors and Security

> Part 2 of the endorsement API design. It defines how errors travel across the
> gRPC boundary and the security and resilience properties the boundary must
> provide. See [00-overview.md](00-overview.md) for framing and
> [01-api-and-proto.md](01-api-and-proto.md) for the service and messages.

## Table of Contents

- [Scope](#scope)
- [Error Model in the Endorser](#error-model-in-the-endorser)
- [Error Classes](#error-classes)
- [Two Channels: Response Status vs gRPC Error](#two-channels-response-status-vs-grpc-error)
- [Error Mapping](#error-mapping)
- [Transport Security: mTLS](#transport-security-mtls)
- [Authentication and Authorization](#authentication-and-authorization)
- [Resilience](#resilience)

## Scope

How the API represents and transports failures, and how the boundary is secured
and kept resilient. Configuration mechanics (where certs and timeouts are set)
are in part 3; this part defines *what* the semantics must be.

## Error Model in the Endorser

After the endorser restructure (#229), the endorser returns **every application
outcome as a response with a status code** - never as a Go error. The Go error
return is reserved for real gRPC/connectivity faults. This already prepares the
code for the gRPC API and keeps application errors and infrastructure errors
cleanly separate. Status codes are defined in
[`common/proposal.go`](../../../common/proposal.go) and set in
[`endorser/core/endorser.go`](../../../endorser/core/endorser.go):

- **200 `StatusOK`** - success; payload carries the result.
- **201 `StatusEVMRevert`** - the EVM **reverted**; a committed outcome, the
  receipt records `status=0`.
- **400 `StatusTxRejected`** - an **invalid transaction**, rejected before
  execution (nonce, funds, intrinsic gas, signature).
- **460 `StatusExecFailure`** - a **valid transaction whose EVM execution
  failed** (out of gas, invalid opcode); should be mined (not yet).
- **500 `StatusServerError`** - a server-side fault (e.g. a signing failure).

`ProcessEVMTransaction` returns `(response, nil)` for all of these; the gateway
maps the status to its Ethereum response (e.g. a revert → JSON-RPC `-32000` with
reason and data).

## Error Classes

The status codes above fall into two groups:

**Application outcomes - carried in the response (status code):**

1. **Revert** (`201`) - deterministic EVM revert with reason + data. A committed
   outcome; the transaction is valid and mined with `status=0`.
2. **Tx rejected** (`400`) - an invalid transaction, rejected before execution
   (nonce, funds, intrinsic gas, signature).
3. **Exec failure** (`460`) - a valid transaction whose EVM execution failed
   (out of gas, invalid opcode).
4. **Server error** (`500`) - a server-side fault (e.g. a signing failure).

**Transport / connectivity - carried as a gRPC status error (Go error):**

- endorser unavailable, deadline exceeded, internal transport fault. These are
  *not* endorsement outcomes; only these ever surface as a gRPC error.

## Two Channels: Response Status vs gRPC Error

The API keeps application errors and infrastructure errors cleanly separate,
matching the restructured endorser:

- **Response status (in-band):** every application outcome. `Execute` carries it
  in the `ExecuteResponse` `status`/`message`/`payload` fields - OK (200),
  revert (201), tx-rejected (400), exec-failure (460), server-error (500). The
  RPC itself succeeds; the *result* carries the status. The read RPCs likewise
  return any application error (e.g. an `eth_call` revert) in their response, so
  the gateway can map it to JSON-RPC `-32000`.
- **gRPC error (Go error):** reserved for real transport/connectivity faults -
  `UNAVAILABLE`, `DEADLINE_EXCEEDED`, `INTERNAL`. Nothing application-level
  travels here.

This mirrors the endorser, where `ProcessEVMTransaction` returns
`(response, nil)` for every application outcome and reserves the Go error for
infrastructure faults.

## Error Mapping

| Outcome | Status | Channel | Gateway result |
|---------|--------|---------|----------------|
| Success | `200` | in-band response, gRPC OK | payload |
| Revert | `201` | in-band response, gRPC OK | mined with `status=0`; `eth_call` → JSON-RPC `-32000` |
| Tx rejected | `400` | in-band response, gRPC OK | typed error (nonce/funds/gas/signature) |
| Exec failure | `460` | in-band response, gRPC OK | execution error surfaced (mining planned) |
| Server error | `500` | in-band response, gRPC OK | generic error |
| Endorser down | - | gRPC `UNAVAILABLE` | retryable error |
| Timeout | - | gRPC `DEADLINE_EXCEEDED` | retryable error |
| Transport fault | - | gRPC `INTERNAL` | non-retryable error |

The gateway inspects the in-band status for every application outcome; only
transport-level gRPC errors bypass the response. The JSON-RPC layer above is
unchanged.

## Transport Security: mTLS

- **mTLS is required.** Both sides present certificates: the gateway
  authenticates the endorser, and the endorser authenticates the gateway. No
  plaintext or server-only-TLS fallback.
- The endorser trusts a **list of CA certificates of trusted organizations**,
  using the committer's exact TLS config format (`ca-cert-paths`):

```yaml
tls:
  mode: mtls
  cert-path: /node-tls/server.crt
  key-path: /node-tls/server.key
  ca-cert-paths:
    - /orderer-cas/orderer-org-1/msp/tlscacerts/tlsca.orderer-org-1-cert.pem
    - /orderer-cas/orderer-org-2/msp/tlscacerts/tlsca.orderer-org-2-cert.pem
```

- Pin a modern TLS floor (TLS 1.2+/1.3). Certificate rotation is a
  drain-and-restart of the listener, not a hot swap.

## Authentication and Authorization

- **Authentication:** the client identity is the mTLS peer certificate. The
  endorser accepts only connections whose cert chains to one of its trusted
  organization CAs (the `ca-cert-paths` above).
- **Authorization (v1):** mTLS is the authorization boundary - trust is the CA
  list and the endorser is not a public endpoint. Finer-grained, per-client
  authorization can be layered on later; a stricter committer authorization
  design is in progress in
  [fabric-x-rfcs#7](https://github.com/hyperledger/fabric-x-rfcs/pull/7).
- **Identity vs signing:** this is orthogonal to the MSP identity the endorser
  uses to *sign* endorsements - that behavior is unchanged. mTLS governs *who
  may call*; the endorsement signature governs *who endorsed*.

## Resilience

- **Deadlines:** the RPC honors the caller's context deadline; the current code
  already threads `context.Context` through every operation, so this is a
  transport-level pass-through.
- **Retries (v1):** retry lives entirely with the caller (gateway / mempool
  #50) and the endorser stays **stateless** - no server-side retry or idempotency
  keys. Only transport failures (`UNAVAILABLE`, `DEADLINE_EXCEEDED`) are
  retryable; application statuses (revert, tx-rejected, exec-failure) are not.
  Coordinate with #50 to avoid double-retrying the same transaction.
- **Connection management:** long-lived, pooled connections with keepalive
  between gateway and endorser rather than per-call dial cost.
- **Backpressure (v1):** bounding in-flight requests / max concurrent streams
  and surfacing overload as `RESOURCE_EXHAUSTED` is sufficient - no per-client
  rate limits or quotas yet.
