# Contributing

Thanks for considering to contribute. This is a practical guide to getting a change in — building, the
test suites, and how PRs get reviewed. For what the project *is*, see the [README](README.md); for
what it asserts about itself and why, see [`docs/TESTING_STRATEGY.md`](docs/TESTING_STRATEGY.md).
Please read the [Code of Conduct](CODE_OF_CONDUCT.md) before participating — maintainers are listed
in [`MAINTAINERS.md`](MAINTAINERS.md).

```shell
make help          # every target, grouped and described
make build         # bin/fxevm
make checks        # gofmt, vet, staticcheck, license headers
make unit-tests    # the quick local loop — same as CI's Quick Tests
```

You'll need Go (see `go.mod` for the version) and Docker or Podman for anything that touches a real
Fabric-X network — see the README's [Building and running from
source](README.md#building-and-running-from-source) for the full setup.

**Run `make checks` before pushing.** It is the first thing CI runs, and formatting or a missing
license header will fail your PR before any test does.

---

## Got a question, found a bug, or just want to say how you're using this?

Open a GitHub issue — that's what we mainly track and follow up on.

We also sometimes hang out on
[Discord](https://discord.com/channels/905194001349627914/1550488625576415232)
for informal discussion, but might not be active there all the time. 

A good issue includes:

- A clear, specific title.
- What you ran or did, and what happened vs. what you expected.
- Enough to reproduce it — a test case, a script, or exact steps.
- The version/commit and environment, in case of a bug.

## Finding something to work on

Issues tagged [`good-first-issue`](https://github.com/hyperledger/fabric-x-evm/labels/good-first-issue)
are a smaller, self-contained place to start. Issues with the status "Ready" should be ready to work on.
Comment on the issue to get assigned, so effort doesn't collide with work already in flight.

Nothing open fits what you want to do? Open an issue and describe it first, unless the change is
genuinely trivial. Or ask a maintainer if anything is ready to implement.

---

## The test suites

Everything is a `make` target; `make help` lists them. What matters is knowing which suite owns what,
because that tells you where a failure comes from:

| Suite                    | Owns                                    | Command                                             |
| ------------------------ | --------------------------------------- | --------------------------------------------------- |
| Unit + short integration | components and their contracts          | `make unit-tests`                                   |
| Ethereum conformance     | EVM correctness vs the spec             | `make eth-tests`                                    |
| OpenZeppelin / Hardhat   | real contracts and tooling over our RPC | `make hardhat-tests`                                |
| Integration              | the Fabric-X pipeline end to end        | `make test-local`, `make test-x`, `make test-fablo` |
| Perf                     | throughput under load (short version)   | `make perf-smoke`                                   |

Note that the integration and perf tests require a running blockchain. Examples:

```shell
make init-x # only needed once

make start-x test-x stop-x
make start-x perf-smoke stop-x

make start-fablo test-fablo stop-fablo
```

The conformance and OpenZeppelin suites need assets that are fetched or built on first use — fixtures
(~400 MB, cached) and OZ's `node_modules` plus compiled contracts. So it's possible that the first
run of either is slow.

### Narrowing a run

The OpenZeppelin suite is the one you will most often want to narrow:

```shell
make hardhat-tests FILE=test/token/ERC20/ERC20.test.js
make hardhat-tests GREP='ERC20 _mint'
make hardhat-tests PORT=8600     # run alongside one already in progress
```

A narrowed run **skips the baseline diff** — a partial run cannot tell a test that did not fail from
one that did not run.

---

## My PR went red on OpenZeppelin or conformance — now what

**CI is expected to be green.** Take a red job seriously.

For the other three suites (unit, integration, perf) that's the whole story: a failure means
something broke — fix it. OpenZeppelin and Ethereum conformance are different: they do not pass
100%, and are not expected to. Each keeps a checked-in list of tests known to fail
(`testdata/oz_known_failures.json`, `testdata/eth_known_failures.json`), and CI diffs the actual run
against that list. Tests known to be nondeterministic are marked *flaky* in the same list and are
quarantined, so they never gate either.

**Only one thing fails the build: a test that failed and is not accounted for in the list.** That is
what makes green the normal state — the known failures and the known flakes are already absorbed, so
a red job is a real signal rather than noise to re-run away.

So work through it in this order:

### 1. Is it a real regression?

Your change broke something that used to pass. Fix the change.

### 2. Is the new failure expected?

Sometimes a change legitimately makes a test start failing — you removed a stub, or you fixed a
"beforeEach" which now causes new tests to run and fail for a new reason. Reconcile the baseline
and commit it:

```shell
go run ./cmd/baseline check  --suite oz-hardhat   # look first
go run ./cmd/baseline update --suite oz-hardhat   # then reconcile
```

Always `check` before `update`. `update` will happily accept a genuine regression as expected.

### 3. Is it genuinely nondeterministic?

If a test cannot be relied on either way, quarantine it so it never gates:

```shell
go run ./cmd/baseline tag --suite oz-hardhat --match '<substring>' --flaky
```

When the flake is a *race* that can hit any of many similar tests, do not add one entry per victim —
that is whack-a-mole, and the entries you haven't hit yet look identical to the ones you have until
one regresses on someone else's PR. Instead give a single entry `idPattern` and `messagePattern`
(Go RE2 regexps) instead of a literal `id`. Both patterns are required together, and the entry must
be `flaky`; `check` refuses to run against a baseline that breaks either rule. See the
`Governor-family before-each-hook dropped-tx race` entry in `testdata/oz_known_failures.json`.

Use quarantine sparingly — it is an admission that a test tells us nothing.

### 4. Stale entries — clean up, don't panic

A listed test that stopped failing shows up as stale. It does **not** fail the build, precisely
because it is ambiguous: it may have been fixed, or it may simply not have run. Clear them with
`update` when convenient, and if one of your own fixes caused it, say so in the commit.
**The list shrinking is how progress is measured on this project** — it just isn't enforced.

Full details of the tool: [`cmd/baseline/README.md`](cmd/baseline/README.md).

---

## Picking up an OpenZeppelin failure

One specific case of [finding something to work on](#finding-something-to-work-on): most entries in
the known-failure lists carry a `cause`, and the causes with the most entries behind them are the
highest-leverage things to fix. `cmd/baseline check --json` prints the histogram; the deviations
those causes correspond to are described in [`docs/COMPATIBILITY.md`](docs/COMPATIBILITY.md). If one
looks interesting, open an issue (or comment on an existing one) before starting.

Once you've got a cause to work from:

1. Find which tests it covers — search `testdata/oz_known_failures.json` for that cause.
2. Reproduce just those: `make hardhat-tests GREP='<something from the test title>'`.
3. Fix, then re-run the full suite and confirm the entries turn stale.
4. `go run ./cmd/baseline update --suite oz-hardhat`, commit the shrunken baseline with your fix.

Tagging coverage is generally good, but a residual of entries still carry **no cause at all**.
Classifying those is genuinely useful work and needs no deep knowledge of the codebase — the goal is
that every cause is either a documented deviation (with a reference in
[`docs/COMPATIBILITY.md`](docs/COMPATIBILITY.md)) or a tracked defect (with an issue).

---

## Where to add a test

> Behaviour is tested at the lowest level that can observe it. Deployment topologies are a wiring
> concern: tested once per topology, deliberately shallow.

In practice: do not add EVM-semantics or contract-behaviour tests by hand — the conformance corpus
and the OpenZeppelin suite already cover those far better than we can. Hand-written Go earns its
place for what is unique to running an EVM on Fabric-X. See
[`docs/TESTING_STRATEGY.md`](docs/TESTING_STRATEGY.md#levels).

---

## Commits and PRs

All commits require a **Developer Certificate of Origin sign-off** — it certifies you have the right
to submit the change under the project's license:

```shell
git commit -s -m "your message"
```

Keep the subject line short and imperative. Explain *why* in the body — the diff already shows what.

New Go files need the standard license header, checked by `make checks`:

```go
/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/
```

PRs are reviewed by the maintainers listed in [`MAINTAINERS.md`](MAINTAINERS.md).
