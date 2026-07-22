/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/grpc/grpclog"
)

// executionSpecForks limits the suite to Osaka-forward, plus Prague/Cancun as cheap regression.
var executionSpecForks = map[string]struct{}{
	"Osaka":  {},
	"BPO1":   {},
	"BPO2":   {},
	"Prague": {},
	"Cancun": {},
}

// TestExecutionSpecStateTests runs the execution-specs state_tests through the shared harness, limited to executionSpecForks.
func TestExecutionSpecStateTests(t *testing.T) {
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, os.Stderr, os.Stderr)) // disable grpc logging

	testsDir := filepath.Join("..", "testdata", "execution-specs-tests", "fixtures", "state_tests")
	if _, err := os.Stat(testsDir); os.IsNotExist(err) {
		t.Skipf("execution-specs fixtures not found at %s; run `make fetch-execution-specs-tests`", testsDir)
	}

	skip, err := loadSkip(filepath.Join("..", "testdata", "execution_specs_tests.skip"))
	if err != nil {
		t.Fatalf("Failed to load skip list: %v", err)
	}
	t.Logf("Loaded skip list with %d entries", len(skip))

	slow, err := loadSlow(filepath.Join("..", "testdata", "execution_specs_tests.slow"))
	if err != nil {
		t.Fatalf("Failed to load slow list: %v", err)
	}
	t.Logf("Loaded slow list with %d entries", len(slow))

	allFiles, err := findJSONFiles(testsDir)
	if err != nil {
		t.Fatalf("Failed to find test files: %v", err)
	}
	allFiles = filterSkippedTests(allFiles, skip)
	testFiles := filterSlowTests(allFiles, slow, *want_very_slow)
	t.Logf("Running %d execution-specs state_tests files after filtering", len(testFiles))

	for _, testPath := range testFiles {
		t.Run(filepath.Base(testPath), func(t *testing.T) {
			runExecutionSpecStateTestFile(t, testPath)
		})
	}
}

// runExecutionSpecStateTestFile runs one fixtures file's state tests against the fork allowlist.
func runExecutionSpecStateTestFile(t *testing.T, path string) {
	tests, err := ParseTestFile(path)
	if err != nil {
		t.Fatalf("Failed to parse test file: %v", err)
	}
	for name, test := range tests {
		// EEST bakes the fork into the test name, so skip a test with no allowlisted fork entirely.
		if !hasAllowlistedFork(test, executionSpecForks) {
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runStateTestSubtests(t, test, executionSpecForks)
		})
	}
}

// hasAllowlistedFork reports whether any subtest targets a fork in the allowlist.
func hasAllowlistedFork(test *StateTest, forkAllowlist map[string]struct{}) bool {
	for _, subtest := range test.Subtests() {
		if _, ok := forkAllowlist[subtest.Fork]; ok {
			return true
		}
	}
	return false
}
