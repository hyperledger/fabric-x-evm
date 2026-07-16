# Endorsement API Design - Proto and API

> First part of the endorsement API design (#22). It defines the gRPC service,
> the proto messages, what we serialize, and the RPC/streaming shape. See
> [00-overview.md](00-overview.md) for framing and scope. Follow-up parts
> (errors and security; client, configuration, and testing; implementation
> plan) will be added once this API part is agreed.

## Table of Contents

- [Scope](#scope)
- [Requirements](#requirements)
- [The Functions Behind the API](#the-functions-behind-the-api)
- [Service Definition](#service-definition)
- [Messages](#messages)
- [What Execute's Endorsement Covers](#what-executes-endorsement-covers)
- [Errors](#errors)
- [Serialization Choices](#serialization-choices)
- [Unary vs Streaming](#unary-vs-streaming)
- [Alignment](#alignment)
- [Code Reuse](#code-reuse)
- [Proto Sketch](#proto-sketch)

## Scope

Design the gRPC contract between the gateway (client) and the endorser
(server). This part covers the service methods, the request/response messages,
and the serialization and streaming decisions. It does **not** cover error
semantics in depth, mTLS/config, or the rollout - those follow in later parts.

The API is a **clean, fabric-x-evm-specific contract**. It is not bound to the
generic Fabric endorsement shapes: requests do not carry a `peer.Proposal`,
and responses are not `peer.ProposalResponse`. The Fabric transaction envelope
is assembled by the gateway; the wire API only moves what the endorser
actually needs and produces.

## Requirements

- Expose every operation the gateway needs today: transaction execution,
  read-only calls, and the four state reads (balance, storage, code, nonce).
- Typed, self-documenting schema - one RPC per function, no dispatch enums.
- Only `Execute` produces an endorsement; the read-only RPCs return plain
  results with no signing or proposal machinery.
- Easy to read, future-proof, aligned with fabric-x-common naming.

## The Functions Behind the API

The engine interface the endorser already implements
([`endorser/endorser.go`](../../../endorser/endorser.go),
`EVMEngineInterface`) is the natural shape of the API:

```go
Execute(ctx, tx *types.Transaction) (endorsement.ExecutionResult, error)
Call(msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
BalanceAt(ctx, account common.Address, blockNumber *big.Int) (*big.Int, error)
StorageAt(ctx, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error)
CodeAt(ctx, account common.Address, blockNumber *big.Int) ([]byte, error)
NonceAt(ctx, account common.Address, blockNumber *big.Int) (uint64, error)
```

Note: in the current code all six methods **do** return an error alongside the
result; failures travel as gRPC status errors (see [Errors](#errors)).

`ExecutionResult` (fabric-x-sdk) carries the read-write set, an optional
event, and a status/message/payload triple - this is what `Execute` must get
back to the gateway, signed.

## Service Definition

One RPC per function, mirroring the engine interface for type safety:

- `Execute(ExecuteRequest) → ExecuteResponse` - executes an Ethereum
  transaction and returns the **signed** execution result.
- `Call(CallRequest) → CallResponse` - read-only `eth_call`.
- `BalanceAt(BalanceRequest) → BalanceResponse`
- `StorageAt(StorageRequest) → StorageResponse`
- `CodeAt(CodeRequest) → CodeResponse`
- `NonceAt(NonceRequest) → NonceResponse`

Splitting the reads into their own RPCs (instead of a `StateQueryRequest`
with a type enum) keeps every request and response fully typed: a caller
cannot send a storage key on a balance query, and each response says exactly
what it is.

## Messages

**`ExecuteRequest`** - the marshaled Ethereum transaction
(`types.Transaction.MarshalBinary`). The signed raw transaction is all the
endorser needs: it re-derives the sender from the signature and executes
against its own state.

**`ExecuteResponse`** - the fields of `endorsement.ExecutionResult` (read-write
set, event, status/message/payload) plus the endorser's identity and signature
over the result. See
[What Execute's Endorsement Covers](#what-executes-endorsement-covers).

**`CallRequest`/`CallResponse`** - the `ethereum.CallMsg` fields (from, to,
gas, gas price, value, data) plus a block selector; the response is the return
data.

**State reads** - each request is just the account (plus the storage key for
`StorageAt`) and a block selector; each response is the single typed value
(balance bytes, storage word, code bytes, nonce).

## What Execute's Endorsement Covers

`Execute` is the only RPC whose result feeds a Fabric transaction, so it is the
only one that needs an endorsement signature. Following maintainer review, what
is signed is settled per platform, keeping the wire as small as possible:

- **Fabric-X - no proposal on the wire.** Fabric-X does not require the proposal
  bytes for a valid transaction, so the endorser signs the execution result
  directly and the gateway packages it without a proposal. Fewer bytes, less
  complexity; this is expected to be a small change in the SDK
  `fabricx.TxPackager`.
- **Classic Fabric - proposal hash only.** For Fabric the proposal is required
  as part of the submitted transaction and the payload must include the
  `ProposalHash`. The gateway builds the proposal and sends only its **hash**
  with `ExecuteRequest`; the endorser signs against that hash. The full proposal
  never crosses the wire.

> Decision (with @arner): leave the proposal out entirely for Fabric-X; send only
> the proposal hash for classic Fabric. To be confirmed in code that
> `fabricx.TxPackager` accepts a transaction without the proposal bytes.

## Errors

With `peer.ProposalResponse` gone, results no longer carry Fabric status codes
(200/201/500). Instead:

- **Success** - typed response message.
- **EVM revert / execution failure on `Execute`** - part of the execution
  result itself (status/message/payload fields), since a reverted transaction
  is still endorsed and committed with `status=0`.
- **Revert on `Call`** - gRPC error status with structured revert details
  (reason + data), so the gateway can map to JSON-RPC `-32000`.
- **Everything else** (invalid argument, unavailable, timeout, internal) -
  standard gRPC status codes.

The full error taxonomy and mapping table follows in the errors-and-security
part once this API shape is agreed.

## Serialization Choices

- **Ethereum transaction:** opaque bytes (`MarshalBinary`) - re-encoding into
  proto fields would be redundant and risk fidelity loss.
- **Addresses / hashes / big integers:** fixed-width `bytes` (20-byte address,
  32-byte word, big-endian integer bytes), matching how the code moves them.
- **Block selector:** `optional uint64`; absent means latest.
- **Read-write set:** the SDK's existing serialization of
  `blocks.ReadWriteSet`, kept byte-exact for signing.

## Unary vs Streaming

**Unary RPCs for v1**, laid out so a streaming variant can be added without
breaking changes. Unary matches the current one-shot call semantics and is easy
to reason about. It is also not as serial as it looks: gRPC runs over HTTP/2,
which multiplexes many concurrent requests over one pooled connection, so at
high throughput the limiting factor is per-call overhead rather than unary vs
streaming as such.

**Throughput target: 15-20k tps.** Rather than assume, we benchmark the unary
path against this target as part of the mempool throughput work (#50). If
per-call overhead proves to be the bottleneck, an `ExecuteStream` can be added
alongside the unary method (the way the orderer keeps a connection open) - an
additive, non-breaking change.

## Alignment

The lightweight reference is `fabric-x-samples/custom-endorser`. We align on the
server scaffolding and connection-handling patterns it demonstrates (now via
`serve` in fabric-x-common - see Code Reuse), and on fabric-x-common naming where
an equivalent concept exists. EVM-specific messages (call args, state reads) are
our own rather than bent generic messages.

## Code Reuse

**We do not depend on fabric-x-committer.** The server needs standard gRPC
bootstrapping - listen, TLS, graceful shutdown, health check, and config - which
the committer's `utils/serve` package provides and the
`fabric-x-samples/custom-endorser` sample is built on. Identifying that as the
one reusable piece led to committer
[#675](https://github.com/hyperledger/fabric-x-committer/issues/675): @arner is
**publishing `serve` (+ `connection`, `retry`) into fabric-x-common**, agreed by
the committer maintainers.

fabric-x-evm already depends on fabric-x-common, so we take the bootstrap **from
fabric-x-common** - a single source of truth, the right dependency direction
(common ← evm), and no fabric-x-committer dependency. Everything else
(reflection, status codes) is stock `google.golang.org/grpc`.

## Proto Sketch

Illustrative, not final. Per the decision above, `ExecuteRequest` carries no
proposal for Fabric-X (and only a proposal hash for classic Fabric), and
`ExecuteResponse` carries the signed execution result.

```proto
syntax = "proto3";

package fabricxevm.endorsement.v1;

service EvmEndorsement {
  rpc Execute(ExecuteRequest)     returns (ExecuteResponse);
  rpc Call(CallRequest)           returns (CallResponse);
  rpc BalanceAt(BalanceRequest)   returns (BalanceResponse);
  rpc StorageAt(StorageRequest)   returns (StorageResponse);
  rpc CodeAt(CodeRequest)         returns (CodeResponse);
  rpc NonceAt(NonceRequest)       returns (NonceResponse);
}

message ExecuteRequest {
  bytes ethereum_tx = 1; // types.Transaction MarshalBinary
}

message ExecuteResponse {
  bytes  read_write_set = 1; // serialized blocks.ReadWriteSet
  bytes  event          = 2;
  int32  status         = 3;
  string message        = 4;
  bytes  payload        = 5;
  bytes  endorser_id    = 6; // serialized identity of the signer
  bytes  signature      = 7; // over the result (exact coverage: open item)
}

message CallRequest {
  bytes  from      = 1; // 20-byte address (optional)
  bytes  to        = 2; // 20-byte address (empty for contract creation)
  uint64 gas       = 3;
  bytes  gas_price = 4; // big-endian integer bytes
  bytes  value     = 5; // big-endian integer bytes
  bytes  data      = 6;
  optional uint64 block_number = 7; // absent = latest
}

message CallResponse {
  bytes return_data = 1;
}

message BalanceRequest {
  bytes account = 1; // 20-byte address
  optional uint64 block_number = 2;
}

message BalanceResponse {
  bytes balance = 1; // big-endian integer bytes
}

message StorageRequest {
  bytes account = 1; // 20-byte address
  bytes key     = 2; // 32-byte storage key
  optional uint64 block_number = 3;
}

message StorageResponse {
  bytes value = 1; // 32-byte storage word
}

message CodeRequest {
  bytes account = 1; // 20-byte address
  optional uint64 block_number = 2;
}

message CodeResponse {
  bytes code = 1;
}

message NonceRequest {
  bytes account = 1; // 20-byte address
  optional uint64 block_number = 2;
}

message NonceResponse {
  uint64 nonce = 1;
}
```
