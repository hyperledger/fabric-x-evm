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

TEST_PATH="${1:-test/token/ERC20/ERC20.test.js}"
TESTNODE_PID=""

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

main() {
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}Fabric-EVM Hardhat Integration Test${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""

    cd "${PROJECT_ROOT}"
    check_prerequisites
    init_openzeppelin
    start_testnode
    run_tests
}

main
