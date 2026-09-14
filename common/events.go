/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"encoding/json"
	"strings"

	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-sdk/state"
	"google.golang.org/protobuf/proto"
)

func MarshalLogs(logs []byte, namespace, txID string) ([]byte, error) {
	if len(logs) == 0 {
		return nil, nil
	}
	return proto.Marshal(&peer.ChaincodeEvent{
		Payload:     logs,
		ChaincodeId: namespace,
		TxId:        txID,
		EventName:   "log",
	})
}

// UnmarshalLogs takes a proto-marshaled ChaincodeEvent and converts it
// back to a list of logs.
func UnmarshalLogs(event []byte) ([]state.Log, error) {
	if len(event) == 0 {
		return []state.Log{}, nil
	}

	var ev peer.ChaincodeEvent
	if err := proto.Unmarshal(event, &ev); err != nil {
		return nil, err
	}

	if len(ev.Payload) == 0 {
		return []state.Log{}, nil
	}

	var logs []state.Log
	if err := json.Unmarshal(ev.Payload, &logs); err != nil {
		return nil, err
	}

	return logs, nil
}

// eventNameRevertPrefix is the prefix of the inner ChaincodeEvent name used
// to signal an EVM revert. The Fabric txid is appended so the full name is
// unique per transaction and cannot be forged by an EVM contract.
const eventNameRevertPrefix = "revert:"

// eventNameExecFailurePrefix marks a valid tx whose EVM execution otherwise
// faulted (out of gas, invalid opcode, ...) - mined like a revert, but with
// no ABI-encoded reason to carry.
const eventNameExecFailurePrefix = "execfail:"

// MarshalRevert wraps the raw revert payload in a ChaincodeEvent whose name
// is "revert:<txID>" so the committer can detect the revert and the marker
// cannot collide with any name an EVM contract could produce.
func MarshalRevert(payload []byte, namespace, txID string) ([]byte, error) {
	return proto.Marshal(&peer.ChaincodeEvent{
		Payload:     payload,
		ChaincodeId: namespace,
		TxId:        txID,
		EventName:   eventNameRevertPrefix + txID,
	})
}

// MarshalExecFailure wraps the raw EVM return data (typically empty) in a
// ChaincodeEvent whose name is "execfail:<txID>", the same shape MarshalRevert
// uses for a revert.
func MarshalExecFailure(payload []byte, namespace, txID string) ([]byte, error) {
	return proto.Marshal(&peer.ChaincodeEvent{
		Payload:     payload,
		ChaincodeId: namespace,
		TxId:        txID,
		EventName:   eventNameExecFailurePrefix + txID,
	})
}

// innerEventName unwraps the outer/inner ChaincodeEvent nesting the SDK
// Endorse builder produces (outer EventName "log") and returns the inner
// event's name, or ok=false if event isn't that shape.
func innerEventName(event []byte) (name string, ok bool) {
	if len(event) == 0 {
		return "", false
	}
	var outer peer.ChaincodeEvent
	if err := proto.Unmarshal(event, &outer); err != nil {
		return "", false
	}
	var inner peer.ChaincodeEvent
	if err := proto.Unmarshal(outer.Payload, &inner); err != nil {
		return "", false
	}
	return inner.EventName, true
}

// IsRevertEvent reports whether the given event bytes represent an EVM revert.
func IsRevertEvent(event []byte) bool {
	name, ok := innerEventName(event)
	return ok && strings.HasPrefix(name, eventNameRevertPrefix)
}

// IsExecFailureEvent reports whether the given event bytes represent a valid
// tx whose EVM execution faulted without reverting (out of gas, invalid
// opcode, ...).
func IsExecFailureEvent(event []byte) bool {
	name, ok := innerEventName(event)
	return ok && strings.HasPrefix(name, eventNameExecFailurePrefix)
}
