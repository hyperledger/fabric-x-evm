/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

// DebugPanic panics with msg unless the binary is built with -tags release. Call
// it on error paths that should never happen, after logging and before returning
// the error.
func DebugPanic(msg string) {
	if DebugBuild {
		panic(msg)
	}
}
