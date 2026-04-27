#!/bin/bash
# Copyright IBM Corp. All Rights Reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OZ_DIR="${PROJECT_ROOT}/testdata/openzeppelin-contracts"
GATEWAY_PID=""
FABRIC_STARTED=false

# Default test path
TEST_PATH="${1:-test/token/ERC20/ERC20.test.js}"

# Cleanup function
cleanup() {
    echo -e "\n${YELLOW}Cleaning up...${NC}"
    
    # Kill gateway if running
    if [ -n "${GATEWAY_PID}" ]; then
        echo "Stopping gateway (PID: ${GATEWAY_PID})..."
        kill "${GATEWAY_PID}" 2>/dev/null || true
        wait "${GATEWAY_PID}" 2>/dev/null || true
    fi
    
    # Stop Fabric if we started it
    if [ "${FABRIC_STARTED}" = true ]; then
        echo "Stopping Fabric network..."
        cd "${PROJECT_ROOT}"
        make stop-3 2>/dev/null || true
    fi
    
    echo -e "${GREEN}Cleanup complete${NC}"
}

# Set up trap for cleanup
trap cleanup EXIT INT TERM

# Check prerequisites
check_prerequisites() {
    echo -e "${YELLOW}Checking prerequisites...${NC}"
    
    # Check for node/npm
    if ! command -v node &> /dev/null; then
        echo -e "${RED}Error: node is not installed${NC}"
        exit 1
    fi
    
    if ! command -v npm &> /dev/null; then
        echo -e "${RED}Error: npm is not installed${NC}"
        exit 1
    fi
    
    # Check for Go
    if ! command -v go &> /dev/null; then
        echo -e "${RED}Error: go is not installed${NC}"
        exit 1
    fi
    
    echo -e "${GREEN}Prerequisites OK${NC}"
}

# Initialize OpenZeppelin submodule
init_openzeppelin() {
    echo -e "${YELLOW}Initializing OpenZeppelin contracts...${NC}"
    
    cd "${PROJECT_ROOT}"
    
    # Initialize submodule if not already done
    if [ ! -f "${OZ_DIR}/package.json" ]; then
        echo "Initializing git submodule..."
        git submodule update --init --recursive testdata/openzeppelin-contracts
    fi
    
    # Install dependencies
    cd "${OZ_DIR}"
    if [ ! -d "node_modules" ]; then
        echo "Installing npm dependencies..."
        npm install
    else
        echo "Dependencies already installed"
    fi
    
    echo -e "${GREEN}OpenZeppelin contracts ready${NC}"
}

# Start Fabric network
start_fabric() {
    echo -e "${YELLOW}Starting Fabric network...${NC}"
    
    cd "${PROJECT_ROOT}"
    
    # Check if Fabric samples exist
    if [ ! -d "testdata/fabric-samples" ]; then
        echo "Fabric samples not found. Running make init-3..."
        make init-3
    fi
    
    # Start Fabric network
    echo "Starting Fabric test network..."
    make start-3
    FABRIC_STARTED=true
    
    # Wait a bit for network to stabilize
    echo "Waiting for network to stabilize..."
    sleep 5
    
    echo -e "${GREEN}Fabric network started${NC}"
}

# Start gateway
start_gateway() {
    echo -e "${YELLOW}Starting fabric-evm gateway...${NC}"
    
    cd "${PROJECT_ROOT}/gateway"
    
    # Start gateway (test RPC is enabled in FabricSamplesConfig)
    echo "Starting gateway with test RPC enabled..."
    go run . --protocol fabric &
    
    GATEWAY_PID=$!
    
    # Wait for gateway to be ready
    echo "Waiting for gateway to be ready..."
    MAX_RETRIES=30
    RETRY_COUNT=0
    
    while [ ${RETRY_COUNT} -lt ${MAX_RETRIES} ]; do
        if curl -s -X POST -H "Content-Type: application/json" \
            --data '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' \
            http://127.0.0.1:8545 > /dev/null 2>&1; then
            echo -e "${GREEN}Gateway is ready!${NC}"
            return 0
        fi
        
        RETRY_COUNT=$((RETRY_COUNT + 1))
        echo -n "."
        sleep 1
    done
    
    echo -e "\n${RED}Error: Gateway failed to start${NC}"
    exit 1
}

# Run Hardhat tests
run_tests() {
    echo -e "${YELLOW}Running Hardhat tests...${NC}"
    echo -e "Test path: ${GREEN}${TEST_PATH}${NC}"
    
    cd "${OZ_DIR}"
    
    # Run tests against fabricevm network
    echo "Executing: npx hardhat test ${TEST_PATH} --network fabricevm"
    npx hardhat test "${TEST_PATH}" --network fabricevm
    
    TEST_EXIT_CODE=$?
    
    if [ ${TEST_EXIT_CODE} -eq 0 ]; then
        echo -e "\n${GREEN}✓ Tests passed!${NC}"
    else
        echo -e "\n${RED}✗ Tests failed with exit code ${TEST_EXIT_CODE}${NC}"
    fi
    
    return ${TEST_EXIT_CODE}
}

# Main execution
main() {
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}Fabric-EVM Hardhat Integration Test${NC}"
    echo -e "${GREEN}========================================${NC}\n"
    
    check_prerequisites
    init_openzeppelin
    start_fabric
    start_gateway
    run_tests
    
    echo -e "\n${GREEN}========================================${NC}"
    echo -e "${GREEN}Test execution complete${NC}"
    echo -e "${GREEN}========================================${NC}"
}

# Run main function
main

# Made with Bob
