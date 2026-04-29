#!/bin/bash
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
# Shared functions for Fabric test scripts

# Configuration
REFERENCE_VERSION="v3.1.3"
FABRIC_SAMPLES_PATH="./testdata/fabric-samples/"
PEER_PATH="${FABRIC_SAMPLES_PATH}bin/peer"
NETWORK_PATH="${FABRIC_SAMPLES_PATH}test-network/network.sh"
NETWORK_STARTED=false

# Parse verbose flag from environment or command line
parse_verbose_flag() {
    VERBOSE_FLAG=""
    if [[ "$VERBOSE" == "1" ]] || [[ "$VERBOSE" == "true" ]] || [[ "$1" == "-v" ]] || [[ "$1" == "--verbose" ]]; then
        VERBOSE_FLAG="-v"
    fi
}

# Check if script is being run from project root
check_project_root() {
    local script_name="$1"
    if [[ ! -f "./scripts/${script_name}" ]]; then
        echo "Error: Please run this script from the project root directory."
        exit 1
    fi
}

# Ensure ./testdata exists
ensure_testdata_dir() {
    if [[ ! -d "./testdata" ]]; then
        echo "Creating ./testdata directory..."
        mkdir -p ./testdata
    fi
}

# Function to check and download fabric-samples if needed
check_and_download_fabric_samples() {
    local found=false

    # Check if peer executable exists
    if [[ -x "$PEER_PATH" ]]; then
        echo "peer executable found at $PEER_PATH"

        # Extract version string
        VERSION_OUTPUT=$("$PEER_PATH" version 2>/dev/null)
        CURRENT_VERSION=$(echo "$VERSION_OUTPUT" | grep -E '^ Version:' | awk '{print $2}')

        echo "Current peer version: $CURRENT_VERSION"

        # Compare with reference version
        if [[ "$CURRENT_VERSION" == "$REFERENCE_VERSION" ]]; then
            echo "Version matches reference ($REFERENCE_VERSION). No action needed."
            found=true
        else
            echo "Version mismatch. Expected $REFERENCE_VERSION, found $CURRENT_VERSION."
            rm -rf "$FABRIC_SAMPLES_PATH"
        fi
    fi

    # Download if not present or wrong version
    if [[ "$found" = false ]]; then
        echo "Downloading fabric samples..."

        pushd ./testdata || { echo "Failed to enter ./testdata"; exit 1; }

        echo "Downloading and running install-fabric.sh..."
        curl -sSLO https://raw.githubusercontent.com/hyperledger/fabric/main/scripts/install-fabric.sh && chmod +x install-fabric.sh
        ./install-fabric.sh --fabric-version "${REFERENCE_VERSION#v}"

        popd
    fi
}

# Function to start network and deploy chaincode
start_network_and_deploy_chaincode() {
    echo "Starting Fabric network and deploying chaincode..."
    "$NETWORK_PATH" up createChannel -i "${REFERENCE_VERSION#v}"
    NETWORK_STARTED=true
    "$NETWORK_PATH" deployCCAAS -ccn basic -ccp "$(realpath "${FABRIC_SAMPLES_PATH}/asset-transfer-basic/chaincode-external")"
}

# Cleanup function to bring down the network if started
cleanup_network() {
    if [[ "$NETWORK_STARTED" = true ]]; then
        echo "Cleaning up: Bringing down Fabric network..."
        if [[ -f "$NETWORK_PATH" ]]; then
            "$NETWORK_PATH" down
        fi
        docker kill peer0org2_basic_ccaas peer0org1_basic_ccaas 2>/dev/null || true
        if [[ -f "$NETWORK_PATH" ]]; then
            "$NETWORK_PATH" down
        fi
    fi
}

# Setup trap for cleanup
setup_cleanup_trap() {
    trap cleanup_network EXIT
}
