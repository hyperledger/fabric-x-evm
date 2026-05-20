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
PERF_DIR="${PROJECT_ROOT}/integration/perf"
TESTDATA_DIR="${PERF_DIR}/testdata"
DATASET_URL="https://dataverse.harvard.edu/api/access/datafile/11691882"
USDC_ADDRESS="a0b86991c6218b36c1d19d4a2e9eb0ce3606eb48"

# Check prerequisites
check_prerequisites() {
    echo -e "${YELLOW}Checking prerequisites...${NC}"
    
    # Check for required commands
    for cmd in go wget abigen; do
        if ! command -v $cmd &> /dev/null; then
            echo -e "${RED}Error: $cmd is not installed${NC}"
            if [ "$cmd" = "abigen" ]; then
                echo "Install with: go install github.com/ethereum/go-ethereum/cmd/abigen@latest"
            fi
            exit 1
        fi
    done
    
    echo -e "${GREEN}Prerequisites OK${NC}"
}

# Download and filter dataset
setup_dataset() {
    echo -e "${YELLOW}Setting up USDC dataset...${NC}"
    
    # Create testdata directory
    mkdir -p "${TESTDATA_DIR}"
    
    # Check if filtered dataset already exists
    if [ -f "${TESTDATA_DIR}/dataset.tsv.gz" ] && [ $(stat -f%z "${TESTDATA_DIR}/dataset.tsv.gz" 2>/dev/null || stat -c%s "${TESTDATA_DIR}/dataset.tsv.gz" 2>/dev/null) -gt 1000000 ]; then
        echo -e "${GREEN}Filtered dataset already exists, skipping download${NC}"
        return 0
    fi
    
    # Download full dataset if not present
    if [ ! -f "${TESTDATA_DIR}/202001.tsv.gz" ]; then
        echo "Downloading January 2020 token transfer dataset (~627MB)..."
        echo "This may take several minutes..."
        wget -O "${TESTDATA_DIR}/202001.tsv.gz" "${DATASET_URL}"
    else
        echo "Full dataset already downloaded"
    fi
    
    # Filter for USDC transactions
    echo "Filtering USDC transactions..."
    # Use gunzip -c for portability (works on both Linux and macOS)
    gunzip -c "${TESTDATA_DIR}/202001.tsv.gz" | grep -iE "(${USDC_ADDRESS}|token_address)" | gzip > "${TESTDATA_DIR}/dataset.tsv.gz"
    
    FILTERED_SIZE=$(stat -f%z "${TESTDATA_DIR}/dataset.tsv.gz" 2>/dev/null || stat -c%s "${TESTDATA_DIR}/dataset.tsv.gz" 2>/dev/null)
    echo -e "${GREEN}Filtered dataset created: $(numfmt --to=iec-i --suffix=B ${FILTERED_SIZE} 2>/dev/null || echo "${FILTERED_SIZE} bytes")${NC}"
}

# Generate Go bindings from ABI
generate_bindings() {
    echo -e "${YELLOW}Generating USDC contract bindings...${NC}"
    
    # Check if bindings already exist
    if [ -f "${PROJECT_ROOT}/integration/contracts/USDC_fiattokenv2_2.gen.go" ]; then
        echo -e "${GREEN}Bindings already exist, skipping generation${NC}"
        return 0
    fi
    
    # Generate bindings from ABI
    abigen --abi "${TESTDATA_DIR}/USDC_FiatTokenV2_2.abi" \
        --pkg contracts \
        --type FiatTokenV2_2 \
        --out "${PROJECT_ROOT}/integration/contracts/USDC_fiattokenv2_2.gen.go"
    
    echo -e "${GREEN}Bindings generated${NC}"
}

# Fetch USDC contract code
fetch_contract() {
    echo -e "${YELLOW}Fetching USDC contract code from Ethereum mainnet...${NC}"
    
    # Check if contract already fetched
    if [ -f "${TESTDATA_DIR}/USDC_contract.json" ]; then
        echo -e "${GREEN}Contract already fetched, skipping${NC}"
        return 0
    fi
    
    # Fetch contract using the fetcher tool
    go run -tags=perf "${PERF_DIR}" -mode fetch
    
    echo -e "${GREEN}Contract fetched${NC}"
}

# Generate pre-signed transactions
generate_transactions() {
    echo -e "${YELLOW}Generating pre-signed transactions...${NC}"
    
    # Check if transactions already generated
    if [ -f "${TESTDATA_DIR}/USDC_dataset.json.gz" ] && [ $(stat -f%z "${TESTDATA_DIR}/USDC_dataset.json.gz" 2>/dev/null || stat -c%s "${TESTDATA_DIR}/USDC_dataset.json.gz" 2>/dev/null) -gt 10000000 ]; then
        echo -e "${GREEN}Transactions already generated, skipping${NC}"
        return 0
    fi
    
    # Generate transactions using the fetcher tool
    go run -tags=perf "${PERF_DIR}" -mode generate -input "${TESTDATA_DIR}/dataset.tsv.gz"
    
    DATASET_SIZE=$(stat -f%z "${TESTDATA_DIR}/USDC_dataset.json.gz" 2>/dev/null || stat -c%s "${TESTDATA_DIR}/USDC_dataset.json.gz" 2>/dev/null)
    echo -e "${GREEN}Transaction dataset created: $(numfmt --to=iec-i --suffix=B ${DATASET_SIZE} 2>/dev/null || echo "${DATASET_SIZE} bytes")${NC}"
}

# Run performance tests
run_tests() {
    echo -e "${YELLOW}Running performance tests...${NC}"
    
    # Run tests with perf tag
    go test -tags=perf -v -timeout 30m ./integration/perf/...
}

# Main execution
main() {
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}Fabric-EVM Performance Test Setup${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    
    # Ensure we're in project root
    cd "${PROJECT_ROOT}"
    
    # Parse arguments and validate environment
    check_project_root "run_perf_test.sh"
    ensure_testdata_dir
    
    # Setup cleanup trap
    setup_cleanup_trap
    
    # Check prerequisites
    check_prerequisites
    
    # Setup dataset
    setup_dataset
    
    # Generate bindings
    generate_bindings
    
    # Fetch contract
    fetch_contract
    
    # Generate transactions
    generate_transactions
    
    # Start Fabric network
    echo -e "${YELLOW}Starting Fabric network...${NC}"
    start_network_and_deploy_chaincode
    echo "Waiting for network to fully stabilize..."
    sleep 10
    echo -e "${GREEN}Fabric network started${NC}"
    
    # Run tests
    run_tests
    
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}Performance tests complete!${NC}"
    echo -e "${GREEN}========================================${NC}"
}

# Run main function
main
