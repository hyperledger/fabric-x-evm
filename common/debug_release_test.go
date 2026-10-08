//go:build release

/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import "testing"

// A release build must never debug-panic.
func TestDebugPanicIsOffInRelease(t *testing.T) {
	if DebugBuild {
		t.Fatal("DebugBuild must be false when built with -tags release")
	}
}
