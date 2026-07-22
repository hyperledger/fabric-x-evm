# cmd/baseline

Diffs a compatibility suite's results against a checked-in list of known failures, so CI can gate on
*new* regressions instead of the whole suite passing.

## Why

The OpenZeppelin Hardhat suite (`testdata/openzeppelin-contracts`, run via `fxevm testnode`) is cheap
enough to run on every PR, but not every test in it passes today, and some never will because of
design choices. Without a record of which specific tests are expected to fail, a CI gate on the full
suite either stays permanently red or has no teeth at all.

We're already skipping known-failing tests via hardcoded lists `testdata/eth_tests.skip`/`.slow`,
with flat file-path lists checked directly in
[`integration/ethereum_test.go`](../../integration/ethereum_test.go) - inspired by `go-ethereum`.
`cmd/baseline` generalizes that same idiom to per-test granularity (not whole files) with an actual
diff: new unlisted failure → regression; listed test that now passes → stale entry, remove it —
instead of a list someone has to remember to prune by hand. It makes the approach consistent and
measurable.

**Currently wired up for OpenZeppelin only, not yet used anywhere in CI.** The `--format` flag
exists so a `go test -json` adapter can be added later for `TestEthereumTests`, replacing
`eth_tests.skip`/`.slow` with the same mechanism at per-subtest granularity — not built yet, but the
reason the result model and CLI are format-agnostic rather than Mocha-specific.

## After bumping `testdata/openzeppelin-contracts`

Not a special case — the same loop as any other run, just with more worth actually reading before you
reconcile:

