//go:build release

/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

// DebugBuild reports whether DebugPanic panics: true unless built with -tags release.
const DebugBuild = false
