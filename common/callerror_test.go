/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import "testing"

func TestCallErrorReverted(t *testing.T) {
	tests := []struct {
		name   string
		status int32
		want   bool
	}{
		{"revert", StatusEVMRevert, true},
		{"exec failure", StatusExecFailure, false},
		{"rejected", StatusTxRejected, false},
		{"server error", StatusServerError, false},
		{"ok", StatusOK, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &CallError{Status: tt.status}
			if got := e.Reverted(); got != tt.want {
				t.Errorf("Reverted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCallErrorError(t *testing.T) {
	e := &CallError{Status: StatusEVMRevert, Message: "execution reverted: bad"}
	if e.Error() != "execution reverted: bad" {
		t.Errorf("Error() = %q, want %q", e.Error(), "execution reverted: bad")
	}
}
