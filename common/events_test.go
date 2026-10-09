/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/hyperledger/fabric-x-evm/api/endorsementpb"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/state"
	"google.golang.org/protobuf/proto"
)

func TestUnmarshalEvents(t *testing.T) {
	tests := []struct {
		name    string
		logs    []state.Log
		wantErr bool
	}{
		{
			name: "empty input",
			logs: nil,
		},
		{
			name: "single log",
			logs: []state.Log{
				{
					Address: []byte{0x01, 0x02, 0x03},
					Topics:  [][]byte{{0x0a, 0x0b}, {0x0c, 0x0d}},
					Data:    []byte{0xff, 0xfe},
				},
			},
		},
		{
			name: "multiple logs",
			logs: []state.Log{
				{
					Address: []byte{0x01},
					Topics:  [][]byte{{0x0a}},
					Data:    []byte{0xff},
				},
				{
					Address: []byte{0x02},
					Topics:  [][]byte{{0x0b}, {0x0c}},
					Data:    []byte{0xee, 0xdd},
				},
			},
		},
		{
			name: "log with empty fields",
			logs: []state.Log{
				{
					Address: []byte{},
					Topics:  [][]byte{},
					Data:    []byte{},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The endorser emits the JSON logs as the event bytes; the SDK
			// carries them through to the block unchanged.
			event, _ := json.Marshal(tt.logs)

			got, err := UnmarshalLogs(event)
			if (err != nil) != tt.wantErr {
				t.Fatalf("UnmarshalEvents() error = %v, wantErr %v", err, tt.wantErr)
			}

			// For nil/empty input, expect empty slice
			if tt.logs == nil {
				if len(got) != 0 {
					t.Errorf("expected empty slice, got %v", got)
				}
				return
			}

			if len(got) != len(tt.logs) {
				t.Fatalf("got %d logs, want %d", len(got), len(tt.logs))
			}

			for i := range tt.logs {
				if !bytes.Equal(got[i].Address, tt.logs[i].Address) {
					t.Errorf("log[%d].Address = %v, want %v", i, got[i].Address, tt.logs[i].Address)
				}
				if len(got[i].Topics) != len(tt.logs[i].Topics) {
					t.Errorf("log[%d].Topics length = %d, want %d", i, len(got[i].Topics), len(tt.logs[i].Topics))
				}
				for j := range tt.logs[i].Topics {
					if !bytes.Equal(got[i].Topics[j], tt.logs[i].Topics[j]) {
						t.Errorf("log[%d].Topics[%d] = %v, want %v", i, j, got[i].Topics[j], tt.logs[i].Topics[j])
					}
				}
				if !bytes.Equal(got[i].Data, tt.logs[i].Data) {
					t.Errorf("log[%d].Data = %v, want %v", i, got[i].Data, tt.logs[i].Data)
				}
			}
		})
	}
}

func TestUnmarshalEvents_InvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{
			name:  "not json",
			input: []byte{0xff, 0xff, 0xff},
		},
		{
			name:  "truncated json",
			input: []byte(`[{"Address":`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := UnmarshalLogs(tt.input)
			if err == nil {
				t.Error("expected error for invalid input")
			}
		})
	}
}

// ---- UnmarshalLogs: empty and empty-payload branches ----

func TestUnmarshalLogs_EmptyInputReturnsEmptySlice(t *testing.T) {
	got, err := UnmarshalLogs(nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("want empty slice, got %v", got)
	}
}

