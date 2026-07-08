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

For now, we have chosen to defer gas pricing. The priority is equivalence in contract execution
and API compatibility. State root computation via a Merkle Patricia Trie (MPT) is partly implemented
and disabled for now (see Block representation → `stateRoot`); the log bloom filter
is still left as the empty bloom.

> [!NOTE]
> This document describes the **current state** and not the **planned state**. It is subject
> to change as we develop the components. There are some quick wins with low architectural impact
> that can be developed right away, and there are bigger decisions with trade-offs. These decisions
> will be documented in detail.

## Testing strategy

Alongside hand-written integration tests, the suite runs:

- the official **ethereum/tests** `GeneralStateTests` from a git submodule under
  `testdata/ethereum-tests/` (`TestEthereumTests`) — the same corpus geth and Besu use; the
  checked-out version is filled for the current forks (Cancun/Prague). Slow and known-failing
  vectors are blacklisted (`testdata/eth_tests.slow`); the geth-reference state-root check always
  runs, while verification of our own trie-store root is opt-in (`-verify_root`). So this is a
  correctness signal in progress, not a clean pass. An opt-in `-legacy` flag *instead* runs the
  frozen Constantinople-era snapshot; that is an old-fork regression set only, and several of its
  vectors are *expected* to fail here because this implementation targets modern (Osaka) semantics
  — see the SELFDESTRUCT and `GetStorageRoot` notes under EVM execution differences.
- the **OpenZeppelin** contract test suites, run against a live network via Hardhat
  (`scripts/run_hardhat_test.sh`).

Per-suite pass rates and compatibility matrices will be added as coverage stabilises.

---

## Supported JSON-RPC methods

| Method                                                        | Status | Notes                                          |
| ------------------------------------------------------------- | ------ | ---------------------------------------------- |
| `eth_chainId`                                                 | ✅      |                                                |
| `eth_blockNumber`                                             | ✅      |                                                |
| `eth_getBlockByNumber`                                        | ✅      | several fields hardcoded — see below           |
| `eth_getBlockByHash`                                          | ✅      | several fields hardcoded — see below           |
| `eth_getBlockTransactionCountByHash`                          | ✅      |                                                |
| `eth_getBlockTransactionCountByNumber`                        | ✅      |                                                |
| `eth_getBalance`                                              | ✅      | routes to endorser; see state query caveats    |
| `eth_getCode`                                                 | ✅      | routes to endorser                             |
| `eth_getStorageAt`                                            | ✅      | routes to endorser                             |
| `eth_getTransactionCount`                                     | ✅      | routes to endorser; see nonce caveats          |
| `eth_sendRawTransaction`                                      | ✅      | limited mempool — see finality section         |
| `eth_call`                                                    | ⚠️      | works but has caveats — see eth_call section   |
| `eth_getTransactionByHash`                                    | ✅      | includes pending (null block fields)           |
| `eth_getTransactionByBlockHashAndIndex`                       | ✅      |                                                |
| `eth_getTransactionByBlockNumberAndIndex`                     | ✅      |                                                |
| `eth_getTransactionReceipt`                                   | ✅      | see receipt section                            |
| `eth_getLogs`                                                 | ✅      |                                                |
| `eth_estimateGas`                                             | 🔧      | returns `10_000_000` after an `eth_call` check |
| `eth_gasPrice`                                                | 🔧      | always returns `0`                             |
| `eth_maxPriorityFeePerGas`                                    | 🔧      | always returns `0`                             |
| `eth_feeHistory`                                              | 🔧      | returns all-zero arrays                        |
| `net_version`                                                 | ✅      | returns chain ID as network ID                 |
| `net_listening`                                               | 🔧      | always returns `true`                          |
| `web3_clientVersion`                                          | 🔧      | returns `"fabric-evm/0.1.0"`                   |
| `eth_subscribe` / `eth_unsubscribe`                           | ❌      | no WebSocket support                           |
| `eth_newFilter` / filter APIs                                 | ❌      | no server-side filter state; use `eth_getLogs` |
| `eth_sendTransaction`                                         | ❌      | server-side signing not supported              |
| `eth_pendingTransactions`                                     | ❌      | endpoint not implemented                       |
| `eth_getUncleBy*` / uncle count                               | ❌      | no uncle concept in Fabric                     |
| `debug_*` / `admin_*` / `personal_*` / `miner_*` / `txpool_*` | ❌      | not implemented                                |

