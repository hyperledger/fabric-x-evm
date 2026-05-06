#!/bin/bash
# Copyright IBM Corp. All Rights Reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Source shared functions
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib/fabric_test_common.sh"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OZ_DIR="${PROJECT_ROOT}/testdata/openzeppelin-contracts"
GATEWAY_PID=""

# Default test path
TEST_PATH="${1:-test/token/ERC20/ERC20.test.js}"

# Enhanced cleanup function
cleanup() {
    echo -e "\n${YELLOW}Cleaning up...${NC}"
    
    # Kill gateway if running
    if [ -n "${GATEWAY_PID}" ] && kill -0 ${GATEWAY_PID} 2>/dev/null; then
        echo "Stopping gateway (PID: ${GATEWAY_PID})"
        kill ${GATEWAY_PID} 2>/dev/null || true
        wait ${GATEWAY_PID} 2>/dev/null || true
    fi
    
    # Clean up triedb to avoid lock issues
    if [ -d "${PROJECT_ROOT}/testdata/triedb" ]; then
        echo "Removing triedb directory..."
        rm -rf "${PROJECT_ROOT}/testdata/triedb"
    fi
    
    # Call shared cleanup for Fabric network
    cleanup_network
    
    echo -e "${GREEN}Cleanup complete${NC}"
}

# Set up trap for cleanup
trap cleanup EXIT INT TERM

# Check prerequisites
check_prerequisites() {
    echo -e "${YELLOW}Checking prerequisites...${NC}"
    
    # Check for required commands
    for cmd in node npx go docker; do
        if ! command -v $cmd &> /dev/null; then
            echo -e "${RED}Error: $cmd is not installed${NC}"
            exit 1
        fi
    done
    
    echo -e "${GREEN}Prerequisites OK${NC}"
}

# Initialize OpenZeppelin contracts
init_openzeppelin() {
    echo -e "${YELLOW}Initializing OpenZeppelin contracts...${NC}"
    
    if [ ! -d "${OZ_DIR}" ]; then
        echo -e "${RED}Error: OpenZeppelin contracts not found at ${OZ_DIR}${NC}"
        echo "Please initialize the submodule: git submodule update --init --recursive"
        exit 1
    fi
    
    cd "${OZ_DIR}"
    
    # Clear Hardhat cache to ensure fresh config is loaded
    if [ -d "cache" ]; then
        echo "Clearing Hardhat cache..."
        rm -rf cache
    fi
    
    # Install dependencies if needed
    if [ ! -d "node_modules" ]; then
        echo "Installing dependencies..."
        npm install
    else
        echo "Dependencies already installed"
    fi
    
    echo -e "${GREEN}OpenZeppelin contracts ready${NC}"
}

