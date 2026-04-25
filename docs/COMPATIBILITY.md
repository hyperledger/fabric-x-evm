# fabric-evm Compatibility Reference

This document tracks how fabric-evm differs from a standard Ethereum node. It is a living
document for three audiences: **external technical users** (via wallets, ethers.js, Foundry, block
explorers), **smart-contract developers** who need to understand what EVM guarantees hold, and
**internal contributors** who make implementation decisions and track open gaps.

---

## Architecture: execute-order-commit

Standard Ethereum uses **order-execute-commit**: transactions are broadcast to a mempool, ordered
by miners/validators, then every node executes them in that fixed order and commits.

This system uses **execute-order-commit** (Fabric's model): a client submits a transaction to one
or more endorser nodes, each of which simulates (executes) the transaction against its current
state and produces a signed read-write set. The gateway then packages those endorsements into a
Fabric envelope and sends it to the orderer. The orderer sequences the envelope and delivers it
to all peers, which validate the read-write sets and commit or reject the transaction.

Key consequence: if another transaction modifies the same state keys between the endorsement
simulation and the commit (an **MVCC conflict**), Fabric rejects the transaction at commit time.
The transaction is still included in the committed block with a non-zero validation code; the
extractor stores it with `ethStatus=0`. A receipt with `status=0` is therefore available — the
transaction does appear on-chain, but with a failure status and no state changes applied.

---

## Design choices / priorities

For now, we have chosen to defer implementation of gas metering and anything related to merkle
patricia trees (state root, log bloom, etc). The priority is equivalence in contract execution 
and API compatibility.

> [!NOTE]
> This document describes the **current state** and not the **planned state**. It is subject
> to change as we develop the components. There are some quick wins with low architectural impact
> that can be developed right away, and there are bigger decisions with trade-offs. These decisions
> will be documented in detail.

## Testing strategy

We have some integration tests to exercise specific expected behaviour, and are planning to move
to official EVM / ethereum compatibility test suites. This document will be enhanced with more 
specific test results and compatibility matrices as we go.

---

## Supported JSON-RPC methods

| Method                                                        | Status | Notes                                           |
| ------------------------------------------------------------- | ------ | ----------------------------------------------- |
| `eth_chainId`                                                 | ✅      |                                                 |
| `eth_blockNumber`                                             | ✅      |                                                 |
| `eth_getBlockByNumber`                                        | ✅      | several fields hardcoded — see below            |
| `eth_getBlockByHash`                                          | ✅      | several fields hardcoded — see below            |
| `eth_getBlockTransactionCountByHash`                          | ✅      |                                                 |
| `eth_getBlockTransactionCountByNumber`                        | ✅      |                                                 |
| `eth_getBalance`                                              | ✅      | routes to endorser; see state query caveats     |
| `eth_getCode`                                                 | ✅      | routes to endorser                              |
| `eth_getStorageAt`                                            | ✅      | routes to endorser                              |
| `eth_getTransactionCount`                                     | ✅      | routes to endorser; see nonce caveats           |
| `eth_sendRawTransaction`                                      | ✅      | execution is synchronous — see finality section |
| `eth_call`                                                    | ⚠️      | works but has caveats — see eth_call section    |
| `eth_getTransactionByHash`                                    | ⚠️      | never pending — see finality section            |
| `eth_getTransactionByBlockHashAndIndex`                       | ✅      |                                                 |
| `eth_getTransactionByBlockNumberAndIndex`                     | ✅      |                                                 |
| `eth_getTransactionReceipt`                                   | ✅      | see receipt section                             |
| `eth_getLogs`                                                 | ✅      |                                                 |
| `eth_estimateGas`                                             | 🔧      | always returns `0`                              |
| `eth_gasPrice`                                                | 🔧      | always returns `1 wei`                          |
| `eth_maxPriorityFeePerGas`                                    | 🔧      | always returns `1 wei`                          |
| `eth_feeHistory`                                              | 🔧      | returns all-zero arrays                         |
| `net_version`                                                 | ✅      | returns chain ID as network ID                  |
| `net_listening`                                               | 🔧      | always returns `true`                           |
| `web3_clientVersion`                                          | 🔧      | returns `"fabric-evm/0.1.0"`                    |
| `eth_subscribe` / `eth_unsubscribe`                           | ❌      | no WebSocket support                            |
| `eth_newFilter` / filter APIs                                 | ❌      | no server-side filter state; use `eth_getLogs`  |
| `eth_sendTransaction`                                         | ❌      | server-side signing not supported               |
| `eth_pendingTransactions`                                     | ❌      | no mempool                                      |
| `eth_getUncleBy*` / uncle count                               | ❌      | no uncle concept in Fabric                      |
| `debug_*` / `admin_*` / `personal_*` / `miner_*` / `txpool_*` | ❌      | not implemented                                 |

Legend: ✅ works as expected · ⚠️ partially works · 🔧 stubbed/mocked · ❌ not implemented

---

## Transaction lifecycle and finality

Standard Ethereum: `eth_sendRawTransaction` broadcasts the transaction and returns a hash
immediately. Execution happens at mining time; the receipt appears when the block is mined.

Here, because of execute-order-commit:

- **EVM reverts are returned immediately as errors**: If the EVM reverts during endorsement,
  `eth_sendRawTransaction` returns a JSON-RPC error (`-32000`) and the transaction is never
  submitted to the orderer. Standard Ethereum clients expect to receive a tx hash regardless of
  revert outcome and check `status=0` in the receipt later. Wallets and libraries that treat any
  error from `eth_sendRawTransaction` as a network error (rather than an execution failure) will
  behave incorrectly.

- **`eth_sendRawTransaction` blocks until the wait period elapses**: Unlike standard Ethereum
  (where the call returns near-instantly with a mempool hash), here `SendTransaction` is fully
  synchronous: it runs EVM execution, waits for the orderer to accept the envelope, then sleeps
  a fixed `waitAfterSubmit` duration (currently 2200 ms, a temporary FIXME until a proper finality
  listener is implemented) before returning. The tx hash is therefore returned **after** that wait,
  by which time the block has *usually* committed. Clients should expect `eth_sendRawTransaction`
  to take several seconds rather than milliseconds.

- **MVCC conflict — committed with status=0**: A transaction can be rejected at commit time if
  another transaction modified the same state keys between endorsement and commit. The transaction
  IS included in the committed block with a non-zero Fabric validation code, stored with
  `ethStatus=0`. A receipt is available (status=0), but none of its EVM state changes are applied.

- **No pending transactions**: `eth_getTransactionByHash` always returns `isPending=false`. A
  transaction either exists in a committed block or returns `null`. There is no intermediate
  "pending" state visible to clients.

- **EVM reverts leave no on-chain trace**: If the EVM reverts during endorsement, the transaction
  is never submitted to the orderer. `eth_getTransactionReceipt` returns `null` — unlike standard
  Ethereum, where reverted transactions are committed with `status=0`. Note that MVCC-rejected
  transactions are the exception: they are committed to the block and do have a `status=0` receipt
  (see above).

---

## Nonce management

**No nonce validation**: The sender's nonce is not verified before execution. Any transaction
with any nonce value is accepted and executed. Replay protection is not implemented yet.

**No increment on failed transaction**: The nonce is incremented on the sender's account only 
after a *successful* execution — reverts or MVCC conflicted transactions do not increment the nonce.

**No pending nonce**: `eth_getTransactionCount` returns the **committed** nonce for an address —
it routes to the endorser's state DB, which reflects only committed blocks. Because there is no
mempool, two transactions sent in rapid succession from the same address will:

1. Both read the same committed nonce `N`.
2. Both generate a read-write set that writes `nonce = N+1`.
3. Both be endorsed successfully.
4. At commit time, one will hit an MVCC conflict on the nonce key and be committed with `status=0`.

Wallets that track a "pending nonce" (MetaMask, ethers.js `Signer.sendTransaction`) by consulting
`eth_getTransactionCount` with the `"pending"` tag will see the committed nonce, not the pending
one. The `pending` tag is treated as `latest` (see block tags note in the Block representation
section).

---

## EVM execution differences

**Fork-level opcodes and precompiles**: All EVM opcodes through Osaka are active. This includes
`MCOPY` (EIP-5656), `TLOAD`/`TSTORE` (EIP-1153, transient storage is fully implemented with
snapshot/journal support), `BLOBHASH`/`BLOBBASEFEE` (EIP-4844), and BLS12-381 precompiles
(EIP-2537). Blob transactions (type 3) are accepted and executed; KZG proof validation is
skipped (no DA layer). EIP-7702 set-code transactions (type 4) are accepted and processed —
EOA code is set via the standard `SetCode` path.

**Snapshot / revert is implemented**: `Snapshot()` and `RevertToSnapshot()` use a journal-based
mechanism that correctly rolls back all in-memory state changes (balances, nonces, code, storage,
transient storage, logs, self-destruct flags). Sub-call reverts isolate state as expected.

**`SELFDESTRUCT` is a no-op** (EIP-6780, active from Osaka): With EIP-6780 active, the EVM always
calls `SelfDestruct6780`. Our implementation returns `(0, false)` — it does nothing: no balance
transfer to the beneficiary, no code removal, no storage clearing, and `HasSelfDestructed` always
returns `false`. Previously (Shanghai), `SelfDestruct()` at least zeroed the account balance. That
no longer happens either. Contracts that depend on SELFDESTRUCT for cleanup, ETH recovery, or
reentrancy detection via `HasSelfDestructed` will not work correctly.

Implementation note: clearing storage slots requires enumerating all keys for the address
(non-trivial) and may produce an impractically large RWSet for contracts with many storage entries.

**Native ETH balances not funded**: balances are implemented but unused. Accounts have zero ETH 
balance by default. Value transfers inside the EVM (`CALL` with value, `SELFDESTRUCT` beneficiary, 
etc.) will fail or produce wrong results for accounts that were never explicitly funded.

**`GetStateAndCommittedState` always returns zero as the committed value**:
The second return value (the pre-transaction snapshot of a slot) is always `common.Hash{}`. This
is used by geth for EIP-2929/3529 SSTORE gas calculations. Since gas is not metered here, the
practical impact is gas costs only — but the committed-state value is unreliable if ever read by
tracing or analytics tools.

**Access list not warmed — all accesses are cold**: `statedb.Prepare()` is never called, so the access
list is never populated. Every address and storage slot is treated as cold on each transaction. In geth,
the sender, recipient, precompiles, and `tx.accessList` entries are pre-warmed. Practical impact is 
minimal because gas is not metered.

---

## Block context and chain environment

The following values are hardcoded or synthetic. Contracts should not rely on them matching real
network values.

| Opcode / field              | This system                                       | Ethereum                    |
| --------------------------- | ------------------------------------------------- | --------------------------- |
| `BLOCKHASH(n)`              | always `0x000…`                                   | hash of block `n`           |
| `COINBASE`                  | `0x000…`                                          | block proposer address      |
| `DIFFICULTY` / `PREVRANDAO` | `0x000…` (stub — do not rely on for randomness)   | current random / difficulty |
| `BASEFEE`                   | `0`                                               | actual EIP-1559 base fee    |
| `BLOBBASEFEE`               | ~1 wei (calculated from `ExcessBlobGas = 0`)      | actual EIP-4844 blob fee    |
| `TIMESTAMP`                 | `1_000_000` (when blockInfo not supplied)         | actual Unix timestamp       |
| `NUMBER`                    | Fabric block number (when blockInfo not supplied) | Ethereum block number       |

**Current gateway behaviour**: `gateway/core/api.go` always passes `nil` as `blockInfo` when
calling `ExecuteEthTx`. Therefore the fallback values — `TIMESTAMP = 1_000_000` and `NUMBER =
current DB version` — apply to **all** transactions and calls submitted through the gateway.
The `blockInfo` parameter exists to support richer block context in future or in custom/test
harness integrations; integration tests that pass an explicit `blockInfo` do get real values.

**`eth_call` with a historical block number**: When `eth_call` is issued with a historical block
number, the state DB is correctly snapshotted at that height. However, the EVM block context
(`NUMBER`, `TIMESTAMP` opcodes) uses the fallback values (`NUMBER = current latest block`,
`TIMESTAMP = 1_000_000`) — not the values corresponding to the historical block. Contracts that
read `block.number` or `block.timestamp` inside a view function will see inconsistent values.

---

## Gas and fees

Gas mechanics are intentionally not implemented.

| Aspect                      | Fabric.                      | Ethereum                            |
| --------------------------- | ---------------------------- | ----------------------------------- |
| `GASPRICE` opcode           | `0`                          | actual tx gas price                 |
| Sender balance check        | not performed                | must cover `gas × gasPrice + value` |
| Intrinsic gas deduction     | not deducted                 | ~21 000 deducted before execution   |
| Gas refund counter          | always `0`                   | tracks SSTORE/SELFDESTRUCT refunds  |
| Default gas per call/deploy | `5 000 000` if not specified | whatever the tx sets                |
| Block gas limit             | `10 000 000`                 | network-set limit                   |
| Access list warmup          | not done                     | sender, to, precompiles pre-warmed  |

**JSON-RPC fee stubs**: `eth_estimateGas` always returns `0`; `eth_gasPrice` and
`eth_maxPriorityFeePerGas` always return `1 wei`; `eth_feeHistory` returns all-zero arrays. Clients
that use these values to set gas prices on future transactions will set `gasPrice = 1` and
`gas = 0`, which is harmless here since gas is not enforced, but may confuse tooling.

---

## Block representation

| Field                                    | Value                              | Notes                                             |
| ---------------------------------------- | ---------------------------------- | ------------------------------------------------- |
| `number`                                 | Fabric block number                |                                                   |
| `hash`                                   | Fabric block header hash           |                                                   |
| `parentHash`                             | Fabric previous block hash         |                                                   |
| `timestamp`                              | Node wall-clock time at parse time | **Not** the Fabric block creation time — see note |
| `transactions`                           | Full objects or hashes             | real data                                         |
| `logsBloom`                              | 512 ASCII zeros, no `0x` prefix    | format bug — see note                             |
| `transactionsRoot`                       | empty hash                         | no Merkle Patricia Trie (MPT)                     |
| `stateRoot`                              | MPT hash                           | Not fully compatible yet                          |
| `receiptsRoot`                           | empty hash                         | no MPT                                            |
| `miner` / `coinbase`                     | `0x000…`                           |                                                   |
| `gasLimit` / `gasUsed` / `baseFeePerGas` | `0`                                | gas not metered                                   |
| `difficulty` / `totalDifficulty`         | `0`                                |                                                   |
| `uncles`                                 | `[]`                               | no uncle concept                                  |
| `size`                                   | `0`                                |                                                   |
| `extraData`                              | `""`                               |                                                   |

**Block timestamp**: The `timestamp` field is set to the node's wall-clock time when the block is
received and parsed, as there is no authoritative block creation timestamp. Values can diverge
across nodes and across restarts. Do not use block timestamps for precision time ordering.

**Block number tags**: `blockNumberToUint64` returns `0` for any non-positive block number tag.
All of `latest`, `pending`, `earliest`, `safe`, and `finalized` resolve to `0`, which means
"latest committed block". Consequences:
- Clients that use `earliest` to bootstrap historical event scans will get the current head
  instead of the genesis block.
- Clients that rely on `safe` or `finalized` tags to detect confirmed state (common in
  ethers.js v6 and viem) will silently receive the latest block instead. In practice, every
  committed Fabric block is final, so `finalized == latest` is semantically correct — but the
  silencing of the distinction may surprise tooling.

**The first EVM block is not block 0**: Fabric channel genesis and any configuration transactions
precede the first EVM transaction, so the lowest block number that contains EVM data is 2 or
higher. Additionally, `blockNumberToUint64` maps the literal `0x0` argument to "latest", so there
is no way to address the genesis block by number — querying `eth_getBlockByNumber("0x0")` always
returns the current head. Event indexers that start scanning from block 0 will start at the
current block, not genesis, and miss all history.

**Block production requires transactions**: Fabric only creates new blocks when there are
transactions to order. If no transactions are submitted, the block number does not advance.
Clients that poll `eth_blockNumber` waiting for the next block, or that use `eth_getLogs` with a
moving `toBlock`, will wait indefinitely if no transaction activity is happening. This affects:
- ethers.js `provider.waitForBlock(n)` and block listener callbacks
- viem `watchBlocks`
- Any client waiting for at least `n` confirmations.

---

## Transaction representation

Fields derived from the signed RLP transaction are fully accurate: `nonce`, `gas`, `gasPrice`,
`to`, `value`, `data`, `v`/`r`/`s`, `hash`, `type`, `chainId`. Block context fields
(`blockHash`, `blockNumber`, `transactionIndex`) are populated from stored data.

For contract deployment transactions (`to` = null), `contractAddress` is correctly computed as
`crypto.CreateAddress(from, nonce)`.

The `from` field is populated in all transaction responses from the stored `FromAddress` in
`domain.Transaction`.

---

## Receipt representation

| Field                                                             | Value                          |
| ----------------------------------------------------------------- | ------------------------------ |
| `status`                                                          | `1` (success) or `0` (failure) |
| `transactionHash`, `blockHash`, `blockNumber`, `transactionIndex` | real data                      |
| `from`, `to`, `contractAddress`                                   | real data                      |
| `logs`                                                            | real data                      |
| `cumulativeGasUsed`, `gasUsed`, `effectiveGasPrice`               | `0`                            |
| `logsBloom`                                                       | `0x` + 512 zeros               |
| `postState`                                                       | not set                        |

**`logsBloom` is empty**: Computing a real bloom filter requires Merkle Patricia Trie machinery
that is not implemented. The field is present but set to the all-zero empty bloom.

**EVM-reverted transactions have no receipt**: In Ethereum, a reverted transaction is committed to
the block with `status=0` and a receipt is available. Here, EVM-reverted transactions are never
submitted to the orderer — `eth_getTransactionReceipt` returns `null`. MVCC-rejected transactions
are the exception: they are committed to the block with `status=0` and a receipt IS available.
Clients that branch on `receipt.status === 0` will catch MVCC-rejected transactions but not EVM
reverts (which have no receipt at all).

---

## eth_call

`eth_call` works for standard read-only contract calls but has two non-standard behaviours (for
the block context mismatch, see the Block context section above).

**`from` defaults to the zero address**: If the caller omits the `from` field, `msg.sender` and
`tx.origin` in the EVM are set to `0x000...0`. Contract functions that check `msg.sender` for
access control will behave as if called by the zero address. Standard Ethereum tooling always
includes the caller address in `eth_call` requests; be explicit when querying access-controlled
views.

**No state overrides**: Standard `eth_call` accepts an optional third parameter for ad-hoc state
overrides (`{"address": {"balance": "0x...", "code": "0x..."}}`). This is not implemented. Foundry
and Hardhat tooling that relies on overrides (e.g., `vm.prank`, `deal`) will not work against this
endpoint.

---

## State queries

`eth_getBalance`, `eth_getCode`, `eth_getStorageAt`, and `eth_getTransactionCount` route to an
endorser node for execution. Two caveats:

- **Block-hash queries silently use latest state**: When a block hash is passed as the block
  parameter (`eth_getBalance(addr, {blockHash: "0x..\"})`), the hash is ignored and the query
  always returns the latest state.

- **Read requests go to a single endorser**: All `eth_call` and state query requests are routed
  to `endorsers[0]` only. If that endorser is unreachable, reads will fail regardless of how many
  endorsers are configured.

---

## Error format

The go-ethereum `rpc.Server` is used, so all errors produce valid JSON-RPC error objects (no
plain string responses). However, because our methods return plain Go errors (not types that
implement `rpc.Error` / `rpc.DataError`), every error gets the generic code `-32000` with no
`data` field.

A standard Ethereum node returns for a reverted `eth_call`:
```json
{"code": 3, "message": "execution reverted", "data": "0x08c379a0..."}
```

We return:
```json
{"code": -32000, "message": "execution reverted: <decoded reason>"}
```

Consequences:
- The decoded revert reason is present in `message`, which is useful for debugging but not compliant.
- Libraries that inspect `data` to decode custom Solidity errors (`error Foo(uint amount)`) will
  not work — `data` is never populated.
- There is no distinction between execution errors (revert) and infrastructure errors (endorser
  or orderer unreachable); both produce `-32000`. Clients that branch on error codes cannot tell
  them apart.

---

## Not implemented

- **WebSocket / subscriptions**: no `eth_subscribe`, `eth_unsubscribe`. No `eth_newFilter`,
  `eth_newBlockFilter`, `eth_newPendingTransactionFilter`, `eth_getFilterChanges`,
  `eth_getFilterLogs`, `eth_uninstallFilter`. Poll `eth_blockNumber` and `eth_getLogs` instead.
- **`eth_sendTransaction`**: server-side key management is not supported. Use
  `eth_sendRawTransaction` with a client-signed transaction.
- **Uncle queries** (`eth_getUncleByBlockHashAndIndex`, etc.): always empty; no uncle concept in
  Fabric.
- **`debug_*` / `admin_*` / `personal_*` / `miner_*` / `txpool_*`**: not implemented.
- **`CREATE` deployed address not returned**: `evm.Create` returns the new contract address, but
  this value is discarded. Callers that need the deployed address must compute it themselves:
  `crypto.CreateAddress(senderAddr, tx.Nonce())`.

---

## Internal notes

- **Storage serialisation**: storage slot values are stored as hex strings (`value.Hex()`) in the
  DB rather than raw 32-byte values. This is an internal detail with no impact on opcode behaviour.
- **`executor.chainID` field**: currently hardcoded, should be configurable per network.
