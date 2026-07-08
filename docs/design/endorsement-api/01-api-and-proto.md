# Endorsement API Design — Proto and API

> Part 1 of the endorsement API design. It defines the gRPC service, the proto
> messages, what we serialize, and the RPC/streaming shape. Error handling and
> security are covered in part 2; the client, configuration, and testing in
> part 3. See [00-overview.md](00-overview.md) for framing and scope.

## Table of Contents

- [Scope](#scope)
- [Requirements](#requirements)
- [What Crosses the Boundary Today](#what-crosses-the-boundary-today)
- [Service Definition](#service-definition)
- [Messages](#messages)
- [Serialization Choices](#serialization-choices)
- [RPC Shape: Separate vs Single](#rpc-shape-separate-vs-single)
- [Unary vs Streaming](#unary-vs-streaming)
- [Alignment with fabric-x-committer](#alignment-with-fabric-x-committer)
- [Code Reuse](#code-reuse)
- [Proto Sketch](#proto-sketch)
- [Decisions and Alternatives](#decisions-and-alternatives)

## Scope

Design the gRPC contract between the gateway (client) and the endorser
(server). This part covers the service methods, the request/response messages,
and the serialization and streaming decisions. It does **not** cover error
semantics (part 2), mTLS/config (parts 2–3), or the rollout (part 4).

## Requirements

- Support the three operations the gateway performs today: **transaction
  endorsement**, **read-only calls**, and **state queries**.
- Preserve exact semantics: same request payloads, same
  `peer.ProposalResponse` result, same status codes.
- Proto schema that is easy to read, future-proof, and aligned with
  fabric-x-common / Fabric proto naming.
- Reuse existing Fabric/Ethereum encodings rather than re-inventing them.

## What Crosses the Boundary Today

The current in-process contract is the `Endorser` interface in
[`gateway/core/endorse.go`](../../../gateway/core/endorse.go):

| Method | Inputs | Output |
|--------|--------|--------|
| `ProcessEVMTransaction` | `endorsement.Invocation`, `*types.Transaction` | `*peer.ProposalResponse` |
| `ProcessCall` | `*ethereum.CallMsg`, `blockNumber *big.Int` | `*peer.ProposalResponse` |
| `ProcessStateQuery` | `common.StateQuery` | `*peer.ProposalResponse` |

Key observations that drive the proto design:

- The **response is already a Fabric protobuf** (`peer.ProposalResponse`), so it
  serializes over gRPC unchanged - no new response message needed.
- For transactions, the gateway builds the Fabric **`Invocation`** (TxID,
  nonce, creator, proposal, proposal hash) via `protoutil` and passes it
  alongside the marshaled Ethereum transaction. The `Invocation` wraps a
  `*peer.Proposal`, which is itself protobuf.
- `CallMsg` and `StateQuery` are plain Go structs that need explicit proto
  messages.

## Service Definition

A single service, `EvmEndorsement`, with one RPC per operation:

- `ProcessTransaction(TransactionRequest) → peer.ProposalResponse`
- `ProcessCall(CallRequest) → peer.ProposalResponse`
- `ProcessStateQuery(StateQueryRequest) → peer.ProposalResponse`

The service is **specialized for fabric-x-evm**, not the generic Fabric
`ProcessProposal` endorsement API, per the design goal.

## Messages

**Response (all three):** reuse `peer.ProposalResponse` directly.

**`TransactionRequest`** - carries what the endorser needs to validate,
execute, and endorse:

- the Fabric proposal (the serialized `peer.Proposal` from the `Invocation`),
- the marshaled Ethereum transaction (`types.Transaction.MarshalBinary`).

> Open item: the exact `Invocation` fields the builder needs on the server side
> (beyond the proposal itself) must be confirmed against
> `endorsement.Builder.Endorse`. If the builder can reconstruct everything from
> the proposal, the request stays minimal; otherwise we add the missing fields
> explicitly. Resolved before the proto lands.

**`CallRequest`** - an `eth_call` message plus block selector:

- from, to, gas, gas price, value, data (mirrors `ethereum.CallMsg`),
- block number (nullable → latest).

**`StateQueryRequest`** - mirrors `common.StateQuery`:

- query type (balance / code / storage / nonce),
- account address, storage key (for storage queries), block number.

## Serialization Choices

- **Ethereum transaction:** send as opaque bytes (`MarshalBinary`). The
  endorser already re-derives the sender and re-executes from these bytes, so
  re-encoding into proto fields would be redundant and risk fidelity loss.
- **Fabric proposal:** send the serialized `peer.Proposal`/`SignedProposal`
  bytes. This keeps the proposal hash and any signatures byte-exact, which
  matters for endorsement validity.
- **Call / state query:** structured proto fields (not opaque bytes), because
  these are small, typed, and benefit from a readable schema.
- **Addresses / hashes / big integers:** fixed-width `bytes` (20-byte address,
  32-byte hash, big-endian integer bytes), matching how the code already moves
  them.

## RPC Shape: Separate vs Single

**Chosen: three separate RPCs**, one per operation.

- *Pro:* strongly typed, self-documenting, each request carries only relevant
  fields; maps 1:1 to the existing interface and to distinct server handlers.
- *Con:* three methods instead of one.

*Alternative - single `ProcessProposal`-style RPC* with a `oneof` payload or a
type tag (closer to Fabric's generic endorser and to the committer's
`ProcessProposal`). Rejected for v1: it pushes type-dispatch into the message
body and weakens the schema's readability, which conflicts with the
"easy to understand" goal. Revisit only if a single stream endpoint is needed
for throughput (see next section).

## Unary vs Streaming

**Chosen: unary RPCs for v1**, with the proto laid out so a streaming variant
can be added without breaking changes.

- Unary is simplest, matches the current one-shot call semantics, and is easy
  to reason about for correctness.
- The orderer keeps a connection open for throughput; the same could help the
  gateway's mempool worker (#50) when submitting many transactions. If
  profiling shows per-call overhead matters, add a
  `ProcessTransactionStream(stream TransactionRequest) → stream ProposalResponse`
  alongside the unary method.

> Decision deferred to data: start unary, measure, add streaming only if the
> mempool throughput work needs it. Flagged for maintainer input.

## Alignment with fabric-x-committer

The lightweight reference is `fabric-x-samples/custom-endorser`. We align on:

- reusing Fabric proto types (`peer.Proposal`, `peer.ProposalResponse`) rather
  than defining parallel messages,
- server scaffolding and connection handling patterns from the committer's
  endorser,
- naming that matches fabric-x-common protos where an equivalent concept
  exists.

Where fabric-x-evm needs EVM-specific fields (call args, state-query types),
we add our own messages rather than bending a generic message to fit.

## Code Reuse

Three options for the server-side gRPC and endorsement plumbing:

1. **Duplicate** the needed pieces from fabric-x-committer into fabric-x-evm.
   *Pro:* no new dependency, full control. *Con:* drift over time.
2. **Depend** on fabric-x-committer directly. *Pro:* single source of truth.
   *Con:* couples fabric-x-evm to committer internals not meant as public API.
3. **Upstream** the shared pieces into fabric-x-common, then depend on that.
   *Pro:* clean shared home. *Con:* needs @senthil / maintainer buy-in and is
   the slowest path.

> Recommendation pending maintainer input: prefer (3) for anything genuinely
> shared, fall back to (1) for small EVM-specific glue. This is a @senthil
> question and is explicitly flagged for the design discussion.

## Proto Sketch

Illustrative, not final - field numbers and exact `Invocation` fields settle
when the open items above are resolved.

```proto
syntax = "proto3";

package fabricxevm.endorsement.v1;

import "peer/proposal_response.proto"; // peer.ProposalResponse

service EvmEndorsement {
  rpc ProcessTransaction(TransactionRequest) returns (protos.ProposalResponse);
  rpc ProcessCall(CallRequest)               returns (protos.ProposalResponse);
  rpc ProcessStateQuery(StateQueryRequest)   returns (protos.ProposalResponse);
}

message TransactionRequest {
  bytes proposal     = 1; // serialized peer.Proposal from the Invocation
  bytes ethereum_tx  = 2; // types.Transaction MarshalBinary
}

message CallRequest {
  bytes  from      = 1; // 20-byte address (optional)
  bytes  to        = 2; // 20-byte address (nil for contract creation)
  uint64 gas       = 3;
  bytes  gas_price = 4; // big-endian integer bytes
  bytes  value     = 5; // big-endian integer bytes
  bytes  data      = 6;
  optional uint64 block_number = 7; // absent = latest
}

enum StateQueryType {
  STATE_QUERY_TYPE_UNSPECIFIED = 0;
  BALANCE                      = 1;
  CODE                         = 2;
  STORAGE                      = 3;
  NONCE                        = 4;
}

message StateQueryRequest {
  StateQueryType type    = 1;
  bytes account          = 2; // 20-byte address
  bytes key              = 3; // 32-byte storage key (STORAGE only)
  optional uint64 block_number = 4; // absent = latest
}
```


