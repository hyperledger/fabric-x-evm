#!/bin/bash
# Copyright IBM Corp. All Rights Reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OZ_DIR="${PROJECT_ROOT}/testdata/openzeppelin-contracts"
WRAPPER_CONFIG="${PROJECT_ROOT}/testdata/hardhat.wrapper.config.js"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

FULL_SUITE=0
TEST_PATH="test/token/ERC20/ERC20.test.js"
if [ "${1:-}" = "--full" ]; then
    FULL_SUITE=1
elif [ -n "${1:-}" ]; then
    TEST_PATH="$1"
fi
TESTNODE_PID=""

# The compatible set: every test/**/*.test.js except test/account/** and
# test/utils/Blockhash.test.js, which need the hardhat-predeploy plugin
# (stubbed as a no-op by the test backend).
COMPAT_DIRS=(access crosschain finance governance metatx proxy token utils)
# Under testdata/ (gitignored by default, not /tmp) so a human actually notices
# it's there — e.g. `grep '"fullTitle": "..."' -A2 testdata/oz-hardhat-results/*.json`
# to find which file a failing test lives in, without any code needing to track it.
RESULTS_DIR="${PROJECT_ROOT}/testdata/oz-hardhat-results"
BASELINE_PATH="${PROJECT_ROOT}/testdata/oz_known_failures.json"

cleanup() {
    if [ -n "${TESTNODE_PID}" ] && kill -0 "${TESTNODE_PID}" 2>/dev/null; then
        echo -e "\n${YELLOW}Stopping testnode (PID: ${TESTNODE_PID})${NC}"
        kill "${TESTNODE_PID}" 2>/dev/null || true
        wait "${TESTNODE_PID}" 2>/dev/null || true
    fi
}
trap cleanup EXIT INT TERM

check_prerequisites() {
    for cmd in node npx go; do
        if ! command -v "$cmd" &> /dev/null; then
            echo -e "${RED}Error: $cmd is not installed${NC}"
            exit 1
        fi
    done
}

init_openzeppelin() {
    if [ ! -d "${OZ_DIR}" ]; then
        echo -e "${RED}Error: OpenZeppelin contracts not found at ${OZ_DIR}${NC}"
        echo "Please initialize the submodule: git submodule update --init --recursive"
        exit 1
    fi

    cd "${OZ_DIR}"
    if [ ! -d "node_modules" ]; then
        echo -e "${YELLOW}Installing OpenZeppelin dependencies...${NC}"
        npm install
    fi
}

start_testnode() {
    echo -e "${YELLOW}Starting self-contained fxevm testnode...${NC}"
    cd "${PROJECT_ROOT}"

    EXISTING_PID=$(lsof -ti :8545 || true)
    if [ -n "${EXISTING_PID}" ]; then
        echo "Killing existing process on port 8545 (PID: ${EXISTING_PID})"
        kill "${EXISTING_PID}" 2>/dev/null || true
        sleep 2
    fi

    echo "Starting testnode (logs: /tmp/testnode_$$.log)..."
    go run ./cmd/fxevm testnode > "/tmp/testnode_$$.log" 2>&1 &
    TESTNODE_PID=$!

    echo "Waiting for testnode to be ready..."
    MAX_RETRIES=30
    RETRY_COUNT=0
    while [ ${RETRY_COUNT} -lt ${MAX_RETRIES} ]; do
        if curl -s -X POST -H "Content-Type: application/json" \
            --data '{"jsonrpc":"2.0","method":"eth_accounts","params":[],"id":1}' \
            http://127.0.0.1:8545 2>/dev/null | grep -q "result"; then
            echo -e "${GREEN}Testnode is ready!${NC}"
            export FABRIC_EVM_URL="http://127.0.0.1:8545"
            return 0
        fi

        if ! kill -0 "${TESTNODE_PID}" 2>/dev/null; then
            echo -e "\n${RED}Error: testnode process died${NC}"
            echo "Last 50 lines of testnode log:"
            tail -50 "/tmp/testnode_$$.log"
            exit 1
        fi

        RETRY_COUNT=$((RETRY_COUNT + 1))
        echo -n "."
        sleep 1
    done

    echo -e "\n${RED}Error: testnode failed to start${NC}"
    echo "Last 50 lines of testnode log:"
    tail -50 "/tmp/testnode_$$.log"
    exit 1
}

run_tests() {
    echo -e "${YELLOW}Running Hardhat tests...${NC}"
    echo "Test path: ${GREEN}${TEST_PATH}${NC}"

    cd "${OZ_DIR}"
    echo "Executing: npx hardhat test ${TEST_PATH} --config ${WRAPPER_CONFIG} --network fabricevm --bail"
    npx hardhat test "${TEST_PATH}" --config "${WRAPPER_CONFIG}" --network fabricevm --bail
}

# run_full_suite drives the whole OZ compatible set, one Hardhat invocation per
# top-level test/ directory, with --bail off. Splitting per-directory (rather
# than one giant run) means a load-time crash in one directory's tests doesn't
# zero out the report for the other seven; each directory's output lands in
# its own file for `cmd/baseline` to glob-merge. HARDHAT_JSON_OUTPUT switches
# to the combined reporter, so the usual pass/fail console view still streams
# by live while the JSON is written straight to file (not stdout).
run_full_suite() {
    echo -e "${YELLOW}Running full OZ compatible set (per-directory)...${NC}"
    rm -rf "${RESULTS_DIR}"
    mkdir -p "${RESULTS_DIR}"
    cd "${OZ_DIR}"

    for dir in "${COMPAT_DIRS[@]}"; do
        echo -e "${YELLOW}-- test/${dir} --${NC}"
        local files=()
        if [ "${dir}" = "utils" ]; then
            while IFS= read -r f; do files+=("$f"); done < <(find "test/${dir}" -name '*.test.js' ! -name 'Blockhash.test.js' | sort)
        else
            while IFS= read -r f; do files+=("$f"); done < <(find "test/${dir}" -name '*.test.js' | sort)
        fi

        if HARDHAT_JSON_OUTPUT="${RESULTS_DIR}/${dir}.json" npx hardhat test "${files[@]}" \
            --config "${WRAPPER_CONFIG}" --network fabricevm; then
            echo -e "${GREEN}test/${dir}: all passed${NC}"
        else
            echo -e "${YELLOW}test/${dir}: has failures (expected — see ${RESULTS_DIR}/${dir}.json)${NC}"
        fi
    done

    echo ""
    echo -e "${GREEN}Results written to ${RESULTS_DIR}/*.json${NC}"
    echo ""

    cd "${PROJECT_ROOT}"
    # Report only — this script's own exit status stays tied to whether the
    # suite ran at all, not to the baseline diff. Regressions failing the
    # build is what the (not-yet-built) CI gate is for; a local, exploratory
    # --full run shouldn't error out just because a known failure is still
    # known to fail.
    go run ./cmd/baseline check --suite oz-hardhat \
        --baseline "${BASELINE_PATH}" --results "${RESULTS_DIR}/*.json" || true
}

main() {
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}Fabric-EVM Hardhat Integration Test${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""

    cd "${PROJECT_ROOT}"
    check_prerequisites
    init_openzeppelin
    start_testnode
    if [ "${FULL_SUITE}" -eq 1 ]; then
        run_full_suite
    else
        run_tests
    fi
}

main
