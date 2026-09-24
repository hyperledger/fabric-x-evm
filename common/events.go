/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"encoding/json"
	"strings"

	"github.com/hyperledger/fabric-x-sdk/state"
)

// eventNameRevertPrefix is the prefix of the event name used to signal an EVM
// revert. The transaction hash is appended so the full name is unique per
// transaction and cannot be forged by an EVM contract.
const eventNameRevertPrefix = "revert:"

// eventNameExecFailurePrefix marks a valid tx whose EVM execution otherwise
// faulted (out of gas, invalid opcode, ...) - mined like a revert, but with
// no ABI-encoded reason to carry.
const eventNameExecFailurePrefix = "execfail:"

// RevertEventName is the event name the endorser sets on an EVM revert.
func RevertEventName(txID string) string { return eventNameRevertPrefix + txID }

// ExecFailureEventName is the event name the endorser sets on an execution
// failure that did not revert.
func ExecFailureEventName(txID string) string { return eventNameExecFailurePrefix + txID }

// IsRevertEvent reports whether an event name signals an EVM revert.
func IsRevertEvent(eventName string) bool {
	return strings.HasPrefix(eventName, eventNameRevertPrefix)
}

// IsExecFailureEvent reports whether an event name signals a valid tx whose
// EVM execution faulted without reverting.
func IsExecFailureEvent(eventName string) bool {
	return strings.HasPrefix(eventName, eventNameExecFailurePrefix)
}

// UnmarshalLogs converts a transaction's event payload back to a list of logs.
// Only a successful transaction carries logs here; a revert carries its return
// data instead, so callers must check the event name first.
func UnmarshalLogs(event []byte) ([]state.Log, error) {
	if len(event) == 0 {
		return []state.Log{}, nil
	}

	var logs []state.Log
	if err := json.Unmarshal(event, &logs); err != nil {
		return nil, err
	}

	return logs, nil
}
