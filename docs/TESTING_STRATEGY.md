# Testing Strategy

This document describes what fabric-x-evm asserts about itself, how each assertion is checked, and
what it deliberately does **not** assert.

For specifics and values that are more subject to change, refer to the following docs:

| Kind of fact                             | Where it lives                         |
| ---------------------------------------- | -------------------------------------- |
| How we differ from Ethereum, and why     | [`COMPATIBILITY.md`](COMPATIBILITY.md) |
| Current pass rates and failure causes    | the gate's own report — see [`cmd/baseline/README.md`](../cmd/baseline/README.md) |
| Which individual tests are known to fail | `testdata/*_known_failures.json`       |
| How the known-failure tooling works      | [`cmd/baseline/README.md`](../cmd/baseline/README.md) |
| What gates what                          | [`.github/workflows/tests.yml`](../.github/workflows/tests.yml) |
| How to run any of it                     | `make help`, [`CONTRIBUTING.md`](../CONTRIBUTING.md) |

---

## Trust boundary — what is ours

We limit retesting of external code so we can make assertions about fabric-x-evm itself.

**Inherited, not re-tested:**

- **The EVM itself** is go-ethereum's. We run its `core/vm` unchanged.
- **Ordering, block delivery, and MVCC conflict rejection** belong to the fabric-x-sdk and the
  fabric-x committer, and are tested there.

**Ours, and therefore asserted here:**

- The **translation between EVM state and Fabric read-write sets** — the StateDB that produces a
  read/write set (RWS), and the trie that replays one.
- **Execution determinism across endorsers.** The SDK guarantees the *protocol*; whether two of our
  endorsers produce identical RWS for the same transaction depends on our executor.
- The **gateway's JSON-RPC surface** and its transaction lifecycle semantics.

---

## Claim register

Each row is one assertion, its ground truth, the level it is tested at, and where its known
exceptions are recorded.

### EVM equivalence

