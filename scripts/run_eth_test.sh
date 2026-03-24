#!/bin/bash
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0

# Source shared functions
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib/fabric_test_common.sh"

# Function to check and initialize ethereum-tests submodule
check_and_init_ethereum_tests() {
    # Check if testdata/ethereum-tests directory exists
    if [[ ! -d "./testdata/ethereum-tests" ]]; then
        echo "ethereum-tests directory not found. Initializing submodule..."
        git submodule update --init --recursive
        return
    fi

    # Check if it's a git submodule (has .git)
    if [[ ! -d "./testdata/ethereum-tests/.git" ]] && [[ ! -f "./testdata/ethereum-tests/.git" ]]; then
        echo "ethereum-tests exists but is not a git submodule. Initializing..."
        git submodule update --init --recursive
        return
    fi

    # Check if it has the expected content
    if [[ ! -d "./testdata/ethereum-tests/LegacyTests" ]]; then
        echo "ethereum-tests submodule appears empty. Updating..."
        git submodule update --init --recursive
        return
    fi

    echo "ethereum-tests submodule is already initialized."
}

# Parse arguments and validate environment
parse_verbose_flag "$@"
check_project_root "run_eth_test.sh"
ensure_testdata_dir

# Setup cleanup trap
setup_cleanup_trap

# Execute setup steps
check_and_init_ethereum_tests

# Final verification of ethereum-tests
if [[ ! -d "./testdata/ethereum-tests/LegacyTests" ]]; then
    echo "Error: ethereum-tests submodule not properly initialized."
    echo "Please run: git submodule update --init --recursive"
    exit 1
fi

check_and_download_fabric_samples
start_network_and_deploy_chaincode

echo "Running Ethereum tests..."

go test $VERBOSE_FLAG -timeout 360s -run 'TestEthereumTests$' ./integration/