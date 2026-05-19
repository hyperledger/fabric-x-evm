# USDC Performance Testing

This directory contains infrastructure for performance testing using real USDC transaction data from Ethereum mainnet.

## Overview

The performance tests replay actual USDC transfers to measure Fabric-EVM throughput and latency under realistic workloads.

The replay harness supports:

- Configurable dataset windowing
- Optional wrap-around replay across the selected window
- Environment-variable control for replay behavior
- Two-layer nonce handling for wrap-around replay (contained in `testimpl` directories)

## Running the Tests

Performance tests are gated behind the `perf` build tag and must be run explicitly:

```bash
go test -tags=perf ./integration/perf/...
```

A convenience target is also available from the repository root:

```bash
make perf-tests
```

## Setup Instructions

**All commands should be run from the repository root directory.**

### Prerequisites

- Go 1.26+
- Python 3.x (for visualization scripts)
- `wget` or `curl` for downloading datasets
- `abigen` for generating contract bindings

The helper script in [`scripts/run_perf_test.sh`](scripts/run_perf_test.sh) uses portable gzip-based filtering, so `zgrep` is not required.

### Step 1: Download the Dataset

Download the token transfer dataset into [`integration/perf/testdata`](integration/perf/testdata/):

```bash
wget https://dataverse.harvard.edu/api/access/datafile/11691882 -O integration/perf/testdata/202001.tsv.gz
```

This downloads the complete January 2020 token transfer dataset to the perf test data directory.

**Note**: The large dataset files ([`integration/perf/testdata/dataset.tsv.gz`](integration/perf/testdata/dataset.tsv.gz), [`integration/perf/testdata/USDC_dataset.json.gz`](integration/perf/testdata/USDC_dataset.json.gz)) are **not committed to the repository**. You must generate them locally following these instructions.

### Step 2: Filter USDC Transactions

Extract only USDC transfers (contract address `0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48`):

```bash
gunzip -c integration/perf/testdata/202001.tsv.gz | grep -iE '(a0b86991c6218b36c1d19d4a2e9eb0ce3606eb48|token_address)' | gzip > integration/perf/testdata/dataset.tsv.gz
```

This creates a filtered dataset (~10MB compressed) containing only USDC transfers in [`integration/perf/testdata/`](integration/perf/testdata/).

### Step 3: Fetch USDC Contract Code

The USDC contract is a proxy, so we need to fetch both the proxy and implementation code from Ethereum mainnet:

```bash
go run -tags=perf ./integration/perf -mode fetch
```

This creates [`integration/perf/testdata/USDC_contract.json`](integration/perf/testdata/USDC_contract.json) with the contract bytecode and metadata.

### Step 4: Generate Go Bindings

Generate the USDC contract bindings required by the test infrastructure.

**Prerequisites:**
```bash
go install github.com/ethereum/go-ethereum/cmd/abigen@latest
```

**Generate bindings from ABI:**
```bash
abigen --abi integration/perf/testdata/USDC_FiatTokenV2_2.abi \
    --pkg contracts --type FiatTokenV2_2 \
    --out integration/contracts/USDC_fiattokenv2_2.gen.go
```

This generates the Go bindings file from the committed ABI. The generated file is excluded from the repository via [`.gitignore`](.gitignore) and must be created locally before running perf tests.

### Step 5: Pre-generate Transactions

Convert the TSV dataset into pre-signed Ethereum transactions:

```bash
go run -tags=perf ./integration/perf -mode generate -input ./integration/perf/testdata/dataset.tsv.gz
```

This creates [`integration/perf/testdata/USDC_dataset.json.gz`](integration/perf/testdata/USDC_dataset.json.gz) (~27MB compressed) containing pre-generated transaction payloads.

### Step 6: Run Performance Tests

Now you can run the performance tests:

```bash
go test -tags=perf -v ./integration/perf/...
```

Or use the helper target:

```bash
make perf-tests
```

The test will:

1. Prime the USDC contract code in the ledger
2. Use balance priming to automatically fund accounts on first access
3. Replay the configured transfer window
4. Optionally wrap around and replay the same window multiple times
5. Output throughput and failure metrics

## Replay Configuration

Replay behavior is controlled via environment variables and applied by `runReplayTest()`.

### Default Behavior

With no environment variables set:

- Uses the first 3000 transfers
- Replays them once
- Stops after a single pass

### Supported environment variables

#### `PERF_REPLAY_WINDOW_SIZE`

Controls how many transfers to use from the dataset.

- `0` = entire dataset (~151k transfers)
- Positive value = first N transfers
- Unset = defaults to `3000`

**Warning**: `PERF_REPLAY_WINDOW_SIZE=0` uses the full dataset and is intended for distributed infrastructure, not local testing. For local runs, use `3000` to `10000`.

Examples:

```bash
# Using go test directly
PERF_REPLAY_WINDOW_SIZE=1000 go test -tags=perf -v ./integration/perf/...
```

```bash
# Using make target
PERF_REPLAY_WINDOW_SIZE=1000 make perf-tests
```