Legend: ✅ works as expected · ⚠️ partially works · 🔧 stubbed/mocked · ❌ not implemented

---

## Transaction lifecycle and finality

Standard Ethereum broadcasts a transaction into a mempool; it is executed and a receipt appears
when mined, and it can be reorged out later. Submission here (`eth_sendRawTransaction` validates,
enqueues, and returns the hash; a background worker endorses and submits) looks the same to a
client — return a hash, poll for a receipt. The execute-order-commit differences are:

- **Finality is immediate**: once a transaction is in a committed block it is final — no reorgs,
  no confirmations to wait for. (Hence the `safe`/`finalized` tags equal `latest`; see Block
  number tags.)

- **MVCC conflicts are a failure mode with no Ethereum equivalent**: a transaction that endorses
  successfully can still fail at commit time if another transaction modified the same state keys in
  between. It is committed with a non-zero Fabric validation code and `status=0`, and none of its
  EVM state changes — including the nonce increment — are applied. At the receipt level this is
  **indistinguishable** from an ordinary EVM revert (both are `status=0`), so a client cannot tell
  a revert from a lost MVCC race; only the internal Fabric validation code separates them.

- **Limited mempool**: submitted transactions are briefly queued before endorsement, but this is
  not a full Ethereum mempool — transaction replacement/cancellation is unsupported and nonce gaps
  are admitted rather than held until the gap fills (see Nonce management and Pre-flight transaction
  validation). A new block is also only produced when there is transaction activity (see Block
  representation → Block production).

---

## Pre-flight transaction validation

`eth_sendRawTransaction` runs geth's stateless validation before a transaction is enqueued for
endorsement, so callers see geth-style errors immediately. The gateway calls
`core/txpool.ValidateTransaction` directly (see `gateway/core/validate.go`); upgrading
go-ethereum picks up upstream changes automatically.

The only stateful check is **nonce-too-low**: we look up the sender's committed nonce from the
endorser and reject the transaction if it is lower than expected. We do not call
`txpool.ValidateTransactionWithState` — building a per-tx `*state.StateDB` for one nonce/balance
lookup is too expensive — so the balance/cost check is skipped (gas is not metered anyway).

**Deliberate deviations from geth's failure model:**

- **Sender balance is not validated**: `txpool.ValidateTransactionWithState` would reject a tx
  whose `gas × gasPrice + value` exceeds the sender balance. We skip this since gas is not
  metered and balances are unfunded by default (see Gas and fees).
- **Replacement transactions are not supported**: tracked in #62. A queued transaction cannot be
  replaced or cancelled before it is endorsed.
- **Nonce gaps are accepted**: a transaction with a nonce higher than the account's current
  nonce is admitted; the endorser enforces ordering at execution time.
- **Blob transactions (EIP-4844, type 3) are rejected at submission**: the `Accept` bitmap
  excludes them and `MaxBlobCount` is `0`.
- **Set-code transactions (EIP-7702, type 4) are rejected at submission**: the `Accept` bitmap
  excludes them. Authorization-list checks are therefore skipped entirely.
- **Synthetic block context for stateless rules**: `head.Number = 0`, `head.Time = 0`,
  `head.Difficulty = 0` (post-merge), `head.GasLimit = math.MaxUint64`. All forks in our chain
  config activate at genesis, so any `(number, time)` yields the same fork rule set. Because the
  block gas limit is effectively unbounded, the binding submission-time gas ceiling is the per-tx
  Osaka cap (`params.MaxTxGas` = `1<<24` = 16,777,216), which geth enforces in
  `txpool.ValidateTransaction` while Osaka is active.
- **No RPC tx-fee cap**: geth's `internal/ethapi.SubmitTransaction` rejects transactions whose
  total fee exceeds an operator-configured cap. We don't expose such a knob; submission is
  accepted regardless of fee size, subject to the 256-bit sanity checks geth applies.
- **`MinTip = 0`**: any tip is accepted; we do not enforce a mempool-style minimum.

The replay-protection (`!tx.Protected()`) and `MaxSize` (128 KiB) checks match geth's defaults.

---

## Nonce management

**Pre-flight nonce validation**: A transaction whose nonce is **lower** than the sender's
committed nonce is rejected synchronously by `eth_sendRawTransaction` with `nonce too low`.
Higher-nonce gaps, however, are admitted rather than held until the gap fills — see "Pre-flight
transaction validation" above.

