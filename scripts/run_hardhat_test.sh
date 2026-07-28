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

DEFAULT_PORT=8545

usage() {
    cat <<EOF
Usage: $(basename "$0") [--file <path>] [--grep <pattern>] [--port <n>]

With no arguments, runs the whole OpenZeppelin compatible set — one Hardhat
invocation per top-level test/ directory — and diffs the result against
testdata/oz_known_failures.json. This is what CI runs.

  --file <path>     Narrow to one test file (or directory) instead of the full set.
  --grep <pattern>  Narrow to tests whose full title matches <pattern>.
  --port <n>        Port for this run's testnode (default ${DEFAULT_PORT}). Use it to run
                    a second suite without disturbing one already going.
  -h, --help        Show this message.

A narrowed run skips the results/baseline step, since a partial run can't tell a
test that didn't fail from one that didn't run.

  $(basename "$0") --file test/token/ERC20/ERC20.test.js
  $(basename "$0") --file test/token/ERC20/ERC20.test.js --grep _approve
  $(basename "$0") --grep 'ERC20 _mint'
  $(basename "$0") --port 8600
EOF
}

TEST_FILE=""
GREP_PATTERN=""
PORT="${DEFAULT_PORT}"
while [ $# -gt 0 ]; do
    case "$1" in
        --file)
            [ $# -ge 2 ] || { echo -e "${RED}Error: --file needs a path${NC}" >&2; exit 2; }
            TEST_FILE="$2"; shift 2 ;;
        --grep)
            [ $# -ge 2 ] || { echo -e "${RED}Error: --grep needs a pattern${NC}" >&2; exit 2; }
            GREP_PATTERN="$2"; shift 2 ;;
        --port)
            [ $# -ge 2 ] || { echo -e "${RED}Error: --port needs a number${NC}" >&2; exit 2; }
            case "$2" in *[!0-9]*|'') echo -e "${RED}Error: --port must be a number, got '$2'${NC}" >&2; exit 2 ;; esac
            PORT="$2"; shift 2 ;;
        -h|--help)
            usage; exit 0 ;;
        *)
            echo -e "${RED}Error: unknown argument '$1'${NC}" >&2
            echo >&2
            usage >&2
            exit 2 ;;
    esac
done
TESTNODE_PID=""

# The compatible set: every test/**/*.test.js except test/account/** and
# test/utils/Blockhash.test.js, which need the hardhat-predeploy plugin
# (stubbed as a no-op by the test backend).
COMPAT_DIRS=(access crosschain finance governance metatx proxy token utils)
# Under testdata/ (gitignored by default, not /tmp) so a human actually notices
# it's there — e.g. `grep '"fullTitle": "..."' -A2 testdata/oz-hardhat-results/*.json`
# to find which file a failing test lives in, without any code needing to track it.
RESULTS_DIR="${PROJECT_ROOT}/testdata/oz-hardhat-results"

cleanup() {
    if [ -n "${TESTNODE_PID}" ] && kill -0 "${TESTNODE_PID}" 2>/dev/null; then
        echo -e "\n${YELLOW}Stopping testnode (PID: ${TESTNODE_PID})${NC}"
        # Negative PID kills the whole process group. `go run` execs the compiled
        # program as a child, so killing only the parent leaves the server
        # orphaned and still holding the port.
        kill -- -"${TESTNODE_PID}" 2>/dev/null || true
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

    # Refuse rather than kill: whatever is on the port may well be someone
    # else's run, and taking it out from under them corrupts both.
    EXISTING_PID=$(lsof -ti :"${PORT}" -sTCP:LISTEN 2>/dev/null || true)
    if [ -n "${EXISTING_PID}" ]; then
        echo -e "${RED}Error: port ${PORT} is already in use (PID: ${EXISTING_PID})${NC}" >&2
        echo "Stop it, or run this suite elsewhere with --port (make hardhat-tests PORT=8600)." >&2
        exit 1
    fi

    echo "Starting testnode on port ${PORT} (logs: /tmp/testnode_$$.log)..."
    # Job control on, so the background job lands in its own process group and
    # cleanup can take down go run and the server it execs together.
    set -m
    go run ./cmd/fxevm testnode --listen ":${PORT}" > "/tmp/testnode_$$.log" 2>&1 &
    TESTNODE_PID=$!
    set +m

    echo "Waiting for testnode to be ready..."
    MAX_RETRIES=30
    RETRY_COUNT=0
    while [ ${RETRY_COUNT} -lt ${MAX_RETRIES} ]; do
        if curl -s -X POST -H "Content-Type: application/json" \
            --data '{"jsonrpc":"2.0","method":"eth_accounts","params":[],"id":1}' \
            "http://127.0.0.1:${PORT}" 2>/dev/null | grep -q "result"; then
            echo -e "${GREEN}Testnode is ready!${NC}"
            export FABRIC_EVM_URL="http://127.0.0.1:${PORT}"
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

# compat_files echoes the compatible set for one top-level test/ directory:
# every *.test.js except the ones needing the hardhat-predeploy plugin.
compat_files() {
    if [ "$1" = "utils" ]; then
        find "test/$1" -name '*.test.js' ! -name 'Blockhash.test.js' | sort
    else
        find "test/$1" -name '*.test.js' | sort
    fi
}

# run_narrowed handles --file/--grep: a single Hardhat invocation over whatever
# the caller asked for. No results file and no baseline diff — a partial run
# can't distinguish a test that didn't fail from one that never ran, so feeding
# it to `baseline check` would report the entire rest of the suite as stale.
run_narrowed() {
    cd "${OZ_DIR}"

    local files=()
    if [ -n "${TEST_FILE}" ]; then
        files=("${TEST_FILE}")
    else
        # --grep with no --file: search the whole compatible set.
        for dir in "${COMPAT_DIRS[@]}"; do
            while IFS= read -r f; do files+=("$f"); done < <(compat_files "${dir}")
        done
    fi

    local args=(test "${files[@]}" --config "${WRAPPER_CONFIG}" --network fabricevm)
    [ -n "${GREP_PATTERN}" ] && args+=(--grep "${GREP_PATTERN}")

    echo -e "${YELLOW}Running Hardhat tests...${NC}"
    [ -n "${TEST_FILE}" ] && echo -e "File: ${GREEN}${TEST_FILE}${NC}"
    [ -n "${GREP_PATTERN}" ] && echo -e "Grep: ${GREEN}${GREP_PATTERN}${NC}"
    # %q so a pattern with spaces stays copy-pasteable.
    printf 'Executing: npx hardhat'; printf ' %q' "${args[@]}"; printf '\n'
    npx hardhat "${args[@]}"
}

# run_full_suite drives the whole OZ compatible set, one Hardhat invocation per
# top-level test/ directory. Splitting per-directory (rather than one giant
# run) means a load-time crash in one directory's tests doesn't
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
        while IFS= read -r f; do files+=("$f"); done < <(compat_files "${dir}")

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
    # suite ran at all, not to the baseline diff. Regressions failing the build
    # is what CI's own gate step is for; a local run shouldn't error out just
    # because a known failure is still known to fail.
    go run ./cmd/baseline check --suite oz-hardhat || true
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
    if [ -n "${TEST_FILE}" ] || [ -n "${GREP_PATTERN}" ]; then
        run_narrowed
    else
        run_full_suite
    fi
}

main
