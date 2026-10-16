# OZ fix backlog → GitHub issues (drafts)

Local working doc, not committed. Drafts for the [`OZ_COMPAT.md`](OZ_COMPAT.md) fix backlog, checked
against the real issue tracker first — see "Already covered" below before filing anything, several
items already exist or overlap with existing work.

Epics: **#7** `[EPIC] Ethereum Compatibility` (production-relevant) · **#248** `[EPIC] Hardhat
compatibility` (test-RPC-only, no production path). #248's only current sub-issue is **#156**
"Complete Hardhat tests support" — vague, links to an old PR thread, no itemization. The filed issues
below are effectively the concrete breakdown of it; worth considering closing #156 in favor of them
(not done here — that's a call for whoever files the rest, not something to decide in a doc draft).

**Dependency note:** `cmd/baseline` and the full-suite runner (`scripts/run_hardhat_test.sh --full`,
`testdata/oz_known_failures.json`) aren't merged yet — tracked by **#249** (open). Two consequences
for the issues below: every test count here ("163 tests, last full run", etc.) comes from a local,
pre-merge run of that tooling, not something reproducible from `main` yet; and any "re-run
`cmd/baseline check`" validation step assumes #249 has landed first.

**Filed** (removed from the backlog below): **#250** (`eth_signTypedData_v4` + `personal_sign`),
**#251** (`hardhat_mine` stub), **#252** (`hardhat_impersonateAccount` stub), **#253**
(`evm_setAutomine` stub), **#254** (pre-fund test accounts) — all children of #248. **#255**
(`eth_getProof`, EIP-1186), **#257** (`gasUsed` in receipts) — children of #7.

---

## Already covered — do not duplicate

| Backlog item | Existing issue | Notes |
| --- | --- | --- |
| Real `eth_estimateGas` | **#241** (open) | Body already matches our research almost verbatim (`ApplyMessage`'s discarded `UsedGas`, `ProcessCall`'s proto gap, `CallContract` signature). The one thing #241 doesn't have yet: the correction that `CallContract` isn't actually pinned to `ethereum.ContractCaller` (nothing asserts that conformance) and the blast radius is just 3 call sites — worth adding as a comment if you want the issue to reflect that. |
| Storage root / trie correctness | **#85** (open) | Originally scoped narrowly ("wire `GetStorageRoot` to the trie calc"), but the 2026-07-20 comment on the issue redefines "done" as `go test -run ^TestEthereumTests$ ./integration -args -verify_root=true` passing. Per `docs/COMPATIBILITY.md`'s "EVM execution differences" section, that test diverges for exactly two documented reasons: (a) `GetStorageRoot`'s zero-hash stub disables the EIP-7610 CREATE/CREATE2 collision guard — **this is #85 itself** — and (b) SELFDESTRUCT is only partially implemented (EIP-6780: no balance transfer to beneficiary, no code/storage clearing, not fork-aware). So #85 alone won't make `-verify_root=true` pass; (b) has no existing issue and isn't filed yet — deliberately not filed here without confirming first, since it's a real EVM-semantics gap, not trie plumbing. |
| (historical) correct post-commit state roots | **#82** (closed) | Same goal, closed — predecessor to #85 above, not reopened. |
| Broad "gas handling for tests" | **#43** (open → closing) | Vague, predated #241 and the OZ/`COMPATIBILITY.md` work. Its scope is now covered by #241 (`eth_estimateGas`) plus **#257** (`gasUsed` in receipts, filed as a child of #7) and documented as a deliberate deferral in `COMPATIBILITY.md`'s Gas and fees section — closing #43 as done in favor of those. |

---

## New issues — investigation first (see "not clearly scoped" section for why these are framed this way)

### Child of #248

**1. [Investigate] Confirm why `Packing.test.js`'s exposed mock fails**
212 tests — the entire `pack`/`extract`/`replace` permutation matrix, the single largest bucket in
the whole OZ suite — fail with `could not decode result data (value="0x", ...)`. Hypothesis: the
exposed `$Packing` mock is oversized (relies on OZ's own `allowUnlimitedContractSize: true`, a
Hardhat-dev-only setting) and never actually deploys under fabricevm's real EIP-170 enforcement; an
empty return from a call to a codeless address produces exactly this symptom. **Not yet confirmed**
— the observed error is an empty return, not an explicit "max code size exceeded" at deploy.
*Deliverable:* re-run just this file with gateway debug logs, confirm or rule out a code-size
rejection at deploy time, report back. This determines whether relaxing EIP-170 is worth scoping as
real follow-up work (see "not clearly scoped" below) or whether this is a permanent, accepted gap
(OZ's own mock already relies on non-standard Hardhat-only behavior).

**2. [Investigate] Sample and classify the 69 bare `execution reverted` failures**
Mostly `AccessManager`. Unlike every other bucket in the baseline, this one is a raw-message grouping
with no assigned cause — genuinely could be several different real issues, or a revert-surfacing
quirk. **Check whether this is a regression of #99** (closed — "Preserve geth failure model for
`eth_sendRawTransaction`") before assuming a new bug; if #99's fix was meant to cover this exact
class of behavior, that changes the framing significantly. *Deliverable:* sample a handful (starting
with `AccessManager`, the majority), determine actual root cause(s), tag with an existing cause or
spin off focused follow-ups.

**3. [Investigate] Feasible write path for `hardhat_setStorageAt`**
Checked whether this is a trivial registration gap (like the filed `hardhat_mine`/
`hardhat_impersonateAccount` stubs) — it isn't. `hardhat_setCode` is the only precedent and it's
itself an unfulfilled stub (`hardhat_helpers.go:42-48`: logs, returns `true, nil`, never touches
state — comment admits "In a full implementation, this would modify..."). The `Backend` interface
only exposes reads (`gateway/api/eth.go:30-57`); the one real write path is via committed Fabric
blocks through `applyStorageWrite` (`gateway/storage/trie/store.go:123-187`), reachable only via
endorse→order→commit — a real tx, not a direct poke. A no-op stub here is riskier than the automine
one: tests using `setStorageAt` typically read the slot back immediately after, so "method found,
does nothing" would silently fail the next assertion instead of surfacing as a clean
method-not-found. Checked `endorser/testimpl/balance_priming_executor.go` as a possible
direct-write-bypass precedent — it doesn't generalize: it's a `StateDB` wrapper that intercepts
*reads* for one hardcoded contract/mapping-slot config, not a generic write path.
*Deliverable:* decide whether this needs a real (consensus-going) write, or a test-only bypass write
API outside the normal tx path (analogous to geth's own `debug_setStorageAt` — direct state mutation,
no block); scope the actual fix once that's decided, rather than assuming it's cheap. 3 tests, last
full run — low count is why this hasn't been worth more than a stub attempt so far, but worth getting
right rather than shipping a silent no-op.

### Child of #7

**4. [Investigate] `MerkleProof.test.js` revert assertions — possible regression of #99**
Hypothesis: the backend's `SendRawTransaction` returns the tx hash once committed regardless of
`status=0` (`gateway/testimpl/test_eth_api.go:177`), so a reverting state-mutating call doesn't throw
at send the way `revertedWithCustomError`/`revertedWithPanic` expect. **This looks like it could be
exactly what #99 (closed) was supposed to fix** — confirm whether it's a regression before treating
it as new. *Deliverable:* re-run with logs; check whether failures are on `view`/`pure` calls
(`eth_call`, should already surface correctly) vs. state-mutating wrappers (the hypothesized gap);
resolve the #99-regression question first, since it changes urgency and where the fix belongs.

---

## Not clearly scoped / not designed yet

**5. [Design needed] Deterministic `block.timestamp`/`block.number`/`PREVRANDAO` for pre-order
endorsement**
`TIMESTAMP` (`1_000_000`), `NUMBER` (`0`), `PREVRANDAO` (zero hash) are hardcoded in
`endorser/execution/executor.go` — not an oversight, an architectural consequence. This project
endorses the Fabric way: multiple independent endorsers each simulate a transaction and must produce
byte-identical read-write sets before the endorsement policy is satisfied (`docs/ARCHITECTURE.md`
step 9), and ordering — which fixes a transaction's actual position in the chain — happens *after*
endorsement (step 11). If each endorser read its own wall clock, randomness, or guessed the next
block number, independent endorsers would compute different values for any contract branching on
them, breaking endorsement.

A real fix means embedding an agreed value into the transaction proposal itself, before endorsement —
protocol-level work, not an `executor.go` patch, and almost certainly touches `fabric-x-sdk`'s
proposal/endorsement flow, not just this repo. **This is squarely why I'd treat it differently from
the other issues here — see recommendation below.**

Real production value, not just an OZ-suite gap: blocks any real contract using time-locks, vesting,
deadlines, or checkpoint-based governance (OZ's own `ERC20Votes`/`Governor` pattern, widely deployed
in real DAOs) from behaving correctly. 76+ OZ tests currently affected (`fixed-timestamp`: 64+,
`fixed-block-number`: 12+ — both undercounts, since many instances manifest as generic assertion
diffs rather than a clean "expected timestamp" message).

**6. (not filed as its own issue) Relaxing/removing the EIP-170 code-size limit**
Contingent entirely on issue #1 above confirming the Packing hypothesis. Even if confirmed, the
mechanism isn't clean: `params.MaxCodeSize` is a plain go-ethereum constant, and the only real lever
(`rules.IsEIP158`) is bundled with contract nonce initialization, empty-account handling, and CALL gas
accounting — disabling it to get bigger contracts reverts three other pieces of core EVM semantics.
Filing this now, before the investigation resolves, risks a stale "maybe fix this" issue nobody acts
on. Recommend folding this into issue #1's own follow-up discussion instead of a standalone issue.

---

## What I'd suggest for the two genuinely open questions

**The "not designed yet" ones (#5, and #6's non-issue) are a different *kind* of thing than #1–#4**,
and I'd treat them differently:

- **#1–#4 (investigations)** are well-bounded even though the *fix* isn't known yet — each has a
  concrete, falsifiable deliverable ("confirm or rule out X," "sample and classify Y"). These are
  fine to file now, as investigation issues, not implementation issues. Don't pre-commit to a fix in
  the issue title/scope; let the investigation's outcome decide whether it spawns a real
  implementation issue, gets folded into an existing cause as "permanent, accepted, documented," or
  turns out to need its own design pass.

- **#5 is not bounded the same way** — we already know the *value* (real, high), but not the
  *shape* of the fix, and the shape genuinely requires cross-repo protocol design (probably
  `fabric-x-sdk`'s proposal/endorsement flow, not just this repo). I'd scope the GitHub issue itself
  narrowly — "produce a design for X," not "implement X" — so it can't rot as an ever-growing,
  never-actioned mega-issue. Given there's an actual `fabric-x-rfcs` repo in this org for exactly
  this kind of cross-cutting protocol question, **it's worth considering whether this belongs there
  first**, with the fabric-x-evm issue tracking "adopt the RFC's design here" as a follow-up once one
  exists — rather than starting directly with an implementation-shaped GitHub issue in this repo.
  Your call on which repo it starts in; flagging the option since I don't know your RFC bar.

- **#6 deliberately isn't a standalone issue** — it's downstream of #1's outcome and has no design
  of its own yet (patch go-ethereum, or a bigger rewrite of `CREATE` — "no smaller a project"). Noted
  as a follow-up inside #1 rather than filed, so it doesn't sit as a phantom "someday" issue.