# Start gateway (fresh instance, similar to integration tests)
start_gateway() {
    echo -e "${YELLOW}Starting fabric-evm gateway with test RPC enabled...${NC}"
    
    cd "${PROJECT_ROOT}"
    
    # Kill any existing gateway processes on port 8545
    echo "Checking for existing gateway processes..."
    EXISTING_PID=$(lsof -ti :8545 || true)
    if [ -n "${EXISTING_PID}" ]; then
        echo "Killing existing process on port 8545 (PID: ${EXISTING_PID})"
        kill ${EXISTING_PID} 2>/dev/null || true
        sleep 2
    fi
    
    # Clean up any existing triedb to ensure fresh start
    if [ -d "testdata/triedb" ]; then
        echo "Removing existing triedb directory..."
        rm -rf testdata/triedb
    fi
    
    # Start gateway in test node mode
    echo "Starting test node (logs: /tmp/gateway_$$.log)..."
    go run ./cmd/fxevm testnode --protocol fabric \
        --test-accounts-path testdata/test_accounts.json \
        > /tmp/gateway_$$.log 2>&1 &
    
    GATEWAY_PID=$!
    echo "Gateway PID: ${GATEWAY_PID}"
    
    # Wait for gateway to be ready and verify test RPC
    echo "Waiting for gateway to be ready..."
    MAX_RETRIES=30
    RETRY_COUNT=0
    
    while [ ${RETRY_COUNT} -lt ${MAX_RETRIES} ]; do
        # Test both chainId and accounts to verify test RPC is working
        if curl -s -X POST -H "Content-Type: application/json" \
            --data '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' \
            http://127.0.0.1:8545 2>/dev/null | grep -q "result" && \
           curl -s -X POST -H "Content-Type: application/json" \
            --data '{"jsonrpc":"2.0","method":"eth_accounts","params":[],"id":1}' \
            http://127.0.0.1:8545 2>/dev/null | grep -q "result"; then
            echo -e "${GREEN}Gateway is ready with test RPC enabled!${NC}"
            
            # Display test account count for verification
            ACCOUNTS=$(curl -s -X POST -H "Content-Type: application/json" \
                --data '{"jsonrpc":"2.0","method":"eth_accounts","params":[],"id":1}' \
                http://127.0.0.1:8545 2>/dev/null | grep -o '"0x[^"]*"' | wc -l)
            echo "Test accounts available: ${ACCOUNTS}"
            
            if [ "${ACCOUNTS}" -eq "0" ]; then
                echo -e "${RED}Warning: No test accounts returned!${NC}"
            fi
            
            return 0
        fi
        
        # Check if gateway process is still running
        if ! kill -0 ${GATEWAY_PID} 2>/dev/null; then
            echo -e "\n${RED}Error: Gateway process died${NC}"
            echo "Last 50 lines of gateway log:"
            tail -50 /tmp/gateway_$$.log
            exit 1
        fi
        
        RETRY_COUNT=$((RETRY_COUNT + 1))
        echo -n "."
        sleep 1
    done
    
    echo -e "\n${RED}Error: Gateway failed to start or test RPC not responding${NC}"
    echo "Last 50 lines of gateway log:"
    tail -50 /tmp/gateway_$$.log
    exit 1
}

# Run Hardhat tests
run_tests() {
    echo -e "${YELLOW}Running Hardhat tests...${NC}"
    echo "Test path: ${GREEN}${TEST_PATH}${NC}"
    
    cd "${OZ_DIR}"
    
    # Fabric-EVM limitation: Transaction reverts are not reported like Ethereum.
    # Tests expecting revert errors will timeout. Skip these tests and related
    # test suites that share beforeEach hooks executing transactions.
    #
    # KNOWN LIMITATION: This skip pattern is brittle and specific to OpenZeppelin's
    # ERC20 test suite. It will need updates if:
    # - OpenZeppelin changes test descriptions
    # - Testing other contracts with different test naming
    # - New revert-related test patterns are added
    #
    # Future improvement: Consider a configuration file approach or accept timeouts
    # and document the limitation instead of trying to skip specific tests.
    SKIP_PATTERN="^(?!.*(reverts|rejects|overflow|when the spender has enough allowance|when the spender has unlimited allowance|when the spender does not have enough allowance|for entire balance|for less value than balance|when the sender transfers all balance|executes with balance))"
    
    echo -e "${YELLOW}Note: Skipping revert-related tests (Fabric-EVM limitation)${NC}"
    echo ""
    
    echo "Executing: npx hardhat test ${TEST_PATH} --network fabricevm --grep \"${SKIP_PATTERN}\""
    npx hardhat test "${TEST_PATH}" --network fabricevm --grep "${SKIP_PATTERN}"
}

# Main execution
main() {
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}Fabric-EVM Hardhat Integration Test${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    
    # Ensure we're in project root for all operations
    cd "${PROJECT_ROOT}"
    
    # Parse arguments and validate environment
    check_project_root "run_hardhat_test.sh"
    ensure_testdata_dir
    
    # Clean up any existing network from previous runs
    echo -e "${YELLOW}Cleaning up any existing Fabric network...${NC}"
    cd "$FABLO_DIR" 2>/dev/null && ./fablo down 2>/dev/null || true
    cd - > /dev/null || true
    
    # Check prerequisites
    check_prerequisites
    
    # Initialize OpenZeppelin
    init_openzeppelin
    
    # Start Fabric network (must be in project root)
    cd "${PROJECT_ROOT}"
    echo -e "${YELLOW}Starting Fabric network...${NC}"
    start_network_and_deploy_chaincode
    echo "Waiting for network to fully stabilize..."
    sleep 10
    echo -e "${GREEN}Fabric network started${NC}"
    
    # Start gateway (fresh instance for this test run)
    start_gateway
    
    # Run tests
    run_tests
    
    # Cleanup happens automatically via trap
}

# Run main function
main
