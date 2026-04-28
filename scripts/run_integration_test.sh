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
start_network_and_deploy_chaincode

# Run integration tests
# Keep this configurable so callers can centralize timeout values and ensure
# any outer wall timeout is greater than or equal to the Go test timeout.
GO_TEST_TIMEOUT="${GO_TEST_TIMEOUT:-120s}"
go test -timeout "$GO_TEST_TIMEOUT" $VERBOSE_FLAG -run '^TestFablo$' ./integration/