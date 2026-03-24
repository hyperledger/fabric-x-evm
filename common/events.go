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