func TestUnmarshalLogs_EmptyJSONArrayReturnsEmptySlice(t *testing.T) {
	got, err := UnmarshalLogs([]byte("[]"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("want empty slice, got %v", got)
	}
}

// ---- EVM transaction marker ----

func TestEVMTx(t *testing.T) {
	raw := []byte{0xaa, 0xbb}
	tests := []struct {
		name string
		tx   blocks.Transaction
		ok   bool
	}{
		{"evm tx", blocks.Transaction{EventName: ProposalTypeEVMTx, InputArgs: [][]byte{raw}}, true},
		{"other event name", blocks.Transaction{EventName: "event", InputArgs: [][]byte{raw}}, false},
		{"no event name", blocks.Transaction{InputArgs: [][]byte{raw}}, false},
		{"no args", blocks.Transaction{EventName: ProposalTypeEVMTx}, false},
		{"old two-arg layout", blocks.Transaction{EventName: ProposalTypeEVMTx, InputArgs: [][]byte{{0xfb}, raw}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := EVMTx(tt.tx)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && !bytes.Equal(got, raw) {
				t.Errorf("tx bytes = %x, want %x", got, raw)
			}
		})
	}
}

// ---- execution metadata ----

func TestExecutionMetadata_RoundTrip(t *testing.T) {
	in := &endorsementpb.ExecutionMetadata{
		Status:    endorsementpb.ExecutionStatus_EXECUTION_STATUS_REVERTED,
		Reason:    []byte{0x08, 0xc3, 0x79, 0xa0},
		GasUsed:   21_000,
		Timestamp: 1_700_000_000,
	}
	b, err := MarshalExecutionMetadata(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := UnmarshalExecutionMetadata(b)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !proto.Equal(in, out) {
		t.Errorf("round trip = %v, want %v", out, in)
	}

	// Every endorser must produce the same bytes for the same outcome.
	again, err := MarshalExecutionMetadata(in)
	if err != nil || !bytes.Equal(b, again) {
		t.Errorf("marshal is not stable: %x vs %x (err %v)", b, again, err)
	}
}

// An empty Payload decodes to the unspecified status, never to success.
func TestUnmarshalExecutionMetadata_EmptyIsUnspecified(t *testing.T) {
	md, err := UnmarshalExecutionMetadata(nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if md.GetStatus() != endorsementpb.ExecutionStatus_EXECUTION_STATUS_UNSPECIFIED {
		t.Errorf("status = %v, want unspecified", md.GetStatus())
	}
}

func TestExecutionOutcome(t *testing.T) {
	payload := func(status endorsementpb.ExecutionStatus, gas uint64) []byte {
		b, err := MarshalExecutionMetadata(&endorsementpb.ExecutionMetadata{Status: status, GasUsed: gas})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	tests := []struct {
		name      string
		tx        blocks.Transaction
		succeeded bool
		gas       uint64
	}{
		{"success", blocks.Transaction{Status: blocks.StatusCommitted, Payload: payload(endorsementpb.ExecutionStatus_EXECUTION_STATUS_SUCCESS, 21000)}, true, 21000},
		{"revert", blocks.Transaction{Status: blocks.StatusCommitted, Payload: payload(endorsementpb.ExecutionStatus_EXECUTION_STATUS_REVERTED, 30000)}, false, 30000},
		{"exec failure", blocks.Transaction{Status: blocks.StatusCommitted, Payload: payload(endorsementpb.ExecutionStatus_EXECUTION_STATUS_EXEC_FAILED, 50000)}, false, 50000},
		{"not committed", blocks.Transaction{Status: blocks.StatusMVCCConflict, Payload: payload(endorsementpb.ExecutionStatus_EXECUTION_STATUS_SUCCESS, 21000)}, false, 0},
		{"no payload", blocks.Transaction{Status: blocks.StatusCommitted}, false, 0},
		{"bad payload", blocks.Transaction{Status: blocks.StatusCommitted, Payload: []byte{0xff, 0xff}}, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			succeeded, gas := ExecutionOutcome(tt.tx)
			if succeeded != tt.succeeded || gas != tt.gas {
				t.Errorf("got (%v, %d), want (%v, %d)", succeeded, gas, tt.succeeded, tt.gas)
			}
		})
	}
}

func TestUnmarshalExecutionMetadata_InvalidInput(t *testing.T) {
	if _, err := UnmarshalExecutionMetadata([]byte{0xff, 0xff, 0xff}); err == nil {
		t.Error("expected error for invalid input")
	}
}