**Nonce increment on revert vs MVCC conflict**: a transaction that the EVM **reverts** still
increments the sender's nonce — geth bumps the nonce before the revertable EVM frame, and that
write is part of the committed read-write set. A transaction rejected at commit time by an
**MVCC conflict** does *not* increment the nonce, because Fabric discards its entire write set
(the nonce increment included).

**No pending nonce**: `eth_getTransactionCount` returns the **committed** nonce for an address —
it routes to the endorser's state DB, which reflects only committed blocks and does not consult
transactions still in flight in the gateway's queue. Two transactions sent in rapid succession
from the same address will therefore:

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

**Fork-level opcodes and precompiles**: all EVM opcodes through Osaka are active, including
`MCOPY` (EIP-5656), `TLOAD`/`TSTORE` (EIP-1153), `BLOBHASH`/`BLOBBASEFEE` (EIP-4844), and the
BLS12-381 precompiles (EIP-2537). The difference is at the **transaction-type** level: blob
(type 3) and set-code (type 4) transactions are rejected at submission (see Pre-flight transaction
validation), so although the opcodes exist, those tx-type code paths are unreachable.

**`SELFDESTRUCT` is partially implemented** (EIP-6780, active from Osaka): `SelfDestruct(addr)`
zeroes the destructed account's *own* balance, and — only if the contract was created in the same
transaction — marks it destructed so `HasSelfDestructed` returns `true`. It does **not** transfer
the balance to the beneficiary inside the StateDB, and it does **not** clear the contract's code
or storage. So same-transaction create-then-destruct is detectable, but cross-transaction cleanup,
ETH recovery to the beneficiary, and storage erasure do not work as on Ethereum. The logic is also
**not fork-aware** — it always applies EIP-6780, so pre-6780 semantics (full destruction of a
pre-existing contract, and the since-removed SELFDESTRUCT gas refund) are never reproduced, even
when a call or test targets an older fork.

Implementation note: clearing storage slots requires enumerating all keys for the address
(non-trivial) and may produce an impractically large RWSet for contracts with many storage entries.

**`GetStorageRoot` is a stub**: it returns the zero hash. geth uses an account's storage root for
the EIP-7610 collision guard — a `CREATE`/`CREATE2` must fail if the target address already holds
storage. With the stub that guard is effectively disabled: a contract can be deployed onto an
address that has storage (but no code or nonce). Collisions detected via code or nonce still apply.

**How these surface in equivalence tests**: the `ethereum/tests` harness runs the EVM against our
StateDB while mirroring every write to a reference go-ethereum StateDB, then compares the two state
roots. Because the EVM reads account state (existence, `HasSelfDestructed`, storage root) from
*our* StateDB, the two deviations above make a small, known set of vectors diverge on the final
root — SELFDESTRUCT and CREATE/CREATE2-collision / empty-account cases. These divergences are
deliberate consequences of the modern-only semantics above, not regressions.

**Native ETH balances not funded**: balances are implemented but unused. Accounts have zero ETH 
balance by default. Value transfers inside the EVM (`CALL` with value, `SELFDESTRUCT` beneficiary, 
etc.) will fail or produce wrong results for accounts that were never explicitly funded.

---

## Block context and chain environment

The following values are hardcoded or synthetic. Contracts should not rely on them matching real
network values.

| Opcode / field              | This system                                     | Ethereum                    |
| --------------------------- | ----------------------------------------------- | --------------------------- |
| `BLOCKHASH(n)`              | always `0x000…`                                 | hash of block `n`           |
| `COINBASE`                  | `0x000…`                                        | block proposer address      |
| `DIFFICULTY` / `PREVRANDAO` | `0x000…` (stub — do not rely on for randomness) | current random / difficulty |
| `BASEFEE`                   | `0`                                             | actual EIP-1559 base fee    |
| `BLOBBASEFEE`               | ~1 wei (calculated from `ExcessBlobGas = 0`)    | actual EIP-4844 blob fee    |
| `TIMESTAMP`                 | always `1_000_000`                              | actual Unix timestamp       |
| `NUMBER`                    | `0` on tx execution; block arg on `eth_call`    | Ethereum block number       |

**Current gateway behaviour**: the executor builds the EVM `BlockContext` from defaults; there is
no per-call block-info plumbing. For **transaction execution** the EVM `NUMBER` is `0` (the block
context number is never set) and `TIMESTAMP` is `1_000_000`, even though the state is read from the
latest committed block. So `block.number` inside an executed transaction always reads `0`.