```bash
# Full dataset (for distributed infrastructure)
PERF_REPLAY_WINDOW_SIZE=0 go test -tags=perf -v ./integration/perf/...
```

#### `PERF_REPLAY_WRAP_COUNT`

Controls wrap-around replay count.

- Unset or `1` = single pass
- `2` = replay window twice
- `3` = replay window three times

Examples:

```bash
# Replay window twice using go test
PERF_REPLAY_WRAP_COUNT=2 go test -tags=perf -v ./integration/perf/...
```

```bash
# Replay window twice using make target
PERF_REPLAY_WRAP_COUNT=2 make perf-tests
```

```bash
# Replay 500 transfers 4 times using go test
PERF_REPLAY_WINDOW_SIZE=500 PERF_REPLAY_WRAP_COUNT=4 go test -tags=perf -v ./integration/perf/...
```

```bash
# Replay 500 transfers 4 times using make target
PERF_REPLAY_WINDOW_SIZE=500 PERF_REPLAY_WRAP_COUNT=4 make perf-tests
```

In wrap-around mode, the test dispatches `wrapCount * len(window)` transfers total and logs each restart.

## Nonce Handling During Wrap-Around Replay

Wrap-around replay resubmits the same signed transactions multiple times. Under normal validation, these would be rejected after the first pass due to nonce mismatches.

The test infrastructure handles this with two layers (both in `testimpl` directories):

1. **Gateway-level bypass** - `NonceBypassGateway` wrapper in `gateway/testimpl/`
   - Skips Gateway's pre-flight nonce validation
   - Directly executes and submits transactions
   - Used only in performance tests

2. **Endorser-level nonce priming** - `BalancePrimingWrapper` in `endorser/testimpl/`
   - `SetExpectedNonce()` stores the transaction's expected nonce
   - `GetNonce()` returns the expected nonce instead of ledger nonce
   - Makes Executor's `tx.Nonce() == ledgerNonce` check pass

Both layers work together to enable wrap-around replay while keeping all test-specific code in `testimpl` directories. Production Gateway and Executor code remain unchanged.

## Visualization

Two Python scripts are provided for visualizing performance results:

- [`integration/perf/plot_performance.py`](integration/perf/plot_performance.py) - Static plots
- [`integration/perf/plot_performance_interactive.py`](integration/perf/plot_performance_interactive.py) - Interactive plots with Plotly

Install dependencies:

```bash
pip install matplotlib pandas plotly
```

Run the scripts after collecting performance data from the tests.

## Balance Priming

Balance priming automatically returns a high balance when an ERC-20 balance slot is read and found to be zero, eliminating the need to pre-fund thousands of test accounts.

Implemented in `endorser/testimpl/balance_priming_statedb.go`.

## Dataset Files

Large files are **excluded** from the repository:

- `integration/perf/testdata/dataset.tsv.gz` (~10MB) - Filtered USDC transfers
- `integration/perf/testdata/USDC_dataset.json.gz` (~27MB) - Pre-generated transactions
- `integration/perf/testdata/202001.tsv.gz` - Source dataset

Generate these locally using the steps above. Small files like `USDC_FiatTokenV2_2.abi` remain committed.

## Troubleshooting

### "dataset not found" errors

Make sure you've completed steps 1-5 to generate the required dataset files in [`integration/perf/testdata/`](integration/perf/testdata/).

### Wrap-around replay fails

Verify you're running through the perf test harness. Wrap-around replay requires:

- `NonceBypassGateway` wrapper (gateway-level)
- `BalancePrimingWrapper` with nonce priming (endorser-level)

Both are automatically configured in performance tests.

### Test runs 30+ minutes then crashes

You're likely using `PERF_REPLAY_WINDOW_SIZE=0` locally. This selects the full ~151k transfer dataset, intended for distributed infrastructure.

Use a bounded window instead:

```bash
PERF_REPLAY_WINDOW_SIZE=3000 go test -tags=perf -v ./integration/perf/...
```

or

```bash
PERF_REPLAY_WINDOW_SIZE=10000 go test -tags=perf -v ./integration/perf/...
```

### Out of memory or long setup times

You can create a smaller filtered dataset by limiting the number of lines:

```bash
gunzip -c integration/perf/testdata/202001.tsv.gz | grep -iE '(a0b86991c6218b36c1d19d4a2e9eb0ce3606eb48|token_address)' | head -n 1000 | gzip > integration/perf/testdata/dataset.tsv.gz
```

You can also reduce replay size at execution time:

```bash
PERF_REPLAY_WINDOW_SIZE=500 go test -tags=perf -v ./integration/perf/...
```

### Contract fetch failures

`integration/perf/fetcher.go` requires Ethereum mainnet RPC access. It uses public endpoints by default, but you may need your own if rate-limited.

## References

- Dataset source: [Harvard Dataverse - Ethereum Token Transfers](https://dataverse.harvard.edu/dataset.xhtml?persistentId=doi:10.7910/DVN/8YO2VZ)
- USDC contract: `0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48`
