/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"bytes"
	"encoding/json"
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
