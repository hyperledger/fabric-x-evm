/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

// CallError is the application outcome of a failed read-only call, carrying the
// same status/message/payload the endorser would otherwise put in a response.
// It lets the endorser report a revert or an execution failure without the
// gateway's error types, which it cannot import.
type CallError struct {
	Status  int32
	Message string
	// Data is the revert payload; set only for StatusEVMRevert.
	Data []byte
}

func (e *CallError) Error() string { return e.Message }

// Reverted reports whether the call failed with an EVM revert.
func (e *CallError) Reverted() bool { return e.Status == StatusEVMRevert }
