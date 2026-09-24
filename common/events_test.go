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

	"github.com/hyperledger/fabric-x-sdk/state"
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

// ---- event names ----
//
// The SDK carries the event name beside the payload on both backends, so revert
// and exec-failure detection is a prefix check rather than a proto unwrap.

func TestRevertEventName_IncludesTxID(t *testing.T) {
	name := RevertEventName("tx-1")
	if !strings.HasPrefix(name, "revert:") || !strings.HasSuffix(name, "tx-1") {
		t.Errorf("RevertEventName = %q", name)
	}
	if !IsRevertEvent(name) {
		t.Error("IsRevertEvent must accept the name it builds")
	}
	if IsExecFailureEvent(name) {
		t.Error("a revert must not read as an exec failure")
	}
}

func TestExecFailureEventName_IncludesTxID(t *testing.T) {
	name := ExecFailureEventName("tx-2")
	if !strings.HasPrefix(name, "execfail:") || !strings.HasSuffix(name, "tx-2") {
		t.Errorf("ExecFailureEventName = %q", name)
	}
	if !IsExecFailureEvent(name) {
		t.Error("IsExecFailureEvent must accept the name it builds")
	}
	if IsRevertEvent(name) {
		t.Error("an exec failure must not read as a revert")
	}
}

// A successful transaction carries the SDK's default name, which must not be
// mistaken for either failure marker.
func TestDefaultEventName_IsNeitherFailure(t *testing.T) {
	for _, name := range []string{"", "event", "log", "Transfer"} {
		if IsRevertEvent(name) {
			t.Errorf("%q must not read as a revert", name)
		}
		if IsExecFailureEvent(name) {
			t.Errorf("%q must not read as an exec failure", name)
		}
	}
}
