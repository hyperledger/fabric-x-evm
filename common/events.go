/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"encoding/json"

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

// EventNameRevert is the ChaincodeEvent name used to signal an EVM revert
// from the endorser to the committer, so receipts can be marked status=0.
const EventNameRevert = "revert"

// MarshalRevert wraps the raw revert payload in a ChaincodeEvent with name
// "revert" so the committer can detect the revert via the block-parsed events.
func MarshalRevert(payload []byte, namespace, txID string) ([]byte, error) {
	return proto.Marshal(&peer.ChaincodeEvent{
		Payload:     payload,
		ChaincodeId: namespace,
		TxId:        txID,
		EventName:   EventNameRevert,
	})
}

// IsRevertEvent reports whether the given event bytes represent an EVM revert.
// The SDK Endorse builder wraps res.Event in an outer ChaincodeEvent
// (EventName "log"), so the marker we set in the inner event is one
// proto-unmarshal deeper.
func IsRevertEvent(event []byte) bool {
	if len(event) == 0 {
		return false
	}
	var outer peer.ChaincodeEvent
	if err := proto.Unmarshal(event, &outer); err != nil {
		return false
	}
	var inner peer.ChaincodeEvent
	if err := proto.Unmarshal(outer.Payload, &inner); err != nil {
		return false
	}
	return inner.EventName == EventNameRevert
}
