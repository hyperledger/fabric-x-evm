# Architecture Overview

## Table of Contents

- [Introduction](#introduction)
- [Key Design Decisions](#key-design-decisions)
  - [Full Ethereum Ecosystem Compatibility](#full-ethereum-ecosystem-compatibility)
  - [MVCC Validation for Transaction Consistency](#mvcc-validation-for-transaction-consistency)
  - [Dual Synchronization Architecture](#dual-synchronization-architecture)
  - [Gas as Metering, Not Payment](#gas-as-metering-not-payment)
  - [Fabric Ordering Service Instead of PoW/PoS](#fabric-ordering-service-instead-of-powpos)
- [Core Components](#core-components)
  - [Gateway](#gateway)
  - [Endorser](#endorser)
- [Data Flow](#data-flow)
  - [Transaction Execution Flow](#transaction-execution-flow)
- [State Management](#state-management)
  - [SnapshotDB: The EVM-Fabric Bridge](#snapshotdb-the-evm-fabric-bridge)
  - [Synchronization Architecture](#synchronization-architecture)
  - [Storage Layer](#storage-layer)
- [Performance, Throughput, and Concurrency Management](#performance-throughput-and-concurrency-management)
  - [Overview](#overview)
  - [Concurrency Challenges](#concurrency-challenges)
  - [Scaling Strategy: Phased Approach](#scaling-strategy-phased-approach)

## Introduction

Fabric-EVM makes Hyperledger Fabric-x and Fabric compatible with the Ethereum ecosystem by embedding an Ethereum Virtual Machine (EVM) that executes Solidity smart contracts within Fabric's permissioned environment. This integration combines the rich Ethereum tooling and contract ecosystem with Fabric's robust endorsement and consensus model.

The system enables developers to deploy and invoke existing Ethereum contracts without modification, using familiar tools like MetaMask, Web3.js, and Hardhat. Externally, the system exposes standard Ethereum JSON-RPC endpoints, making it indistinguishable from an Ethereum node. Internally, however, all transactions follow Fabric's endorsement, ordering, and MVCC validation flow, ensuring enterprise-grade governance and security.

**Key Benefits**:
- **Ethereum Compatibility**: Deploy Solidity contracts and use existing Ethereum tooling
- **Fabric Security**: Leverage Fabric's permissioned model, endorsement policies, and BFT consensus
- **High Performance**: Pre-order execution with parallel transaction processing where state doesn't conflict
- **Enterprise Governance**: Fabric's identity and access control layer on top of EVM execution

## Key Design Decisions

### Full Ethereum Ecosystem Compatibility

**API Compatibility**: The system exposes standard Ethereum JSON-RPC endpoints (eth_*, net_*, web3_*), making it fully compatible with existing Ethereum tooling. Clients like MetaMask, Web3.js, Ethers.js, and Hardhat can interact with Fabric-EVM without any modifications. This preserves the entire Ethereum developer experience while running on Fabric infrastructure.

**Bytecode Compatibility**: The system uses an unmodified `go-ethereum` EVM implementation, ensuring 100% compatibility with Solidity-compiled bytecode. All Ethereum hard forks are enabled from block 0, supporting the full range of EVM opcodes and precompiles. Contracts deployed on Ethereum can be deployed on Fabric-EVM without recompilation or modification.

### MVCC Validation for Transaction Consistency

Fabric's Multi-Version Concurrency Control (MVCC) ensures transaction consistency without requiring global locks. During endorsement, each transaction captures a read-write set with version information for all accessed keys. At commit time, Fabric validates that all read versions still match the current ledger state. If another transaction modified any read key, the transaction fails validation and is marked invalid.

This approach enables high concurrency: transactions touching disjoint state can execute in parallel and commit successfully. Only transactions with overlapping read-write dependencies face potential conflicts, which can be mitigated through retry logic or dependency tracking.

### Dual Synchronization Architecture

The system maintains two independent synchronizers to serve different purposes:

- **Endorser Synchronizers**: Keep endorsers' VersionedDB current with committed ledger state, ensuring accurate simulation and read-write set generation for new transactions
- **Gateway Synchronizer**: Indexes committed blocks into SQLite for efficient historical queries via Ethereum RPC endpoints

This separation allows endorsers to focus on execution accuracy while the gateway optimizes for query performance.

### Gas as Metering, Not Payment

Gas is tracked during EVM execution to bound computation and prevent runaway contracts, but **no fees are charged**. Gas serves purely as a metering mechanism to enforce per-transaction limits and protect the network from resource exhaustion. This aligns with Fabric's permissioned model where participants are known and trusted, eliminating the need for economic incentives found in public blockchains.

Configurable gas limits can be set at the channel or application level to control resource usage while maintaining deterministic execution.

### Fabric Ordering Service Instead of PoW/PoS

Unlike Ethereum's Proof-of-Work or Proof-of-Stake consensus, Fabric-EVM uses Fabric's **crash fault-tolerant (CFT) or Byzantine fault-tolerant (BFT) ordering service**. This provides:

- **Deterministic Finality**: Transactions are final once committed; no probabilistic confirmation delays
- **High Throughput**: Pre-order execution allows parallel endorsement before ordering
- **Permissioned Consensus**: Only authorized orderers participate in consensus
- **Fast Block Times**: Blocks are produced based on configuration (time or transaction count), not mining difficulty

The ordering service establishes a total order across all transactions, which are then validated and committed by peers according to their endorsement policies.

## Core Components

### Gateway

The Gateway (located at `gateway/`) serves as the primary entry point for clients, exposing an Ethereum-compatible JSON-RPC API.

- **API Layer** (`gateway/api/`): Implements Ethereum JSON-RPC endpoints (eth_*, net_*, web3_*)
- **Core** (`gateway/core/`): Orchestrates endorsement, transaction submission, and block processing
- **Storage** (`gateway/storage/`): SQLite-based persistence for blocks, transactions, and logs
- **Synchronizer**: Continuously fetches committed blocks from Fabric peers, parses them, and indexes EVM transactions

**Key Responsibilities**:
- Accept Ethereum transactions via JSON-RPC
- Coordinate endorsement from multiple endorsers
- Submit endorsed transactions to Fabric orderers
- Synchronize and index committed blockchain data
- Serve blockchain queries (blocks, transactions, logs, state)

### Endorser

The Endorser (located at `endorser/`) simulates EVM transaction execution and produces signed endorsements.

- **API** (`endorser/api/`): Processes Fabric proposals containing EVM transactions
- **EVMEngine** (`endorser/executor.go`): Manages EVM instantiation and execution
- **SnapshotDB** (`endorser/state.go`): Custom StateDB wrapper that captures read-write sets
- **Synchronizer**: Continuously fetches committed blocks from Fabric peers to maintain up-to-date ledger state

**Key Responsibilities**:
- Execute EVM transactions against versioned state snapshots
- Track state reads/writes as Fabric read-write sets
- Synchronize ledger state for accurate simulation
- Handle three proposal types:
  - `ProposalTypeEVMTx`: State-changing transactions
  - `ProposalTypeCall`: Read-only contract calls (eth_call)
  - `ProposalTypeState`: Direct state queries (balance, code, storage, nonce)
- Return signed proposal responses for endorsement

## Data Flow

### Transaction Execution Flow

```
Client (Metamask/Web3)
    ↓ [Ethereum Transaction via JSON-RPC]
Gateway API
    ↓ [Parse & Validate]
Gateway Core (EndorsementClient)
    ↓ [Create Fabric Proposal]
Endorser(s)
    ↓ [Simulate EVM Execution]
EVMEngine → SnapshotDB
    ↓ [Capture Read-Write Set]
Endorser API
    ↓ [Sign Proposal Response]
Gateway Core
    ↓ [Collect Endorsements]
Fabric Orderer
    ↓ [Order & Distribute]
Fabric Peers (Commit)
    ↓ [MVCC Validation & Write to Ledger]
    ↓
    ├─→ Endorser Synchronizer(s)
    │       ↓ [Fetch Committed Blocks]
    │       ↓ [Update Ledger State]
    │   Endorser VersionedDB
    │
    └─→ Gateway Synchronizer
            ↓ [Fetch Committed Blocks]
            ↓ [Parse & Extract EVM Transactions]
        Gateway Storage (SQLite)
```

As the diagram shows, the transaction execution flow includes the following key steps:

1. **Client Submission**: A client (e.g., MetaMask) signs an Ethereum transaction and submits it via JSON-RPC to the Gateway. The transaction contains the contract address, ABI-encoded function call, and sender's signature.

2. **Gateway Validation**: The Gateway API validates the Ethereum transaction signature, extracts the sender address, and verifies the transaction format.

3. **Proposal Creation**: The Gateway Core creates a Fabric SignedProposal containing:
   - Proposal type indicator (`ProposalTypeEVMTx`)
   - Serialized Ethereum transaction bytes
   - Fabric channel, namespace, and version metadata
   - Random Fabric nonce for uniqueness

4. **Endorsement Request**: The Gateway forwards the proposal to configured endorsers (typically multiple for fault tolerance and policy satisfaction).

5. **EVM Simulation**: Each endorser:
   - Deserializes the Ethereum transaction
   - Validates the Ethereum signature and extracts `msg.sender`
   - Checks that the transaction nonce matches the sender's ledger nonce (replay protection)
   - Creates an isolated EVM execution context with current block metadata
   - Instantiates a fresh SnapshotDB pointing to the current ledger state

6. **State Access During Execution**: As the EVM executes:
   - **Reads** (`SLOAD`): SnapshotDB fetches values from the Fabric ledger at the current block height, recording each read with its version for MVCC validation
   - **Writes** (`SSTORE`): SnapshotDB accumulates changes in memory without persisting, building the write set
   - **Read-Your-Writes**: SnapshotDB returns in-memory writes for subsequent reads within the same transaction, ensuring EVM semantics

7. **Read-Write Set Capture**: After execution completes, the endorser:
   - Extracts the complete read-write set from SnapshotDB
   - Captures any EVM logs emitted during execution
   - Increments the sender's nonce in the write set
   - Packages the result (return value, logs, rwset) into a Fabric ProposalResponse

8. **Endorsement Signing**: The endorser signs the ProposalResponse with its Fabric identity, creating a cryptographic endorsement that commits to the execution result.

9. **Endorsement Collection**: The Gateway collects responses from all endorsers, verifying that:
   - All endorsers produced identical read-write sets (deterministic execution)
   - The endorsement policy is satisfied (e.g., majority of endorsers)

10. **Transaction Assembly**: The Gateway packages the proposal and endorsements into a Fabric transaction envelope, signs it with its own identity, and submits it to the ordering service.

11. **Ordering**: The Fabric orderer sequences the transaction into a block along with other transactions, establishing a total order across the network.

12. **Commit and Validation**: Fabric peers receive the ordered block and:
    - Validate endorsement signatures and policy compliance
    - Perform MVCC validation: check that all read versions match current ledger state
    - If validation passes, apply the write set to the ledger
    - If validation fails (e.g., due to concurrent conflicting transaction), mark the transaction as invalid

13. **State Synchronization**: After commit:
    - **Endorser Synchronizers** fetch the new block and update their local VersionedDB, ensuring future simulations see the latest state
    - **Gateway Synchronizer** fetches the block, parses EVM transactions and logs, and indexes them in SQLite

14. **Client Response**: The Gateway detects the transaction's commit status and returns an Ethereum-style receipt to the client, including transaction hash, status, gas used, and emitted logs.

## State Management

State management in Fabric-EVM operates across multiple layers, each serving a distinct purpose in the transaction lifecycle. The architecture separates execution-time state access from persistent storage, enabling both accurate EVM simulation and efficient historical queries.

### SnapshotDB: The EVM-Fabric Bridge

SnapshotDB is a custom implementation of the go-ethereum `StateDB` interface that replaces Ethereum's native Merkle Patricia Trie with Fabric's versioned ledger. This component is critical for maintaining EVM semantics while capturing the read-write sets required for Fabric's MVCC validation.

**Key Characteristics**:

- **Versioned Reads**: When the EVM executes an `SLOAD` opcode, SnapshotDB fetches the value from Fabric's ledger at a specific block height, recording both the value and its version number. This version tracking is essential for MVCC validation at commit time.

- **In-Memory Writes**: `SSTORE` operations accumulate changes in memory without touching the ledger. This allows the EVM to execute speculatively while building a complete write set that will only be persisted after successful endorsement and ordering.

- **Read-Your-Writes Semantics**: Within a single transaction, subsequent reads return in-memory writes rather than ledger values. This ensures that EVM contracts behave correctly when they read state they've just modified, preserving standard Ethereum execution semantics.

- **Isolation**: Each transaction simulation creates a fresh SnapshotDB instance, ensuring complete isolation between concurrent endorsements. Multiple endorsers can simulate different transactions in parallel without interference.

**State Key Mapping**: EVM state is mapped to Fabric ledger keys using the following deterministic formats:
```
acc:<address>:bal      # Account balance
acc:<address>:nonce    # Account nonce
acc:<address>:code     # Contract code
str:<address>:<slot>   # Storage slot
```
where `<address>` is the hex-encoded Ethereum address and `<slot>` is the hex-encoded 32-byte storage slot index. This mapping ensures that all endorsers reconstruct identical read-write sets for the same transaction.

**Note**: Fabric transaction metadata (inputs and events) will be added to the read-write set using keys like `input/<fabric-tx-id>` and `event/<fabric-tx-id>` to ensure they are included in the transaction's state dependencies.

### Synchronization Architecture

The system maintains two independent synchronizers that serve complementary roles in keeping the network consistent:

**1. Endorser Synchronizer(s)**

Each endorser runs its own synchronizer to maintain an up-to-date view of the ledger state:

- **Block Fetching**: Receives newly committed blocks via an open gRPC stream from Fabric peers (or committer sidecars)
- **State Updates**: Applies committed write sets to the local VersionedDB, advancing the version numbers for modified keys
- **Simulation Accuracy**: Ensures that when endorsers simulate new transactions, they read from the latest committed state, producing accurate read-write sets
- **Version Tracking**: Maintains version metadata for all keys, enabling precise MVCC validation during commit

Without synchronization, endorsers would simulate against stale state, producing read-write sets with outdated version numbers that would fail MVCC validation.

**2. Gateway Synchronizer**

The gateway runs a single synchronizer focused on indexing rather than execution:

- **Block Parsing**: Fetches committed blocks and extracts EVM-specific data (transactions, receipts, logs)
- **Transaction Indexing**: Stores transaction metadata in SQLite, enabling fast lookups by hash, block number, or sender address
- **Log Indexing**: Indexes EVM logs by contract address and topics, supporting efficient `eth_getLogs` queries with complex filters
- **Historical Queries**: Provides the data layer for all Ethereum RPC endpoints that query historical blockchain state

The gateway's synchronizer is optimized for query performance rather than execution, using relational database indexes to support the diverse query patterns required by Ethereum clients.

### Storage Layer

The gateway maintains a local SQLite database that serves as the query backend for Ethereum RPC endpoints:

**Schema Design**:
- **Blocks Table**: Stores block metadata (number, hash, parent hash, timestamp) with indexes on both number and hash for fast lookups
- **Transactions Table**: Contains full transaction details including Ethereum hash, Fabric transaction ID, status, sender/recipient addresses, and raw transaction bytes
- **Logs Table**: Indexes EVM event logs with separate columns for up to 4 topics, enabling efficient filtering by event signature and indexed parameters

This separation of concerns—VersionedDB for execution, SQLite for queries—allows each component to optimize for its specific workload without compromise.

## Performance, Throughput, and Concurrency Management

### Overview

Fabric-EVM's performance characteristics differ fundamentally from traditional Ethereum due to Fabric's execute-order-validate (EOV) paradigm. While Ethereum follows an order-execute model where transactions are sequenced before execution, Fabric-EVM simulates transactions in parallel before ordering, enabling significantly higher throughput when transactions don't conflict. However, this approach introduces concurrency challenges that must be carefully managed to ensure both high performance and Ethereum client compatibility.

**Key Design Tenet**: The EVM Gateway must not break Ethereum clients. It must remain fully compatible with Ethereum's APIs, their behavior, and failure model. In particular, because "MVCC conflict" does not exist in Ethereum RPC semantics, surfacing MVCC failures to clients would be a breaking change. The gateway must therefore mask such conflicts through retry mechanisms and intelligent scheduling.

### Concurrency Challenges

#### MVCC Conflicts and Goodput

Hyperledger Fabric's Multi-Version Concurrency Control (MVCC) validates transactions at commit time by checking that all read versions still match the current ledger state. When multiple transactions access overlapping state:

1. **Parallel Simulation**: Transactions are simulated concurrently against the same state snapshot
2. **Sequential Ordering**: The ordering service establishes a total order
3. **MVCC Validation**: Only the first transaction commits successfully; subsequent transactions with overlapping reads fail validation

This creates a fundamental tension: **throughput** (total transactions processed) can be high, but **goodput** (successfully committed transactions) may be low under contention. A naive implementation that blindly retries failed transactions wastes resources and degrades performance.

**Example Scenario**: Two transactions both read and modify the same ERC-20 token balance. Both simulate successfully in parallel, but after ordering, only the first commits. The second fails MVCC validation and must be retried, doubling the work for that transaction.

### Scaling Strategy: Phased Approach

The system's concurrency management evolves through multiple phases, each building on the previous to increase throughput and decentralization.

#### Phase 1: Single Gateway (One Node, One Organization)

**Goal**: Throughput equals goodput through intelligent conflict avoidance.

**Milestones**:

1. **Correct Nonce Handling**: Implement ledger-tracked nonces with endorser-side validation to support retries and out-of-order submissions
2. **Basic Single-Node Functionality**:
   - In-memory mempool (transaction queue) absorbing transactions from `eth_sendRawTransaction`
   - Retry loop to mask MVCC conflicts from clients
   - Immediate return to clients with transaction hash
3. **High-Throughput Enablement**:
   - In-memory dependency manager tracking read-write dependencies between queued transactions
   - Endorse and submit only when previous dependent transactions commit
   - Optimal scheduling to minimize conflicts
4. **Tuning and Advanced Features**:
   - Retry for endorsements that fail because endorsers are temporarily out of sync
   - Handle client bursts (multiple sequential nonces submitted rapidly)
   - Handle same-client transaction sequencing
   - Support replacement transactions (same nonce, different payload or gas)
   - Backpressure and mempool queue management to prevent unbounded growth

**Benchmarking Metrics**:
- End-to-end throughput (transactions per second)
- Latency (time from submission to commit)
- Goodput rate (successful commits / total attempts)
- Synchronization lag (endorser/gateway ability to keep up with ledger evolution)

#### Phase 2: Gateway Replicas (One Organization, Multiple Nodes)

**Goal**: High throughput through horizontal scaling while maintaining consistency.

**Milestones**:

1. **Preparation**: Persist mempool to survive node crashes and enable cross-replica visibility
2. **Basic Multi-Node Deployment**:
   - Instantiate dependency manager as a shared service
   - All gateway replicas connect to the centralized dependency manager
   - Replicas collaborate and share load without duplicating work:
     - Coordinated endorsement collection
     - Coordinated transaction submission
   - Dependency manager acts as the mechanism for load distribution
3. **Tuning and Advanced Features**:
   - Speculative execution: emit batches of transactions rather than single dependency-free transactions
   - Streaming behavior for dependency manager to reduce latency

**Benchmarking**: Re-evaluate Phase 1 workload, expecting improvements in throughput and resilience.

#### Phase 3: Multi-Organization Deployment

**Goal**: Decentralization while maintaining high throughput.

**Milestones**:

1. **Basic CFT Deployment**:
   - Multiple dependency managers, one per organization
   - Distributed mempool (e.g., over etcd or directly with ARMA)
   - Basic dependency collection:
     - Nodes coordinate over etcd
     - Each transaction in mempool gets read-write set for dependency graph construction
     - All nodes consult local dependency manager and are aware of dependencies
     - One node per transaction is tasked to collect endorsements and submit
     - Dependency manager uses "blocks" to avoid non-determinism
     - Work assignment protocol to avoid duplication (e.g., which gateway is responsible for a transaction? What if it doesn't make progress?)
2. **Tuning and Advanced Features**:
   - Speculative execution across organizations
   - Handle `TxBatch` abstraction for deterministic sequencing of related transactions

**Benchmarking**: Re-evaluate same workload, expecting unchanged or only slightly lower throughput compared to Phase 2 (trading some performance for decentralization).

#### Phase 4: BFT Deployment

**Goal**: Byzantine fault tolerance for maximum decentralization.

**Approach**: To be determined, likely using ARMA (Atomic Replication with Majority Agreement) for consensus across organizations.

**Benchmarking**: Re-evaluate same workload, expecting unchanged or only slightly lower throughput compared to Phase 3 (trading some performance for decentralization).