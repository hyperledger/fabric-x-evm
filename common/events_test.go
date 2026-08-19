/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
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
			// Marshal the logs
			logs, _ := json.Marshal(tt.logs)
			event, err := MarshalLogs(logs, "chaincode", "tx123")
			if err != nil {
				t.Fatalf("MarshalEvents() error = %v", err)
			}

			// Unmarshal them back
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
			name:  "invalid proto",
			input: []byte{0xff, 0xff, 0xff},
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

// ---- MarshalLogs: early-return branch ----

func TestMarshalLogs_EmptyLogsReturnsNil(t *testing.T) {
	out, err := MarshalLogs(nil, "chaincode", "tx-1")
	if err != nil {
		t.Fatalf("MarshalLogs err: %v", err)
	}
	if out != nil {
		t.Errorf("want nil, got %v", out)
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

func TestUnmarshalLogs_EmptyPayloadReturnsEmptySlice(t *testing.T) {
	// A ChaincodeEvent with no Payload should yield an empty slice, not an error.
	ev := &peer.ChaincodeEvent{ChaincodeId: "cc", TxId: "tx-1", EventName: "log"}
	b, err := proto.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal setup: %v", err)
	}
	got, err := UnmarshalLogs(b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("want empty slice, got %v", got)
	}
}

func TestUnmarshalLogs_BadJSONPayloadReturnsError(t *testing.T) {
	// A well-formed ChaincodeEvent whose Payload is not valid JSON should error.
	ev := &peer.ChaincodeEvent{Payload: []byte{0xff, 0xff}, ChaincodeId: "cc", TxId: "tx-1"}
	b, err := proto.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal setup: %v", err)
	}
	if _, err := UnmarshalLogs(b); err == nil {
		t.Fatal("expected json.Unmarshal error")
	}
}

// ---- MarshalRevert ----

func TestMarshalRevert_SetsEventNameWithTxID(t *testing.T) {
	txID := "tx-abc"
	payload := []byte("revert-data")
	out, err := MarshalRevert(payload, "cc", txID)
	if err != nil {
		t.Fatalf("MarshalRevert err: %v", err)
	}
	var got peer.ChaincodeEvent
	if err := proto.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal roundtrip: %v", err)
	}
	if got.EventName != "revert:"+txID {
		t.Errorf("EventName = %q, want %q", got.EventName, "revert:"+txID)
	}
	if !strings.HasPrefix(got.EventName, "revert:") {
		t.Errorf("EventName missing revert: prefix")
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Errorf("Payload = %x, want %x", got.Payload, payload)
	}
	if got.TxId != txID {
		t.Errorf("TxId = %q, want %q", got.TxId, txID)
	}
}

// ---- IsRevertEvent ----

func TestIsRevertEvent_EmptyReturnsFalse(t *testing.T) {
	if IsRevertEvent(nil) {
		t.Error("nil should be false")
	}
	if IsRevertEvent([]byte{}) {
		t.Error("empty slice should be false")
	}
}

func TestIsRevertEvent_BadOuterProtoReturnsFalse(t *testing.T) {
	if IsRevertEvent([]byte{0xff, 0xff, 0xff}) {
		t.Error("garbage bytes should be false")
	}
}

func TestIsRevertEvent_BadInnerProtoReturnsFalse(t *testing.T) {
	// Outer parses fine but its Payload is not a valid ChaincodeEvent.
	outer := &peer.ChaincodeEvent{Payload: []byte{0xff, 0xff}, EventName: "log"}
	b, err := proto.Marshal(outer)
	if err != nil {
		t.Fatalf("marshal setup: %v", err)
	}
	if IsRevertEvent(b) {
		t.Error("bad inner proto should be false")
	}
}

func TestIsRevertEvent_WithRevertPrefixReturnsTrue(t *testing.T) {
	// Build the wire shape MarshalRevert wraps into: an outer ChaincodeEvent
	// whose Payload is a marshalled inner ChaincodeEvent with the revert: prefix.
	inner, err := MarshalRevert([]byte("payload"), "cc", "tx-1")
	if err != nil {
		t.Fatalf("MarshalRevert: %v", err)
	}
	outer, err := proto.Marshal(&peer.ChaincodeEvent{Payload: inner, EventName: "log"})
	if err != nil {
		t.Fatalf("outer marshal: %v", err)
	}
	if !IsRevertEvent(outer) {
		t.Error("expected revert event detected")
	}
}

func TestIsRevertEvent_WithoutRevertPrefixReturnsFalse(t *testing.T) {
	// Inner EventName is "log", not "revert:*".
	inner, err := proto.Marshal(&peer.ChaincodeEvent{Payload: []byte("x"), EventName: "log"})
	if err != nil {
		t.Fatalf("inner marshal: %v", err)
	}
	outer, err := proto.Marshal(&peer.ChaincodeEvent{Payload: inner, EventName: "log"})
	if err != nil {
		t.Fatalf("outer marshal: %v", err)
	}
	if IsRevertEvent(outer) {
		t.Error("non-revert event should be false")
	}
}
