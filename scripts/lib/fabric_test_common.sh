#!/bin/bash
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0
# Shared functions for Fabric test scripts using Fablo

# Configuration
FABLO_DIR="./testdata/fablo"
FABLO_PATH="$FABLO_DIR/fablo"
FABLO_CONFIG="$FABLO_DIR/fablo-config.json"
FABLO_TARGET="$FABLO_DIR/fablo-target"
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

# Function to verify Fablo binary and config exist
check_fablo_setup() {
    if [[ ! -x "$FABLO_PATH" ]]; then
        echo "Error: Fablo binary not found at $FABLO_PATH"
        exit 1
    fi
    if [[ ! -f "$FABLO_CONFIG" ]]; then
        echo "Error: Fablo config not found at $FABLO_CONFIG"
        exit 1
    fi
    echo "✓ Fablo binary and config verified"
}

# Function to start network with Fablo
start_network_and_deploy_chaincode() {
    echo "Starting Fabric network with Fablo..."

    check_fablo_setup

    # Clean up any previous fablo-target
    echo "Stopping any previously running Fablo network..."
    cd "$FABLO_DIR" || { echo "Failed to enter fablo dir for cleanup"; exit 1; }
    ./fablo down || true
    cd - > /dev/null || exit 1

    if [[ -d "$FABLO_TARGET" ]]; then
        echo "Cleaning up previous fablo-target..."
        rm -rf "$FABLO_TARGET"
    fi

    # Bring up the network (generates artifacts, creates channel, deploys chaincode)
    echo "Bringing up network with Fablo..."
    cd "$FABLO_DIR" || { echo "Failed to enter fablo dir"; exit 1; }
    ./fablo up fablo-config.json || { echo "Failed to start network"; exit 1; }
    cd - > /dev/null || exit 1

    NETWORK_STARTED=true
    echo "✓ Network started successfully"
}

# Cleanup function to bring down the network if started
cleanup_network() {
    if [[ "$NETWORK_STARTED" = true ]]; then
        echo "Cleaning up: Bringing down Fabric network..."
        cd "$FABLO_DIR" || { echo "Failed to enter fablo dir for cleanup"; return; }
        ./fablo down || true
        cd - > /dev/null || true
    fi
}

# Setup trap for cleanup
setup_cleanup_trap() {
    trap cleanup_network EXIT
}
