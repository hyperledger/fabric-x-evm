#!/bin/bash
# Copyright IBM Corp. All Rights Reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
RESULTS_DIR="${PROJECT_ROOT}/testdata/eth-tests-results"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

main() {
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}Ethereum Conformance Tests${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""

    cd "${PROJECT_ROOT}"

    if [ ! -d "testdata/execution-specs-tests/fixtures" ]; then
        echo -e "${RED}Error: execution-specs fixtures not found${NC}"
        echo "Run: make fetch-execution-specs-tests"
        exit 1
    fi

    mkdir -p "${RESULTS_DIR}"

    echo -e "${YELLOW}Running TestEthereumTests...${NC}"
    # -json implies -v; each line is a self-contained event, so unlike the
    # Hardhat suite (many independent file loads, split per-directory so one
    # crash doesn't zero out the rest) this is one test binary and one
    # invocation is enough — a mid-run crash still leaves everything up to
    # that point parseable by cmd/baseline.
    if go test -test.fullpath=true -timeout 2000s -run ^TestEthereumTests$ -json \
        github.com/hyperledger/fabric-x-evm/integration > "${RESULTS_DIR}/eth-tests.json"; then
        echo -e "${GREEN}TestEthereumTests: all passed${NC}"
    else
        echo -e "${YELLOW}TestEthereumTests: has failures (expected — see ${RESULTS_DIR}/eth-tests.json)${NC}"
    fi

    echo ""
    echo -e "${YELLOW}Running TestTransactionTests...${NC}"
    # Separate invocation/file, same reasoning as above — the eth-tests suite's
    # results glob (testdata/eth-tests-results/*.json) picks both up as one suite.
    if go test -test.fullpath=true -timeout 2000s -run ^TestTransactionTests$ -json \
        github.com/hyperledger/fabric-x-evm/integration > "${RESULTS_DIR}/transaction-tests.json"; then
        echo -e "${GREEN}TestTransactionTests: all passed${NC}"
    else
        echo -e "${YELLOW}TestTransactionTests: has failures (expected — see ${RESULTS_DIR}/transaction-tests.json)${NC}"
    fi

    echo ""
    echo -e "${GREEN}Results written to ${RESULTS_DIR}/${NC}"
    echo ""

    # Report only — this script's own exit status stays tied to whether the
    # suite ran at all, not the baseline diff. Regressions failing the build
    # is what a CI gate is for; a local, exploratory run shouldn't error out
    # just because a known failure is still known to fail. Same idiom as
    # run_hardhat_test.sh's --full.
    go run ./cmd/baseline check --suite eth-tests || true
}

main