1. **Regenerate results** against the new submodule commit — see [Generate results](#generate-results).
2. **`check` first, before `update`.** Don't skip straight to reconciling:
   - **Regressions** — failing tests not yet in the baseline. Could be a genuine new incompatibility,
     or just upstream renaming/rewording an already-known-failing test under a new ID. Worth a skim,
     especially if the count is large or unexpected.
   - **Stale entries** — baseline entries no longer failing. Either the underlying issue got fixed for
     real, or upstream renamed/removed the test. Either way, safe to drop.
3. **`update`** to reconcile: drops the stale entries, adds the regressions (auto-tagged where the
   message matches a known signature, blank otherwise).
4. If the bump introduced a fresh batch of an already-understood symptom (e.g. more tests hitting an
   RPC method we haven't implemented), bulk-tag them with `tag` instead of hand-editing dozens of
   entries.

One thing this tool *can't* see: whether the compatible-set **exclusions** themselves are still right
(new test directories, files that now need the `hardhat-predeploy` plugin, etc.) — that's decided in
`scripts/run_hardhat_test.sh`, not here.

## Generate results

Currently supports Mocha's JSON reporter (`--format mocha-json`, the default).

```shell
cd testdata/openzeppelin-contracts
HARDHAT_JSON_OUTPUT=../oz-hardhat-results/erc20.json npx hardhat test test/token/ERC20/ERC20.test.js \
  --config ../hardhat.wrapper.config.js --network fabricevm
```

`HARDHAT_JSON_OUTPUT` switches to the combined reporter
([`testdata/mocha-spec-and-json-reporter.js`](../../testdata/mocha-spec-and-json-reporter.js)):
the usual pass/fail console view still prints live, and the JSON lands at the given path —
one run gives both, instead of picking one or the other.

## Check (the CI gate)

```shell
go run ./cmd/baseline check \
  --suite oz-hardhat \
  --baseline testdata/oz_known_failures.json \
  --results 'testdata/oz-hardhat-results/*.json'
```

Prints a summary and exits non-zero if anything regressed:
- a failure that isn't in the baseline (a real regression), or
- a baseline entry that's no longer failing (stale — remove it).

`--results` is a glob (matched with `filepath.Glob`, so quote it against your shell expanding it
early) that merges several files — useful since a Mocha file that crashes at load time can zero out
the whole report for that invocation, so running per-file/per-directory and merging is safer than one
giant run. `testdata/oz-hardhat-results/` is exactly what `scripts/run_hardhat_test.sh --full` writes
one file per top-level `test/` directory into, so the command above works as-is after a real run.

Sample output, an empty baseline against a real run of `ERC20Permit.test.js` (regressions, since
nothing is listed yet):

```
$ go run ./cmd/baseline check --suite oz-hardhat --baseline /tmp/empty-baseline.json --results testdata/oz-hardhat-results/permit-example.json
# Baseline check: oz-hardhat

2 passed, 4 failed, 0 skipped (6 total, 33.3% passing)

## Regressions (4)

- `ERC20Permit permit accepts owner signature`: the method eth_signTypedData_v4 does not exist/is not available
- `ERC20Permit permit rejects reused signature`: the method eth_signTypedData_v4 does not exist/is not available
- `ERC20Permit permit rejects other signature`: the method eth_signTypedData_v4 does not exist/is not available
- `ERC20Permit permit rejects expired permit`: the method eth_signTypedData_v4 does not exist/is not available

$ echo $?
1
```

Same run again after the baseline is seeded (see `update` below) — clean, with the histogram
grouping the four by their shared, auto-tagged cause (see `inferCause` in `baseline.go`: a message
naming its own missing RPC method gets tagged without a human needing to look):

```
$ go run ./cmd/baseline check --suite oz-hardhat --baseline testdata/oz_known_failures.json --results testdata/oz-hardhat-results/permit-example.json
# Baseline check: oz-hardhat

2 passed, 4 failed, 0 skipped (6 total, 33.3% passing)

## Expected failures by cause (4)

- eth_signTypedData_v4: 4

$ echo $?
0
```

A run spanning multiple suites (multiple `--results` files, or one glob matching several) adds a
`## By suite` breakdown, sorted worst-first, so you can see at a glance where compatibility still
needs the most work — and watch that shrink over time as fixes land.

## Update (seed or reconcile the baseline)

```shell
go run ./cmd/baseline update \
  --suite oz-hardhat \
  --baseline testdata/oz_known_failures.json \
  --results 'testdata/oz-hardhat-results/*.json'
```

Rewrites the baseline file: drops stale entries, adds an entry for every new failure — auto-tagging
`cause` for the high-confidence signatures `inferCause` recognizes, leaving the rest blank for a
human to tag opportunistically (or bulk-tag with `tag`, below). Safe to run any time — for the
initial seed, or after a dependency bump shifts what fails. No hand-curation needed to land it.

```
$ go run ./cmd/baseline update --suite oz-hardhat --baseline testdata/oz_known_failures.json --results testdata/oz-hardhat-results/permit-example.json
testdata/oz_known_failures.json: 0 entries removed, 4 entries added, 4 total now

$ cat testdata/oz_known_failures.json
[
  {
    "id": "ERC20Permit permit accepts owner signature",
    "cause": "eth_signTypedData_v4"
  },
  {
    "id": "ERC20Permit permit rejects expired permit",
    "cause": "eth_signTypedData_v4"
  },
  {
    "id": "ERC20Permit permit rejects other signature",
    "cause": "eth_signTypedData_v4"
  },
  {
    "id": "ERC20Permit permit rejects reused signature",
    "cause": "eth_signTypedData_v4"
  }
]
```

## Tag (bulk-assign a cause)

```shell
go run ./cmd/baseline tag \
  --baseline testdata/oz_known_failures.json \
  --results 'testdata/oz-hardhat-results/*.json' \
  --match "Packing" \
  --cause "max-code-size"
```

Sets `cause` (and optionally `--note`) on every still-failing entry whose ID or current message
contains `--match` — for a symptom that's too broad or too varied in wording for `inferCause` to
recognize automatically, but that a human can look at once and confidently label in bulk (e.g. a
whole describe block, or a message fragment shared across many tests). Entries that already have a
cause are left alone unless `--force` is passed — a broad match shouldn't silently overwrite a more
specific tag an earlier, more targeted pass already got right.