| Claim | Oracle | Level | Suite | Backend | Known exceptions |
| ----- | ------ | ----- | ----- | ------- | ---------------- |
| EVM execution matches Ethereum at Cancun / Prague / Osaka+ | EEST canonical post-state root | system | `TestEthereumTests` | bypass | `testdata/eth_known_failures.json`; causes in [`COMPATIBILITY.md`](COMPATIBILITY.md#evm-execution-differences) |
| Raw-transaction admission accepts/rejects as the spec says | EEST `transaction_tests` verdicts | system | `TestTransactionTests` | bypass | as above |

The oracle is the execution-spec-tests corpus, maintained by the spec authors and shared with every
other client — the strongest available ground truth, and the reason we write no hand-rolled EVM
semantics tests.

### Ecosystem compatibility

| Claim | Oracle | Level | Suite | Backend | Known exceptions |
| ----- | ------ | ----- | ----- | ------- | ---------------- |
| Real contracts and real tooling work over our RPC | OpenZeppelin's own assertions, via Hardhat/ethers | system | OZ suite on `fxevm testnode` | fabrictest | `testdata/oz_known_failures.json`; causes in [`COMPATIBILITY.md`](COMPATIBILITY.md) |

The OpenZeppelin smart contracts are some of the most used and rigorously tested libraries in the
EVM ecosystem. By using their integration tests we exercise far more of the RPC surface, and in more
realistic combinations, than anything we would write by hand. Limitation: they prove contracts
*work*, not that execution is *spec-exact*, and some failures reflect Hardhat-only conveniences that
no production client offers.

### Fabric pipeline

This is where hand-written Go earns its place, because none of it exists in Ethereum.

| Claim | Oracle | Level | Suite | Backend | Known exceptions |
| ----- | ------ | ----- | ----- | ------- | ---------------- |
| Behaviour is identical under `fabric` and `fabric-x` RWS encodings | differential (the two encodings against each other) | system | `TestLocal` / `TestLocalX` | bypass ×2 |
| Multiple transactions commit in one block; conflicts are handled | committed block contents | system | `tether_token_parallel` | all |
| Gateway RPC semantics — nonce, revert, pending, query | hand-written expectations | system | `integration` cases | all |
| Endorsement over gRPC preserves semantics | parity (in-process vs over-wire endorser) | component | `TestGRPCEndorsement_Parity` | — |
| Independent endorsers produce identical RWS | Fabric's endorsement policy — divergent RWS cannot satisfy it | system | `TestFablo` (whole case table, 2-of-2), `two_of_two_endorsement_policy` | fablo, fabric-x |
| Endorsers agree on the transaction timestamp | differential | unit | `TestExecute_IdenticalTimestampAcrossEndorsers` | — |
| KVS backends are interchangeable | parity (`LightKVS` vs `PebbleKVS`) | unit | `endorser/storage/kvs_parity_test.go` | — |

These tests ensure that the whole pipeline works end-to-end, on both Fabric and Fabric-X, with
identical and deterministic endorsements.

### Performance

| Claim | Oracle | Level | Suite | Backend | Known exceptions |
| ----- | ------ | ----- | ----- | ------- | ---------------- |
| The real fabric-x stack endorses, orders, and commits under sustained load | completion without invalid/conflict blowup | system | `integration/perf` replay | fabric-x | reported, **not** enforced |

This measures throughput and a latency breakdown against recorded mainnet datasets and synthetic
load. It also includes a quick smoke test as an indicator that runs on each PR.

---

## Work in progress

Ongoing and possible future steps towards more complete test coverage.

- **Drive the trie check through the production ingestion path**, so a mismatch identifies a real
  translation bug rather than a harness artifact.
- **Make the RWS-replayed state root the conformance oracle**, turning the corpus into a differential
  test of our write-set translation.
- **A long-running soak test** — run the network under sustained load for hours to days, to catch
  failure modes a short replay can't: resource leaks, unbounded log/disk growth, slow degradation.
- **Crash-consistency and restart coverage** across the three persistence surfaces — block store,
  endorser state DB, and trie are updated non-atomically by different components.
- **RPC shape conformance** against the OpenRPC schema. `rpc-compat` is expected to be too much effort
  for what we gain from it, but this is to be decided.
- **Security scanning** — dependency-vulnerability scanning and static analysis.

---

## Baseline vs Parity

We have two mechanical ways to make sure our claims stay true: **baseline** and **parity**.

### Baseline

A suite with known failures checks in a list of them, and a checker diffs actual results against that
list. Three outcomes, and only one of them gates:

| Outcome                            | Meaning                      | Gates?                    |
| ---------------------------------- | ---------------------------- | ------------------------- |
| failing, **not** in the list       | a regression                 | **yes — fails the build** |
| in the list, **no longer failing** | stale entry, clean it up     | no                        |
| in the list and marked **flaky**   | quarantined, whatever it did | no                        |

The goal is:

> **Every known-failure cause is either a documented deviation, with a reference in
> `COMPATIBILITY.md`, or a tracked defect, with an issue.**

### Parity

Where two implementations of the same thing exist, run both and compare. This is how we get high
confidence cheaply, and it is used at several levels:

| Pair                                                    | Where                                 |
| ------------------------------------------------------- | ------------------------------------- |
| `LightKVS` ↔ `PebbleKVS` ↔ `VersionedDB`                | `endorser/storage/kvs_parity_test.go` |
| fabric snapshot StateDB ↔ reference go-ethereum StateDB | `endorser/execution/dual_statedb.go`  |
| in-process endorser ↔ endorser over gRPC                | `TestGRPCEndorsement_Parity`          |
| RWS-replayed trie root ↔ canonical fixture root         | trie oracle (to be added)             |

It also answers a question that otherwise has no good answer: when two storage backends both look
plausible, parity against a common contract enforces the right behaviour.

---

## Levels

> **Behaviour is tested at the lowest level that can observe it. Deployment topologies are a wiring
> concern: tested once per topology, deliberately shallow.**

An end-to-end run exists to prove the pipeline is *connected*. It is not the place to re-derive EVM
semantics or contract behaviour — those have better oracles at cheaper levels. The practical
consequence is that contract-lifecycle cases in the integration suite earn nothing from being run
against every backend: the EVM produces the same result no matter who orders the transaction.

| Level         | Asserts                                  | Example                                         |
| ------------- | ---------------------------------------- | ----------------------------------------------- |
| **unit**      | one component against its contract       | KVS parity, timestamp agreement                 |
| **component** | a boundary; wire protocol, process split | gRPC endorsement parity                         |
| **system**    | the pipeline end to end                  | conformance corpus, OZ suite, integration cases |

---

## Topology and backend

Which backend runs the pipeline is orthogonal to what is being asserted:

- **`bypass`** — endorse, parse the RWS straight back, apply it. No ordering. Fastest.
- **`fabrictest`** — the fabric-x-sdk in-process network: grpc, ordering and commit, no docker.
- **`fabric-x`** — the real committer. Production default.
- **`fablo`** — a classic Fabric network, for compatibility with non-fabric-x deployments.

Orthogonal again is the RWS **encoding** (`fabric` vs `fabric-x`). Real backends fix their own;
`bypass` and `fabrictest` can run either.

The rule for which claims run on which backend: **run a claim on a backend only where that backend
can change the claim's result, or provides a capability the claim needs.** EVM equivalence is
backend-independent and runs on one. Pipeline claims are backend-defining and run on several.
Performance is meaningful only on a real backend.

## What runs when

**Every suite in the claim register runs on every pull request** — including the ones that stand up a
real Fabric-X network.

[`tests.yml`](../.github/workflows/tests.yml) is the executable truth for what runs. The
`timeout-minutes` values there are ceilings, not measurements, and should not be read as an
indication of what anything costs.
