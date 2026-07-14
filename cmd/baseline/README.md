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

## Generate results

Currently supports Mocha's JSON reporter (`--format mocha-json`, the default).

```shell
cd testdata/openzeppelin-contracts
HARDHAT_REPORTER=json npx hardhat test test/token/ERC20/ERC20.test.js \
  --config ../hardhat.wrapper.config.js --network fabricevm \
  > /tmp/oz-results.json
```

## Check (the CI gate)

```shell
go run ./cmd/baseline check \
  --suite oz-hardhat \
  --baseline testdata/oz_known_failures.json \
  --results /tmp/oz-results.json
```

Prints a summary and exits non-zero if anything regressed:
- a failure that isn't in the baseline (a real regression), or
- a baseline entry that's no longer failing (stale — remove it).

`--results` accepts a glob (`--results '/tmp/oz-results/*.json'`) to merge several files — useful
since a Mocha file that crashes at load time can zero out the whole report for that invocation, so
running per-file/per-directory and merging is safer than one giant run.

Sample output, an empty baseline against a real run of `ERC20Permit.test.js` (regressions, since
nothing is listed yet):

```
$ go run ./cmd/baseline check --suite oz-hardhat --baseline /tmp/empty.json --results /tmp/permit-results.json
# Baseline check: oz-hardhat

2 passed, 4 failed, 0 skipped (6 total)

## Regressions (4)

- `ERC20Permit permit accepts owner signature`: the method eth_signTypedData_v4 does not exist/is not available
- `ERC20Permit permit rejects reused signature`: the method eth_signTypedData_v4 does not exist/is not available
- `ERC20Permit permit rejects other signature`: the method eth_signTypedData_v4 does not exist/is not available
- `ERC20Permit permit rejects expired permit`: the method eth_signTypedData_v4 does not exist/is not available

$ echo $?
1
```

Same run again after the baseline is seeded (see `update` below) — clean, with the histogram
grouping the four by their shared cause:

```
$ go run ./cmd/baseline check --suite oz-hardhat --baseline testdata/oz_known_failures.json --results /tmp/permit-results.json
# Baseline check: oz-hardhat

2 passed, 4 failed, 0 skipped (6 total)

## Expected failures by cause (4)

- the method eth_signTypedData_v4 does not exist/is not available: 4

$ echo $?
0
```

## Update (seed or reconcile the baseline)

```shell
go run ./cmd/baseline update \
  --suite oz-hardhat \
  --baseline testdata/oz_known_failures.json \
  --results /tmp/oz-results.json
```

Rewrites the baseline file: drops stale entries, adds a blank-`cause` entry for every new failure.
Safe to run any time — for the initial seed, or after a dependency bump shifts what fails. No
hand-curation needed to land it; tag `cause` on entries later, opportunistically.

```
$ go run ./cmd/baseline update --suite oz-hardhat --baseline testdata/oz_known_failures.json --results /tmp/permit-results.json
testdata/oz_known_failures.json: 0 entries removed, 4 entries added, 4 total now

$ cat testdata/oz_known_failures.json
[
  {
    "id": "ERC20Permit permit accepts owner signature"
  },
  {
    "id": "ERC20Permit permit rejects expired permit"
  },
  {
    "id": "ERC20Permit permit rejects other signature"
  },
  {
    "id": "ERC20Permit permit rejects reused signature"
  }
]
```
