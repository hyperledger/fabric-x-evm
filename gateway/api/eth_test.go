/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
)

func TestRpcBlockNumberToBigInt(t *testing.T) {
	tests := []struct {
		name string
		num  rpc.BlockNumber
		want *big.Int
	}{
		{"pending", rpc.PendingBlockNumber, nil},
		{"latest", rpc.LatestBlockNumber, nil},
		{"zero", 0, big.NewInt(0)},
		{"positive", 100, big.NewInt(100)},
		{"negative", -10, big.NewInt(-10)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rpcBlockNumberToBigInt(tt.num)
			if (got == nil) != (tt.want == nil) {
				t.Errorf("rpcBlockNumberToBigInt() = %v, want %v", got, tt.want)
				return
			}
			if got != nil && got.Cmp(tt.want) != 0 {
				t.Errorf("rpcBlockNumberToBigInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBlockNumberToUint64(t *testing.T) {
	tests := []struct {
		name string
		num  rpc.BlockNumber
		want uint64
	}{
		{"zero", 0, 0},
		{"positive", 100, 100},
		{"negative", -1, math.MaxUint64},
		{"large negative", -100, math.MaxUint64},
		{"pending", rpc.PendingBlockNumber, math.MaxUint64},
		{"latest", rpc.LatestBlockNumber, math.MaxUint64},
		{"earliest", rpc.EarliestBlockNumber, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := blockNumberToUint64(tt.num)
			if got != tt.want {
				t.Errorf("blockNumberToUint64() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBlockNumberOrHashToBlockNumber(t *testing.T) {
	tests := []struct {
		name      string
		numOrHash rpc.BlockNumberOrHash
		want      *big.Int
	}{
		{"latest", rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber), nil},
		{"pending", rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber), nil},
		{"zero", rpc.BlockNumberOrHashWithNumber(0), big.NewInt(0)},
		{"positive", rpc.BlockNumberOrHashWithNumber(100), big.NewInt(100)},
		{"negative", rpc.BlockNumberOrHashWithNumber(-10), big.NewInt(-10)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := blockNumberOrHashToBlockNumber(tt.numOrHash)
			if (got == nil) != (tt.want == nil) {
				t.Errorf("blockNumberOrHashToBlockNumber() = %v, want %v", got, tt.want)
				return
			}
			if got != nil && got.Cmp(tt.want) != 0 {
				t.Errorf("blockNumberOrHashToBlockNumber() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRPCReceiptMarshalJSON(t *testing.T) {
	fromAddr := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	toAddr := common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")

	tests := []struct {
		name        string
		receipt     *rpcReceipt
		checkTo     func(t *testing.T, m map[string]any)
		checkFields func(t *testing.T, m map[string]any)
	}{
		{
			name: "with to address",
			receipt: &rpcReceipt{
				Receipt: types.Receipt{
					Status:      1,
					BlockNumber: big.NewInt(42),
				},
				From: fromAddr,
				To:   &toAddr,
			},
			checkTo: func(t *testing.T, m map[string]any) {
				if m["to"] != toAddr.Hex() {
					t.Errorf("to = %v, want %v", m["to"], toAddr.Hex())
				}
			},
			checkFields: func(t *testing.T, m map[string]any) {
				if m["from"] != fromAddr.Hex() {
					t.Errorf("from = %v, want %v", m["from"], fromAddr.Hex())
				}
				if m["status"] == nil {
					t.Error("status field not preserved")
				}
			},
		},
		{
			name: "nil to address",
			receipt: &rpcReceipt{
				Receipt: types.Receipt{
					Status:      1,
					BlockNumber: big.NewInt(100),
				},
				From: fromAddr,
				To:   nil,
			},
			checkTo: func(t *testing.T, m map[string]any) {
				if m["to"] != nil {
					t.Errorf("to = %v, want nil", m["to"])
				}
			},
			checkFields: func(t *testing.T, m map[string]any) {
				if m["from"] != fromAddr.Hex() {
					t.Errorf("from = %v, want %v", m["from"], fromAddr.Hex())
				}
				if m["status"] == nil {
					t.Error("status field not preserved")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.receipt)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}

			tt.checkTo(t, m)
			tt.checkFields(t, m)
		})
	}
}
