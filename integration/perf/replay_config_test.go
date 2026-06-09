/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import "testing"

func TestLoadReplayWorkerConfigFromEnv(t *testing.T) {
	t.Setenv("PERF_PROCESSING_WORKERS", "16")
	t.Setenv("PERF_SUBMITTING_WORKERS", "16")

	processingWorkers, submittingWorkers := loadReplayWorkerConfigFromEnv(t)

	if processingWorkers != 16 {
		t.Fatalf("processingWorkers = %d, want 16", processingWorkers)
	}
	if submittingWorkers != 16 {
		t.Fatalf("submittingWorkers = %d, want 16", submittingWorkers)
	}
}

func TestLoadReplayWorkerConfigFromEnvDefaultsToOne(t *testing.T) {
	processingWorkers, submittingWorkers := loadReplayWorkerConfigFromEnv(t)

	if processingWorkers != 1 {
		t.Fatalf("processingWorkers = %d, want 1", processingWorkers)
	}
	if submittingWorkers != 1 {
		t.Fatalf("submittingWorkers = %d, want 1", submittingWorkers)
	}
}
