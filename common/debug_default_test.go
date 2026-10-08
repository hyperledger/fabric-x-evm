//go:build !release

/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import "testing"

// Normal builds, CI included, must debug-panic.
func TestDebugPanicIsOnByDefault(t *testing.T) {
	if !DebugBuild {
		t.Fatal("DebugBuild must be true unless built with -tags release")
	}
}