**`eth_call` with a block number**: the state DB is correctly snapshotted at the requested height,
and the EVM `NUMBER` opcode is set to that block-number argument (`0` for `latest`). `TIMESTAMP`,
however, is always `1_000_000` regardless of the requested block. Contracts that read
`block.timestamp` inside a view function therefore see a fixed, non-historical value (and
`block.number` reads `0` for the common `latest` call).

---

## Gas and fees

Gas mechanics are intentionally not implemented.

| Aspect                      | Fabric                               | Ethereum                            |
| --------------------------- | ------------------------------------ | ----------------------------------- |
| `GASPRICE` opcode           | `0`                                  | actual tx gas price                 |
| Sender balance check        | not performed                        | must cover `gas × gasPrice + value` |
| Intrinsic gas deduction     | enforced at submission, not deducted | ~21 000 deducted before execution   |
| Gas refund counter          | always `0`                           | tracks SSTORE/SELFDESTRUCT refunds  |
| Default gas per call/deploy | `5 000 000` if not specified         | whatever the tx sets                |
| Block gas limit             | `300 000 000` (EVM block context)    | network-set limit                   |

**JSON-RPC fee stubs**: `eth_gasPrice` and `eth_maxPriorityFeePerGas` always return `0`;
`eth_feeHistory` returns all-zero arrays. `eth_estimateGas` first runs an `eth_call` (so it *does*
surface reverts as `-32000`; see eth_call / Error format), then returns the constant `10_000_000`
— it is not a real estimate. Clients that use these values to set gas on future transactions will
set `gasPrice = 0` and `gas = 10_000_000`, which is harmless here since gas is not enforced, but
may confuse tooling.

---

## Block representation

| Field                                    | Value                              | Notes                                             |
| ---------------------------------------- | ---------------------------------- | ------------------------------------------------- |
| `number`                                 | Fabric block number                |                                                   |
| `hash`                                   | Fabric block header hash           |                                                   |
| `parentHash`                             | Fabric previous block hash         |                                                   |
| `timestamp`                              | Node wall-clock time at parse time | **Not** the Fabric block creation time — see note |
| `transactions`                           | Full objects or hashes             | real data                                         |
| `logsBloom`                              | `0x` + 512 hex zeros (empty bloom) | always the empty bloom — see Receipt note         |
| `transactionsRoot`                       | empty-txs hash / zero              | no per-block MPT — see note                       |
| `stateRoot`                              | MPT hash (only if trie enabled)    | not Ethereum-compatible — see note                |
| `receiptsRoot`                           | empty-trie root                    | no MPT                                            |
| `miner`                                  | `0x…0F4B`                          | sentinel; `COINBASE` opcode is `0x0` (separate)   |
| `gasLimit` / `gasUsed` / `baseFeePerGas` | `0`                                | gas not metered                                   |
| `difficulty` / `totalDifficulty`         | `0`                                |                                                   |
| `uncles`                                 | `[]`                               | no uncle concept                                  |
| `size`                                   | `0`                                |                                                   |
| `extraData`                              | `"0x"`                             | empty bytes                                       |

**Block timestamp**: The `timestamp` field is set to the node's wall-clock time
(`time.Now().Unix()`) when the block is received and parsed by the Fabric SDK, as there is no
authoritative block creation timestamp. Values can diverge across nodes and across restarts. Do
not use block timestamps for precise time ordering.

**State and transaction roots**: `transactionsRoot` is the canonical empty-txs hash for empty
blocks and the zero hash for blocks that contain transactions — it is never a real per-block MPT
root, and `receiptsRoot` is always the empty-trie root. `stateRoot` is a real MPT root over the
world state **only when the trie is enabled** (otherwise it is the empty-trie root). Even when
enabled, it will not match an Ethereum node's state root: storage values are stored as ASCII hex
strings (see Internal notes), so the trie's leaf encoding differs.

**Block number tags**: there are two resolvers, and they behave differently:
- Block lookups (`eth_getBlockByNumber`, `eth_getBlockTransactionCountByNumber`,
  `eth_getTransactionByBlockNumberAndIndex`) use `blockNumberToUint64`: `earliest` and the literal
  `0x0` resolve to block `0` (genesis); `latest`, `pending`, `safe`, and `finalized` all resolve
  to the latest committed block.
