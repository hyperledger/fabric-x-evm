/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"encoding/json"

	"github.com/hyperledger/fabric-x-evm/api/endorsementpb"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/state"
	"google.golang.org/protobuf/proto"
)

// EVMTx returns the Ethereum transaction bytes of tx, or false if tx is not an
// EVM transaction.
func EVMTx(tx blocks.Transaction) ([]byte, bool) {
	if tx.EventName != ProposalTypeEVMTx || len(tx.InputArgs) != 1 {
		return nil, false
	}
	return tx.InputArgs[0], true
}

// MarshalExecutionMetadata encodes the execution outcome for a transaction's
// Payload. Encoding is deterministic so every endorser produces the same bytes.
func MarshalExecutionMetadata(md *endorsementpb.ExecutionMetadata) ([]byte, error) {
	return proto.MarshalOptions{Deterministic: true}.Marshal(md)
}

// UnmarshalExecutionMetadata decodes the execution outcome from a transaction's Payload.
func UnmarshalExecutionMetadata(payload []byte) (*endorsementpb.ExecutionMetadata, error) {
	md := &endorsementpb.ExecutionMetadata{}
	if err := proto.Unmarshal(payload, md); err != nil {
		return nil, err
	}
	return md, nil
}

// ExecutionOutcome reports whether a committed EVM transaction succeeded and the
// gas it used. A transaction Fabric did not commit never ran on-chain, so it used
// no gas; neither did one whose Payload does not decode.
func ExecutionOutcome(tx blocks.Transaction) (succeeded bool, gasUsed uint64) {
	if !tx.Valid() {
		return false, 0
	}
	md, err := UnmarshalExecutionMetadata(tx.Payload)
	if err != nil {
		return false, 0
	}
	return md.GetStatus() == endorsementpb.ExecutionStatus_EXECUTION_STATUS_SUCCESS, md.GetGasUsed()
}

// UnmarshalLogs converts a transaction's event payload back to a list of logs.
// Only a successful transaction carries logs.
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
