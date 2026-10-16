# OpenZeppelin Compatibility

Status and backlog for the OpenZeppelin Hardhat suite — the ecosystem-compatibility claim in
[`TESTING_STRATEGY.md`](TESTING_STRATEGY.md#ecosystem-compatibility). Its job: turn the suite from a
wall of red into a **compatibility signal that moves** — a pass rate, a ranked list of what to fix
next, and a CI gate that catches regressions.

New here? [`CONTRIBUTING.md`](../CONTRIBUTING.md#picking-up-a-compatibility-failure) explains how to
pick an item off the backlog below and land a fix.

[`COMPATIBILITY.md`](COMPATIBILITY.md) remains the canonical *node-vs-Ethereum* deviation reference.
Where an OZ failure is a **deliberate** deviation, this doc names the cause and points there instead
of restating it.

---

## The scorecard

The spine of this doc: one suite run produces one score, and the score is what we manage.

- **Headline** — passes / failures / skipped + a pass-rate percentage, from a full-sweep run
  (`--bail` off, so the whole set completes rather than stopping at the first failure).
- **By-suite breakdown** — the same headline, split per top-level `test/` directory and sorted
  worst-first, so it's obvious where to look next and easy to watch a specific area's number climb
  as fixes land.
- **Cause histogram** — the actionable output. Each baseline entry gets a
  [cause taxonomy](#cause-taxonomy) bucket (`max-code-size`, `hardhat-time-rpc`, `fixed-timestamp`,
  `subcall-effect-lost`, `hardhat_setBalance`, `fixed-block-number`, …); the histogram is a `group by`
  over those tags, falling back to the raw failure message for anything still untagged. Tagging is a
  mix of automatic and manual: a message that names its own cause (a missing RPC method) is
  auto-tagged on `update`; a bulk `tag --match <substring> --cause <name>` command handles symptoms
  that are real but too varied in wording to recognize mechanically (a whole describe block, a shared
  error shape); genuine one-offs are left for a human to look at individually. This ranks fixes by
  ROI: *"pinning an endorser-agreed `block.number` unlocks N tests."*

`cmd/baseline` reads Mocha's own JSON reporter output and diffs it against
`testdata/oz_known_failures.json`, gating the `oz-hardhat-compat` CI job. The
[taxonomy](#cause-taxonomy) **defines** the buckets; each baseline entry's `cause` field **assigns**
one — see [`cmd/baseline/README.md`](../cmd/baseline/README.md) for the full mechanism and sample
output. Entries marked `flaky` are reported as *quarantined* and never gate.

### Baseline — the CI gate

This suite's expected failures live in **`testdata/oz_known_failures.json`**, each tagged with a
cause bucket. For how the idiom works and why it is not a skip list, see
[`TESTING_STRATEGY.md`](TESTING_STRATEGY.md#baseline); for the tool, see
[`cmd/baseline/README.md`](../cmd/baseline/README.md).

### Reading the current numbers

**No pass rates are written down here — they go stale within days.** Get them from the gate's own
output instead. The `oz-hardhat-compat` job uploads its report as an artifact, so the authoritative
figures are one command away. Use the latest `main` run, not a branch run — branch runs carry that
branch's baseline:

```shell
gh run list --workflow=Tests --branch main --limit 1     # get the run id
gh run download <run-id> -n oz-baseline-report -D /tmp/oz
cat /tmp/oz/report.md          # headline, by-suite breakdown, cause histogram
```

For the checked-in baseline itself — entry count, how many are tagged, how many are quarantined:

```shell
python3 -c "import json,collections as c;d=json.load(open('testdata/oz_known_failures.json'));\
print('entries',len(d),'| flaky',sum(1 for e in d if e.get('flaky')),\
'| untagged',sum(1 for e in d if not e.get('cause')));\
print(c.Counter(e.get('cause') or '(untagged)' for e in d).most_common(12))"
```

The compatible set is every `test/**/*.test.js` except `test/account/**` and
`test/utils/Blockhash.test.js` (see [Running](#running-the-suite)). A submodule bump moves both the
file count and the test count — see
[After bumping openzeppelin-contracts](../cmd/baseline/README.md#after-bumping-testdataopenzeppelin-contracts)
in `cmd/baseline`'s README for the reconciliation workflow.

**What to look at in that output**, beyond the headline:

- **Which suites lag.** `governance` and `access` have consistently trailed the rest; `token` and
  `utils` lead. Small suites (`finance`, `metatx`) swing wildly on a handful of tests, so read their
  percentages with care.
- **How much is quarantined.** A large fraction of the baseline is marked `flaky`, and quarantined
  entries never gate — they are tests that currently tell us nothing, not incompatibilities we are
  tracking. That share is worth watching as closely as the pass rate.
- **`execution reverted`.** One of the biggest buckets is a raw message grouping rather than a real
  cause, so it is certainly several distinct issues wearing one label. Per the classification
  invariant in [`TESTING_STRATEGY.md`](TESTING_STRATEGY.md#baseline), splitting it is real
  work and a good contributor on-ramp.

---

## Running the suite

### Backend: `fxevm testnode` on fabrictest

The run path is [`fxevm testnode`](../cmd/fxevm/main.go) with no `--config`: a self-contained
in-process `fabrictest` network — no docker, no Fablo, no generated crypto — cheap enough to gate
every PR. [`scripts/run_hardhat_test.sh`](../scripts/run_hardhat_test.sh) starts it directly: install
OZ deps → `go run ./cmd/fxevm testnode` → poll `:8545` → run hardhat.

`make hardhat-tests` runs the whole compatible set this way: one Hardhat invocation per top-level
`test/` directory, results written to `testdata/oz-hardhat-results/*.json`, then `cmd/baseline check`
runs automatically at the end and prints the report — one command gets you both the live console view
and the diff. Narrow it with `FILE=`, `GREP=`, or `PORT=`; a narrowed run skips the baseline diff,
since a partial run cannot tell a test that did not fail from one that did not run.

`testnode --config <fabx-or-fablo>.yaml` still preserves the option of running against a real backend
with test RPC forced on, for periodic real-infra verification.

This Hardhat version has no `--reporter` CLI flag — the reporter is set via `HARDHAT_JSON_OUTPUT`
instead (read by `testdata/hardhat.wrapper.config.js`, which switches to the combined reporter in
`testdata/mocha-spec-and-json-reporter.js`: human-readable to stdout, JSON to the given path, in one
run). `run_hardhat_test.sh` already wires this up per directory; the one-liner below is just
the underlying mechanism, useful for running a single file or a custom subset by hand:

```
HARDHAT_JSON_OUTPUT=oz-results.json npx hardhat test <compatible set> \
  --config testdata/hardhat.wrapper.config.js --network fabricevm
```

### The compatible set + exclusions

`run_hardhat_test.sh` drives this per top-level `test/` directory (`COMPAT_DIRS` in the
script) rather than one giant invocation, so a load-time crash in one directory's tests doesn't zero
the report for the other seven. Equivalent as a single command from
`testdata/openzeppelin-contracts/` (zsh — the `A~B` exclusion operator needs `extended_glob`):

```zsh
setopt extended_glob
npx hardhat test test/**/*.test.js~test/account/*~test/account/**/*~test/utils/Blockhash.test.js \
  --config ../hardhat.wrapper.config.js --network fabricevm
```

`test/account/**` (15 files) and `test/utils/Blockhash.test.js` are excluded because they need the
`hardhat-predeploy` plugin, which plants singleton contracts (the ERC-4337 EntryPoint, the EIP-2935
history contract) via `hardhat_setCode` — which the test backend stubs as a **no-op**
([`hardhat_helpers.go:41`](../gateway/testimpl/hardhat_helpers.go#L41)).

Subtlety worth keeping: the test RPC's `web3_clientVersion` returns `"HardhatNetwork/..."`
([`server.go:84`](../gateway/testimpl/server.go#L84)), so the predeploy plugin *believes* it is on a
Hardhat dev network and calls the no-op `hardhat_setCode`. The predeploy globals stay undefined, and
`test/account/utils/draft-ERC4337Utils.test.js` dereferences `predeploy.entrypoint` at the top level
of a `describe` block — a **load-time** crash that aborts the entire run, not just that file. Hence
the whole tree is excluded rather than left to fail individually.

---

## Cause taxonomy

These buckets are both the failure explanation **and** the scorer's histogram categories.

### Known deviations (documented in COMPATIBILITY.md) — not "no action"

These are deliberate simplifications the current implementation makes, and the suite surfaces them
honestly rather than hiding them — they are not bugs. But "known deviation" describes *today's
status quo*, not a decision to never fix them. Effort varies a lot per row; some are genuinely hard
for architectural reasons, not because nobody's gotten to them yet.

| Symptom | Cause | `cmd/baseline` tag | # | Effort | COMPATIBILITY.md § |
| --- | --- | --- | --- | --- | --- |
| `insufficient funds for gas * price + value` | EOAs are unfunded; any value transfer fails the balance check | `insufficient-funds` | 14 (was 93) | **Done, mostly** (see below) | Gas and fees |
| `block.timestamp` is a fixed value | was `TIMESTAMP` hardcoded to `1_000_000`; **fixed** in #317 (see below) | `fixed-timestamp` | 38 (was 64+) | **Done, mostly** (see below) | Block context and chain environment |
| `block.number` reads `0` in tx execution | EVM `NUMBER` is `0`; block context number never set (`Execute` still calls `newExecutor(nil, blockTime)` — [`executor.go:80`](../endorser/execution/executor.go#L80)) | `fixed-block-number` | 18 (was 12+) | **Hard** (see below) | Block context and chain environment |
| randomness / `prevrandao` is zero | `PREVRANDAO` stub returns the zero hash ([`executor.go:245`](../endorser/execution/executor.go#L245)) | — | — | **Hard** — same root cause as timestamp | Block context and chain environment |
| block `stateRoot` / `transactionsRoot` / `receiptsRoot` checks fail | no per-block MPT; roots zero/empty (trie disabled in production) | — | — | **Medium** (see below) | Block representation |

The two block-context tags look like one deviation but tag separately: the same fixed values surface
as a raw timestamp/number diff, a delay/schedule assertion, or (for checkpoint-based contracts like
`ERC20Votes`) an undercount, since votes and checkpoints never actually advance in time. `fixed-block-
number` is the narrower case — only where the message or test name explicitly names block number;
everything else in that family defaults to `fixed-timestamp`.

**`insufficient-funds` — Done.** Fixed in [#296](https://github.com/hyperledger/fabric-x-evm/pull/296)
("pre-fund known Hardhat test accounts on testnode") using the balance-priming infrastructure that
already existed in the test backend
([`balance_priming_executor.go`](../endorser/testimpl/balance_priming_executor.go)). Test-backend
only — real accounts correctly still start at zero balance in production. The tag no longer appears
in the baseline.

### Why the block-context values are fixed

This is the reasoning behind the `fixed-timestamp`, `fixed-block-number` and randomness rows above,
and behind why they are not all equally hard.

This project endorses transactions the Fabric way: multiple independent endorsers each
simulate a transaction and must produce **byte-identical read-write sets** before the endorsement
policy is satisfied ([`ARCHITECTURE.md`](ARCHITECTURE.md), step 9: *"All endorsers produced identical
read-write sets (deterministic execution)"*). Ordering — the step that fixes a transaction's actual
position in the chain — happens **after** endorsement (step 11), not before.

That sequencing means none of `block.timestamp`, `block.number`, or `PREVRANDAO` can simply become
"real" the way a naive fix would try: if each endorser read its own wall clock, its own randomness
source, or guessed at "the next block number," independent endorsers running on separate machines at
different real moments would compute **different** values for any contract that reads or branches on
them — and the read-write sets would diverge, breaking endorsement before ordering ever happens. This
is the mirror image of real Ethereum, where a single block proposer picks these values *once* and
every validator just checks them; Fabric-X's endorse-then-order pipeline has no equivalent role that
picks a value *before* endorsers run.

**How `block.timestamp` got out of this bind ([#317](https://github.com/hyperledger/fabric-x-evm/pull/317)):**
it doesn't need a *pinned* value at all — just an *agreed* one, and the gateway is already the single
point every endorser's request passes through. `EndorsementClient.ExecuteTransaction` now stamps
`reqTime := time.Now()` **once** ([`gateway/core/endorse.go:66`](../gateway/core/endorse.go#L66)) and
sends that same value to every endorser in the proposal; each endorser validates it's within a
configurable past/future skew window and then sets `blockCtx.Time` to it. No endorser samples its own
clock, so the read-write sets stay identical — the "mirror image of Ethereum" gap closes because the
gateway plays the role a block proposer would. `eth_call`'s `TIMESTAMP` was aligned to the same
wall-clock convention so views match executed state. This is why `fixed-timestamp` shrank rather than
disappearing: real elapsed time now flows through, but tests that need to *jump* time (days of
vesting, delay expiry) still get nothing from a stub `evm_increaseTime` — see `hardhat-time-rpc`
below, which is where most of those moved.

**`block.number` / `PREVRANDAO` — still Hard**, and the timestamp trick doesn't transfer. Embedding
an agreed value into the proposal works when every endorser can agree what the value *is* — wall-clock
time is measurable by everyone (up to skew), but block number has an extra wrinkle: endorsers already
read state at a known committed height, so that height is one natural candidate to pin, but it may
not equal the transaction's *eventual* commit height if other transactions get ordered first — a
value that's deterministic across endorsers but not necessarily accurate. Randomness is harder still:
real Ethereum's own PREVRANDAO is itself a pre-agreed value from consensus (the previous epoch's
RANDAO reveal), not true runtime entropy, and there's no equivalent "agreed random beacon" in the
endorsement flow to borrow from today, the way the gateway's clock now serves for timestamp. A
gateway-supplied, bounded block number is tracked in **#278**; randomness has no design yet. That is
why both stay tagged **Hard** rather than **Investigate** or **Easy** — despite `block.number` being
one of the largest buckets in the baseline, directly and again indirectly via the mine work below.

**`stateRoot`/`transactionsRoot`/`receiptsRoot` — Medium.** Unlike the row above, this one *isn't*
subject to the same endorser-divergence problem — the trie root is computed once, after ordering and
commit, by replaying the already-agreed RWS (see
[`TESTING_STRATEGY.md`](TESTING_STRATEGY.md#gap-register)). The trie
machinery already exists (`gateway/storage/trie/`); it's disabled in production (`withTrie=false`)
because the RWS→trie translation does not yet reproduce canonical state roots. Running the
conformance suite with `-verify_root=true` currently mismatches on the large majority of fixtures,
across every fixture group — so this is not the short list of deletion-semantics bugs it was once
believed to be, and it is not yet diagnosed. Note that check runs through a bespoke reconstruction
rather than the production ingestion path, so the harness itself is not ruled out. The path to
production: drive the check through `Chain.Handle`, root-cause what it then shows, flip `withTrie`
on, then implement `eth_getProof` against the now-real
trie. That last part is real, standard functionality (EIP-1186) used by light clients and
cross-chain bridges — not a Hardhat-only convenience — so it's tracked here rather than with the
test-RPC-only stubs below.

### Test-RPC-only conveniences (test backend only — cannot affect production)

`testimpl` is gated behind `EnableTestRPC` and never compiled into a production path. Unlike the
deviations above, these genuinely have no production analog: Hardhat's `hardhat_*` namespace is a
local-dev-network testing API with no equivalent in any real Ethereum client (mining on demand,
impersonating an arbitrary address, overwriting storage directly), and `eth_signTypedData_v4` /
`personal_sign` require the node to hold and use a user's private key server-side — the exact
pattern the test backend's own startup warning already calls out: *"Server-side signing is unsafe
and should never be used in production."*

**Update — most of this table shipped.** `cmd/baseline` auto-tags a failure from the literal
"method ... does not exist" RPC error, so registering a stub (even a no-op one) makes the old tag
disappear from `oz_known_failures.json` — even where the underlying behavior still isn't real. That's
exactly what happened to `hardhat_mine`: the method-not-found error is gone, but the tests that needed
it to actually do something reappeared under a new tag, `hardhat-time-rpc` (see below).
`hardhat_setBalance` was found newly-missing while investigating `hardhat_mine`.

| Symptom | Cause | `cmd/baseline` tag | # now (was) | Status |
| --- | --- | --- | --- | --- |
| `eth_signTypedData_v4 does not exist/is not available` | test RPC lacked typed-data signing | `eth_signTypedData_v4` | 0 (163) | **Done** — [#298](https://github.com/hyperledger/fabric-x-evm/pull/298) added `SignTypedDataV4` to `TestEthAPI` ([`test_eth_api.go`](../gateway/testimpl/test_eth_api.go)). The tag no longer appears in the cause histogram. |
| `hardhat_mine does not exist/is not available` | only `hardhat_setCode`/`evm_*` were registered | `hardhat_mine` | 0 (125) | **Done, but stub-only** — [#271](https://github.com/hyperledger/fabric-x-evm/pull/271) registered `HardhatAPI.Mine` ([`hardhat_helpers.go:54`](../gateway/testimpl/hardhat_helpers.go#L54)) and `EvmAPI.Mine` (from the original Hardhat-support scaffolding, [`hardhat_helpers.go:224`](../gateway/testimpl/hardhat_helpers.go#L224)) is stubbed too. Neither cuts a block or advances time. See [Making `hardhat_mine` real](#making-hardhat_mine-real) below — where the 125 tests' failures actually moved to. |
| `hardhat_impersonateAccount does not exist/is not available` | same registration gap as `hardhat_mine` | `hardhat_impersonateAccount` | 0 (29) | **Done** — [#280](https://github.com/hyperledger/fabric-x-evm/pull/280) registered the stub ([`hardhat_helpers.go:61`](../gateway/testimpl/hardhat_helpers.go#L61)). |
| `personal_sign does not exist/is not available` | not implemented in the test RPC | `personal_sign` | 0 (5) | **Done** — added in [#298](https://github.com/hyperledger/fabric-x-evm/pull/298) alongside `SignTypedDataV4`. |
| `evm_setAutomine does not exist/is not available` | not implemented | `evm_setAutomine` | 0 (6) | **Done** — [#291](https://github.com/hyperledger/fabric-x-evm/pull/291) registered the stub. |
| `hardhat_setStorageAt does not exist/is not available` | not implemented | `hardhat_setStorageAt` | 3 (3) | Still open — small stub, low value on its own. |
| `hardhat_setBalance does not exist/is not available` | not implemented in the test RPC | `hardhat_setBalance` | 29 | Open. All are `"before each"` fixture-setup hooks (`AccessManager`, `Crosschain*`, `ERC2771Context`, `GovernorProposalGuardian`, `RelayedCall`, `TransparentUpgradeableProxy`) — the fixture calls `setBalance` to fund an account, the RPC errors, and the whole hook plus everything inside it fails. Same shape as the rows above: add a `SetBalance` stub next to `SetCode`. Real (non-stub) `hardhat_set*` support is tracked in **#323**. |

`eth_getProof` (6) moved out of this table — it's real, standard functionality (EIP-1186), not a
Hardhat-only convenience, so it's tracked with the trie/stateRoot item above instead.

#### Making `hardhat_mine` real

Registering the stub only silences the RPC error; `hardhat_mine`, `evm_mine`, `evm_increaseTime`, and
`evm_setNextBlockTimestamp` all just log and return today
([`hardhat_helpers.go`](../gateway/testimpl/hardhat_helpers.go)) — none of them change chain state.
The tests tagged `hardhat-time-rpc` (174 as of this writing, up from 54 shortly after #317 landed —
check the histogram per [Reading the current numbers](#reading-the-current-numbers) for the live
count; mostly `AccessManager` scheduled/delay/expiry tests using `time.increase()` + `mine()`) are
exactly the tests that got past the missing-method error and are now failing on the stub's lack of
effect instead — confirmed by #317's own commit history, which notes it "restore[d] OZ known failures
for tests that still need `evm_increaseTime` / `setNextBlockTimestamp` / `mine`."

The same stub gap surfaces separately as `hardhat-block-rpc` (203 as of this writing) wherever the
missing effect is observed through block *height* rather than elapsed time — Hardhat's `mineUpTo`
helper (built on repeated `hardhat_mine` calls) and ERC-6372 `clock()` in blockNumber mode are the two
shapes seen so far. It's tagged apart from `hardhat-time-rpc` because the fix path differs: cutting
real blocks via `CutBlock` (below) is necessary for both, but the block-number tests additionally need
`block.number` itself to stop being pinned at `0` (the `fixed-block-number` / endorser-agreement
problem above) — `time.increase()` tests don't have that second dependency.

[`hyperledger/fabric-x-sdk#51`](https://github.com/hyperledger/fabric-x-sdk/issues/51) proposes a
`CutBlock` method on `fabrictest.Network` so the test harness can cut an empty block without a real
transaction — today the batching loop treats an empty batch as a no-op, so there is *no* way to
advance block height on demand at all. That's necessary infrastructure, but landing it alone would
**not** be sufficient to clear either tag:

- `HardhatAPI.Mine` / `EvmAPI.Mine` still need to be rewired to actually call the new `CutBlock` once
  it exists — the stub doesn't wire itself up.
- Block timestamp comes from a single real `time.Now()` picked once per transaction by the gateway
  (see the `fixed-timestamp` fix above) — there's no virtual-clock/offset mechanism anywhere, so
  `evm_increaseTime` / `evm_setNextBlockTimestamp` have nothing to write to. Most `hardhat-time-rpc`
  tests jump the clock by days via `time.increase()`, which cutting empty blocks in real time won't
  reproduce.
- `block.number` inside contract execution is separately hardcoded regardless of chain height (the
  `fixed-block-number` row above, same root cause) — untouched by `CutBlock`.

So: `CutBlock` is necessary, not sufficient. The full fix is `CutBlock` (fabric-x-sdk) *plus* wiring
the mine stubs to it *plus* a virtual clock for the timestamp side.

### Done: production `eth_estimateGas`

| Symptom | Cause | `cmd/baseline` tag | # (was) | Status |
| --- | --- | --- | --- | --- |
| every test asserting `< 30k` gas | ERC-165 `supportsInterface.estimateGas().to.lte(30000)` failed because `EstimateGas` returned the constant `10_000_000` | `gas-stub` | 0 (36) | **Done** — [#321](https://github.com/hyperledger/fabric-x-evm/pull/321) threaded real simulation gas through the endorser `Call` path and gateway `EstimateGas`; [#342](https://github.com/hyperledger/fabric-x-evm/pull/342) then made the estimate a limit *verified by re-simulation* (pre-refund maximum plus an SSTORE sentry buffer, doubling until it works). `gas-stub` no longer appears in `oz_known_failures.json`. |

Was production code, not test-only — it made `eth_estimateGas` *more* correct rather than
special-casing tests. See
[`COMPATIBILITY.md`](COMPATIBILITY.md#gas-and-fees) for what the returned value now means.

### Hard limitation

| Symptom | Cause | `cmd/baseline` tag | # | Why hard |
| --- | --- | --- | --- | --- |
| `max code size exceeded` | the `hardhat-exposed` mocks (the `$`-prefixed wrappers) are oversized and rely on OZ's `allowUnlimitedContractSize: true` ([`hardhat.config.js:104`](../testdata/openzeppelin-contracts/hardhat.config.js#L104)). fabricevm enforces EIP-170. | `max-code-size` | 212 | see below — not a clean knob, and the closest one isn't a quick win either. |

All 212 are the whole `Packing.test.js` pack/extract/replace permutation matrix (see below) —
confirming this is really one root cause, not 212 independent ones.

**Verified against the actual vendored go-ethereum (`v1.17.3`), not assumed:** the check isn't
unconditionally hardcoded. In `core/vm/common.go`, `CheckMaxCodeSize` only enforces the limit when
`rules.IsEIP158` (Spurious Dragon, 2016) is active in chain config — turn that fork off and the check
is skipped entirely, no source patch needed. But `IsEIP158` isn't a narrow "code size" toggle; the
same flag also gates, elsewhere in `core/vm`:

- **contract nonce initialization** — a new contract's nonce is only set to `1` on creation when
  `IsEIP158` is active; without it, contracts get nonce `0`, a much more foundational deviation than
  the one we're trying to remove.
- **empty-account handling** — a zero-value call to a non-existent account is a no-op under EIP158;
  disable it and an empty account gets created instead.
- **CALL gas accounting** — `CallNewAccountGas` charging changes.

So disabling EIP158 to allow bigger contracts means reverting three other pieces of core EVM
semantics we want correct (we're targeting Cancun/Osaka elsewhere) — trading one compatibility gap
for others, likely worse ones. `params.MaxCodeSize` itself is still a plain Go constant with no
per-call override; the only real lever is a chain-config fork flag bundled with far more than code
size. Patching go-ethereum (or reimplementing `CREATE` outside `core/vm` — no smaller a project)
remains the honest answer.

One narrower, genuinely free option for later: this same geth version already defines a next-fork
placeholder, `Amsterdam`, gated the same way (`rules.IsAmsterdam` in `CheckMaxCodeSize`), that raises
the limit to `32768` bytes without touching any of the EIP158 side effects above. Not
released/stable upstream yet, and 33% bigger likely still isn't enough for the `$Packing` mock
specifically (it enumerates every byte-size permutation), but worth revisiting once that fork
stabilizes.

### Needs confirmation (unverified)

| Symptom | Hypothesis | How to confirm |
| --- | --- | --- |
| `pack` / `extract` / `replace` ([`Packing.test.js`](../testdata/openzeppelin-contracts/test/utils/Packing.test.js)) — tagged `max-code-size` in the baseline | every one of the 212 tests fails with `could not decode result data (value="0x", ...)` — an empty return, not a revert reason — consistent with the exposed `$Packing` mock never actually deploying (oversized) rather than deploying and reverting; the `$pack_N_N`/`$extract_N_N`/`$replace_N_N` method names in the error confirm it's this one mock, not something else | re-run just this file with gateway debug logs; look for a code-size error at deploy to confirm the mock genuinely never lands, rather than landing and reverting for an unrelated reason |
| `merkle tree reverts` ([`MerkleProof.test.js`](../testdata/openzeppelin-contracts/test/utils/cryptography/MerkleProof.test.js)) | either code size, or revert-surfacing differs for state-mutating mock calls: the backend's `SendRawTransaction` returns the hash once committed **regardless of `status=0`** ([`test_eth_api.go:177`](../gateway/testimpl/test_eth_api.go#L177)), so a reverting tx doesn't throw at send the way `revertedWithCustomError` / `revertedWithPanic` expect | re-run with logs; check whether the failing assertions are on `view`/`pure` (`eth_call`, should surface) vs state-mutating wrappers |
| `execfailure-dropped-not-committed` (small) | tagged but not yet written up here; likely the same behaviour as **#312** — EVM execution failures (out-of-gas, invalid opcode) dropped rather than committed with `status=0` | confirm against #312, then either fold into that issue or give it its own taxonomy row |
| `estimate-gas-allowance-not-revert` (small) | tests expecting a revert where estimation instead reports `gas required exceeds allowance` — the ceiling probe cannot distinguish "reverts for a business reason with no data" from "needs more gas". [#342](https://github.com/hyperledger/fabric-x-evm/pull/342) reworked this search, so recheck before investigating | re-run these tests against current `main` and see whether the bucket survives |

The two rows above are recorded rather than omitted: under the classification invariant in
[`TESTING_STRATEGY.md`](TESTING_STRATEGY.md#baseline), a cause that is neither a documented
deviation nor a tracked defect is itself the finding.

---

## Fix backlog (ROI-ranked)

What is worth fixing next. The **effort estimate and the ordering are judgment** and live here;
exact test counts deliberately do not, because they move on every fix and every submodule bump. The
**Cause bucket** column is the join: look each one up in the current histogram
(see [Reading the current numbers](#reading-the-current-numbers)) to size the work today rather than
trusting a number written down months ago. Items below the line are **done**, kept for history and
citations — those counts are historical records of what a specific PR achieved, so they do not rot.

| Effort | Item | Impact | Cause bucket |
| --- | --- | --- | --- |
| **Investigate** | confirm max-code-size for Packing, then decide on the EIP-170 fix | **Large** | `max-code-size` |
| **Hard** | pin an endorser-agreed value for `block.number` / randomness before endorsement — protocol-level design work, not a quick patch; `block.timestamp` already escaped this bucket via #317 (see taxonomy above) | **Large** | `hardhat-block-rpc`, `fixed-block-number` |
| **Medium** | wire `hardhat_mine`/`evm_mine` to a real block-cutting primitive once [fabric-x-sdk#51](https://github.com/hyperledger/fabric-x-sdk/issues/51)'s `CutBlock` lands, *plus* a virtual clock for `evm_increaseTime`/`evm_setNextBlockTimestamp` — see [Making `hardhat_mine` real](#making-hardhat_mine-real) | **Large** | `hardhat-time-rpc`, `fixed-timestamp` |
| **Investigate** | bare `execution reverted` — a raw-message bucket, so genuinely mixed causes; needs sampling before any bulk tag | **Large** | `execution reverted` |
| **Investigate** | `concurrent-tx-race` — fixtures firing concurrent txs via `Promise.all` without waiting for confirmation, racing the endorsement-time read snapshot; needs dependency-aware tx scheduling upstream, not a fabric-x-evm-side fix | **Medium** | `concurrent-tx-race` |
| **Easy** | `hardhat_setBalance` stub in test RPC — cascades through fixture `"before each"` hooks | **Medium** | `hardhat_setBalance` |
| **Medium** | make the RWS→trie translation reproduce canonical state roots, enable `withTrie` in production, then implement `eth_getProof` — real functionality (EIP-1186), not test-only | **Small here, large elsewhere** | `eth_getProof` |
| **Small** | `hardhat_setStorageAt` stub | **Small** | `hardhat_setStorageAt` |
| **Investigate** | `transaction already pending` (`ERC20Votes` same-block checkpoint tests) — nonce/pending-tx handling differs from Hardhat when two txs are submitted back-to-back | **Small** | `transaction already pending` |
| **Hard** | EIP-170 code-size limit for oversized exposed mocks (needs geth patch) — the real fix behind the Packing entries above, once confirmed | — | — |
| — | *(done)* pre-fund test accounts via balance priming — [#296](https://github.com/hyperledger/fabric-x-evm/pull/296) | 79 (93→14) | `insufficient-funds` |
| — | *(done)* real `eth_estimateGas` — [#321](https://github.com/hyperledger/fabric-x-evm/pull/321), refined into a re-simulation-verified limit by [#342](https://github.com/hyperledger/fabric-x-evm/pull/342) | 36 (36→0) | `gas-stub` |
| — | *(done)* gateway-supplied `block.timestamp` — [#317](https://github.com/hyperledger/fabric-x-evm/pull/317) | 26+ (64+→38) | `fixed-timestamp` |
| — | *(done)* `eth_signTypedData_v4` + `personal_sign` in test RPC — [#298](https://github.com/hyperledger/fabric-x-evm/pull/298) | 168 (168→0) | `eth_signTypedData_v4`, `personal_sign` |
| — | *(done)* `hardhat_mine` stub — [#271](https://github.com/hyperledger/fabric-x-evm/pull/271) (method-not-found only; real behavior still open, see above) | 125 (125→0) | `hardhat_mine` |
| — | *(done)* `hardhat_impersonateAccount` stub — [#280](https://github.com/hyperledger/fabric-x-evm/pull/280) | 29 (29→0) | `hardhat_impersonateAccount` |
| — | *(done)* `evm_setAutomine` stub — [#291](https://github.com/hyperledger/fabric-x-evm/pull/291) | 6 (6→0) | `evm_setAutomine` |

---

## CI

Wired: the `oz-hardhat-compat` job in
[`.github/workflows/tests.yml`](../.github/workflows/tests.yml) runs the compatible set on every PR
and gates on `cmd/baseline check`, with a bot posting the report as a PR comment. That file is the
authority on the wiring; this document does not restate it.

---

## Open questions

- ~~Per-suite vs per-file granularity for the runner/merge step.~~ Resolved: per top-level `test/`
  directory (8 groups) — coarse enough to keep the number of Hardhat invocations small, fine enough
  that one directory's load-time crash doesn't zero out the other seven.
- ~~Also emit JUnit XML so GitHub's native test-report UI shows per-test history?~~ Decided against:
  would mean adding `mocha-junit-reporter` as a dependency purely to reach a format only our own tool
  would read, and a rendering GitHub Action needs `checks:write`/`pull-requests:write` with real
  caveats for fork-originated PRs. The tool's own exit code is the gate; its report goes to
  `$GITHUB_STEP_SUMMARY` instead (built in, no extra permissions).
- The excluded account-abstraction suites: permanently out, or revisit if `hardhat_setCode` is ever
  made real (no-op stub today)?
- The bare `execution reverted` bucket: worth splitting into real causes, or an acceptable permanent
  long tail? Tagging coverage is now good (see
  [Reading the current numbers](#reading-the-current-numbers)), so this raw-message bucket is the
  main thing standing between us and the classification invariant.
- The share of the baseline marked `flaky`: quarantined entries never gate, so a growing share means
  a shrinking amount of the suite actually asserts anything. Is there a level at which that becomes
  the priority over any individual fix?
- **New, found while investigating the OZ bump's regressions:** `evm_snapshot`/`evm_revert` failed
  once with `failed to revert Store to block N: snapshot for block N not found`
  (`Arrays.test.js`, `"before each" hook for "sort reversed array"`). `NewTestNode` configures
  `HistorySize: 128` (`gateway/app/testnode.go`) — a rolling window, not unlimited — so a snapshot
  legitimately ages out if more than 128 blocks pass before the matching revert. Not about the OZ
  bump's content; a latent capacity limit that gets more likely to trigger simply because the suite
  keeps growing within one long-lived testnode session. Worth deciding whether 128 still has enough
  headroom, or whether it should scale with suite size.
