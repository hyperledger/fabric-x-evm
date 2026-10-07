/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDebugPanic(t *testing.T) {
	if DebugBuild {
		assert.PanicsWithValue(t, "boom", func() { DebugPanic("boom") })
		return
	}
	assert.NotPanics(t, func() { DebugPanic("boom") })
}
