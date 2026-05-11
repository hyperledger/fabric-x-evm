# USDC Performance Testing

This directory contains infrastructure for performance testing using real USDC transaction data from Ethereum mainnet.

## Overview

The performance tests replay actual USDC transfers to measure Fabric-EVM throughput and latency under realistic workloads. The tests use balance priming to avoid pre-funding thousands of accounts.

## Running the Tests

Performance tests are gated behind the `perf` build tag and must be run explicitly:

```bash
go test -tags=perf ./integration/perf/...
```

## Setup Instructions

**All commands should be run from the repository root directory.**

### Prerequisites

- Go 1.26+
- Python 3.x (for visualization scripts)
- `wget` or `curl` for downloading datasets
- `zgrep` for filtering compressed data

### Step 1: Download the Dataset

Download the token transfer dataset from Harvard Dataverse to the repository root:

```bash
wget https://dataverse.harvard.edu/api/access/datafile/11691882 -O 202001.tsv.gz
```

This downloads the complete January 2020 token transfer dataset (~10GB compressed) to the current directory.

**Note**: The large dataset files (`dataset.tsv.gz`, `USDC_dataset.json.gz`) are **not committed to the repository**. You must generate them locally following these instructions.

### Step 2: Filter USDC Transactions

Extract only USDC transfers (contract address `0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48`):

```bash
zgrep -E '(^.{93}a0b86991c6218b36c1d19d4a2e9eb0ce3606eb48|token_address)' 202001.tsv.gz | gzip > integration/perf/testdata/dataset.tsv.gz
```

This creates a filtered dataset (~10MB compressed) containing only USDC transfers in `integration/perf/testdata/`.

### Step 3: Fetch USDC Contract Code

The USDC contract is a proxy, so we need to fetch both the proxy and implementation code from Ethereum mainnet:

```bash
go run -tags=perf ./integration/perf -mode fetch
```

This creates `integration/perf/testdata/USDC_contract.json` with the contract bytecode and metadata.

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

This generates the Go bindings file from the committed ABI. The generated file is excluded from the repository via `.gitignore` and must be created locally before running perf tests.

### Step 5: Pre-generate Transactions

Convert the TSV dataset into pre-signed Ethereum transactions:

```bash
go run -tags=perf ./integration/perf -mode generate -input ./integration/perf/testdata/dataset.tsv.gz
```

This creates `integration/perf/testdata/USDC_dataset.json.gz` (~27MB compressed) containing pre-generated transaction payloads.

### Step 6: Run Performance Tests

Now you can run the performance tests:

```bash
go test -tags=perf -v ./integration/perf/...
```

The test will:
1. Prime the USDC contract code in the ledger
2. Use balance priming to automatically fund accounts on first access
3. Replay all transactions and measure performance
4. Output metrics (throughput, latency, etc.)

## Visualization

Two Python scripts are provided for visualizing performance results:

- `plot_performance.py` - Static plots
- `plot_performance_interactive.py` - Interactive plots with Plotly

Install dependencies:

```bash
pip install matplotlib pandas plotly
```

Run the scripts after collecting performance data from the tests.

## Balance Priming

The tests use an optimization called "balance priming" that automatically returns a high balance (1 billion tokens) when an ERC-20 balance slot is read and found to be zero. This eliminates the need to pre-fund thousands of test accounts.

Balance priming is implemented in `endorser/statedb_wrapper.go` and configured via `endorser.BalancePrimingConfig`.

## Dataset Files

The following large files are **intentionally excluded** from the repository:

- `testdata/dataset.tsv.gz` (~10MB) - Filtered USDC transfers
- `testdata/USDC_dataset.json.gz` (~27MB) - Pre-generated transactions

These must be generated locally using the steps above. Only the small `USDC_contract.json` file is committed.

## Troubleshooting

### "dataset not found" errors

Make sure you've completed steps 1-5 to generate the required dataset files in `integration/perf/testdata/`.

### Out of memory errors

The full dataset contains thousands of transactions. You can create a smaller test dataset by limiting the number of lines:

```bash
zgrep -E '(^.{93}a0b86991c6218b36c1d19d4a2e9eb0ce3606eb48|token_address)' 202001.tsv.gz | head -n 1000 | gzip > integration/perf/testdata/dataset.tsv.gz
```

### Contract fetch failures

The `fetcher.go` tool requires access to an Ethereum mainnet RPC endpoint. By default it uses public endpoints, but you may need to configure your own if rate limits are hit.

## References

- Dataset source: [Harvard Dataverse - Ethereum Token Transfers](https://dataverse.harvard.edu/dataset.xhtml?persistentId=doi:10.7910/DVN/8YO2VZ)
- USDC contract: `0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48`