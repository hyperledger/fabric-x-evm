#!/bin/bash
# Copyright IBM Corp. All Rights Reserved.
#
# SPDX-License-Identifier: Apache-2.0

# Source shared functions
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/lib/fabric_test_common.sh"

# Parse arguments and validate environment
parse_verbose_flag "$@"
check_project_root "run_integration_test.sh"
ensure_testdata_dir

# Setup cleanup trap
setup_cleanup_trap

# Execute steps
check_and_download_fabric_samples
start_network_and_deploy_chaincode

# Run integration tests
go test $VERBOSE_FLAG -run 'Fabric$' ./integration/