- State queries and `eth_call` (`eth_getBalance`, `eth_getCode`, `eth_getStorageAt`,
  `eth_getTransactionCount`, `eth_call`) use `rpcBlockNumberToBigInt`: only `latest`/`pending`
  resolve to latest; `earliest` resolves to block `0`; `safe`/`finalized` are passed through as
  the raw negative sentinels (`-4`/`-3`) to the endorser, which does not interpret them as
  "latest" — so do **not** rely on `safe`/`finalized` for state reads.

In practice every committed Fabric block is final, so `finalized == latest` is semantically
correct for block lookups; the lost distinction may still surprise ethers.js v6 / viem tooling.

**The first EVM block is not block 0**: Fabric channel genesis and any configuration transactions
precede the first EVM transaction, so the lowest block number that contains EVM data is 2 or
higher. Block `0` is addressable (`earliest` / `0x0` map to it), but it is the Fabric genesis
block and holds no EVM transactions. Event indexers that start scanning from block 0 will simply
find no EVM history until the first EVM block.

**Block production requires transactions**: Fabric only creates new blocks when there are
transactions to order. If no transactions are submitted, the block number does not advance.
Clients that poll `eth_blockNumber` waiting for the next block, or that use `eth_getLogs` with a
moving `toBlock`, will wait indefinitely if no transaction activity is happening. This affects:
- ethers.js `provider.waitForBlock(n)` and block listener callbacks
- viem `watchBlocks`
- Any client waiting for at least `n` confirmations.

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

**`logsBloom` is empty**: the bloom filter is not computed; the field is present but always set to
the all-zero empty bloom (`0x` + 512 hex zeros). Clients that pre-filter logs by testing the bloom
will get no matches and must rely on `eth_getLogs` instead.

**`status=0` is overloaded**: a `status=0` receipt may mean an ordinary EVM revert *or* a Fabric
MVCC conflict — the two are indistinguishable at this level (see Transaction lifecycle and
finality).

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
endorser node for execution. Caveats:

- **Block-hash queries resolve to the historical block**: when a block hash is passed as the block
  parameter, the gateway resolves it to a block number (`BlockNumberByHash`) and snapshots state
  at that height. An unknown hash returns `ethereum.NotFound`. (See the Block number tags note for
  how the `safe`/`finalized` *tags* behave on state queries.)

- **Read requests go to a single endorser**: all `eth_call` and state-query requests are routed to
  `endorsers[0]` only. If that endorser is unreachable, reads fail regardless of how many
  endorsers are configured.

- **Write transactions fan out to all endorsers**: by contrast, a submitted transaction is sent to
  *every* configured endorser in parallel and all must return a successful endorsement (the first
  error aborts submission). More endorsers therefore increase the chance a transaction fails to be
  endorsed if any one of them is unhealthy.

---

## Error format

The go-ethereum `rpc.Server` is used, so all errors produce valid JSON-RPC error objects.
Methods classify errors via the typed `rpcerr` package (`gateway/api/rpcerr`) so callers
receive standard Ethereum codes.

| Surface                                                                                 | Code                          | Notes                                       |
| --------------------------------------------------------------------------------------- | ----------------------------- | ------------------------------------------- |
| Malformed input (bad hex, unparseable raw tx, invalid call args)                        | `-32602` Invalid params       |                                             |
| Validation rejection (nonce, intrinsic gas, funds, type, sender, EIP-3860, unprotected) | `-32003` Transaction rejected |                                             |
| `eth_call` revert                                                                       | `-32000` Execution reverted   | `data` carries the raw revert payload (hex) |
| Backend lookup / endorser / orderer failure                                             | `-32603` Internal error       |                                             |

For a reverted `eth_call` the gateway returns:
```json
{"code": -32000, "message": "execution reverted: <decoded reason>", "data": "0x08c379a0..."}
```

Geth uses `code: 3` historically; the JSON-RPC spec reserves the server range `-32000` to
`-32099`, which is what geth's own `rpc` server emits today. Libraries that decode custom
Solidity errors (`error Foo(uint amount)`) read `data` directly and work unchanged.

Note: `eth_estimateGas` returns a constant rather than a real estimate, but it first runs an
internal `eth_call`, so it *does* surface reverts as `-32000` (with the revert payload as `data`).
The returned gas value is therefore meaningless, but a revert during estimation is reported the
same way a real node would report it.

See [`docs/JSON_RPC_ERRORS.md`](JSON_RPC_ERRORS.md) for the full per-method mapping,
example error objects, and the layering of the classifier.

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
