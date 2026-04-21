#!/bin/bash
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
#
# Run Hardhat tests against fabric-evm gateway
# This script starts the Fabric network, starts the gateway, runs tests, and cleans up

# Source shared functions
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib/fabric_test_common.sh"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Hardhat Integration Test Runner ===${NC}"

# Parse arguments and validate environment
parse_verbose_flag "$@"
check_project_root "run_hardhat_test.sh"
ensure_testdata_dir

# Parse test-specific arguments
TEST_FILE="${1:-sanity.test.js}"
NETWORK="${2:-fabricevm}"
KEEP_RUNNING="${KEEP_RUNNING:-false}"

echo "Test file: $TEST_FILE"
echo "Network: $NETWORK"
echo "Note: Use 'sanity.test.js' for quick validation, or 'test/token/ERC20/ERC20.test.js' for full suite"

# Extended cleanup function
cleanup() {
    echo -e "\n${YELLOW}Cleaning up...${NC}"
    
    # Kill gateway if running
    if [ ! -z "$GATEWAY_PID" ]; then
        echo "Stopping gateway (PID: $GATEWAY_PID)"
        kill $GATEWAY_PID 2>/dev/null || true
        wait $GATEWAY_PID 2>/dev/null || true
    fi
    
    # Stop Fabric network using shared cleanup
    if [ "$KEEP_RUNNING" != "true" ]; then
        cleanup_network
    else
        echo "Keeping Fabric network running (KEEP_RUNNING=true)"
    fi
}

# Set trap to cleanup on exit (overrides the one from fabric_test_common.sh)
trap cleanup EXIT INT TERM

# Step 1: Setup Fabric network
echo -e "\n${GREEN}Step 1: Setting up Fabric network...${NC}"
# Clean up trie database from previous runs
rm -rf testdata/triedb 2>/dev/null || true
check_and_download_fabric_samples
start_network_and_deploy_chaincode

# Step 2: Start gateway
echo -e "\n${GREEN}Step 2: Starting fabric-evm gateway...${NC}"
# Navigate to gateway directory (we're already in project root from check_project_root)
cd gateway

# Start gateway in background with --protocol fabric flag
# This tells the gateway to use FabricSamplesConfig which points to the correct crypto paths
go run . --protocol fabric > /tmp/fabric-evm-gateway.log 2>&1 &
GATEWAY_PID=$!

echo "Gateway started with PID: $GATEWAY_PID"
echo "Gateway logs: /tmp/fabric-evm-gateway.log"

# Wait for gateway to be ready
echo "Waiting for gateway to be ready..."
MAX_WAIT=30
WAIT_COUNT=0
while ! nc -z localhost 8545 2>/dev/null; do
    if [ $WAIT_COUNT -ge $MAX_WAIT ]; then
        echo -e "${RED}Gateway failed to start within ${MAX_WAIT} seconds${NC}"
        echo "Last 20 lines of gateway log:"
        tail -20 /tmp/fabric-evm-gateway.log
        exit 1
    fi
    sleep 1
    WAIT_COUNT=$((WAIT_COUNT + 1))
    echo -n "."
done
echo -e "\n${GREEN}Gateway is ready!${NC}"

# Step 3: Run Hardhat tests
echo -e "\n${GREEN}Step 3: Running Hardhat tests...${NC}"
# Navigate back to project root, then to openzeppelin-contracts
cd "$(dirname "$SCRIPT_DIR")/testdata/openzeppelin-contracts"

# Run the test
echo "Running: npx hardhat test $TEST_FILE --network $NETWORK"
if npx hardhat test "$TEST_FILE" --network "$NETWORK"; then
    echo -e "\n${GREEN}✓ Tests completed successfully!${NC}"
    EXIT_CODE=0
else
    echo -e "\n${RED}✗ Tests failed${NC}"
    EXIT_CODE=1
fi

# Show gateway logs if tests failed
if [ $EXIT_CODE -ne 0 ]; then
    echo -e "\n${YELLOW}Last 50 lines of gateway log:${NC}"
    tail -50 /tmp/fabric-evm-gateway.log
fi

exit $EXIT_CODE

# Made with Bob
