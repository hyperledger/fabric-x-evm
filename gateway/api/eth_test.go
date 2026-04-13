/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
)

// testAccount represents a test account for loading from JSON
type testAccount struct {
	Address    string `json:"address"`
	PrivateKey string `json:"privateKey"`
}

// testAccountsFile represents the structure of the test accounts JSON file
type testAccountsFile struct {
	Accounts []testAccount `json:"accounts"`
}

// loadTestAccountsFromFile loads test accounts from JSON and returns addresses and pre-converted keys
func loadTestAccountsFromFile(path string) ([]common.Address, map[common.Address]*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	var accountsFile testAccountsFile
	if err := json.Unmarshal(data, &accountsFile); err != nil {
		return nil, nil, err
	}

	addresses := make([]common.Address, len(accountsFile.Accounts))
	keys := make(map[common.Address]*ecdsa.PrivateKey)

	for i, acc := range accountsFile.Accounts {
		addr := common.HexToAddress(acc.Address)
		addresses[i] = addr

		privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(acc.PrivateKey, "0x"))
		if err != nil {
			return nil, nil, err
		}
		keys[addr] = privateKey
	}

	return addresses, keys, nil
}

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
		{"negative", -1, 0},
		{"large negative", -100, 0},
		{"pending", rpc.PendingBlockNumber, 0},
		{"latest", rpc.LatestBlockNumber, 0},
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

func TestEthAPI_Accounts(t *testing.T) {
	tests := []struct {
		name         string
		testAccounts []common.Address
		wantCount    int
	}{
		{
			name: "multiple accounts",
			testAccounts: []common.Address{
				common.HexToAddress("0x1234567890123456789012345678901234567890"),
				common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"),
				common.HexToAddress("0x9876543210987654321098765432109876543210"),
			},
			wantCount: 3,
		},
		{
			name:         "single account",
			testAccounts: []common.Address{common.HexToAddress("0x1234567890123456789012345678901234567890")},
			wantCount:    1,
		},
		{
			name:         "no accounts",
			testAccounts: []common.Address{},
			wantCount:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := NewEthAPI(nil, tt.testAccounts, nil)

			accounts, err := api.Accounts(context.TODO())
			if err != nil {
				t.Fatalf("Accounts() error = %v", err)
			}

			if len(accounts) != tt.wantCount {
				t.Errorf("Accounts() returned %d accounts, want %d", len(accounts), tt.wantCount)
			}

			// Verify addresses match
			for i, addr := range accounts {
				if addr != tt.testAccounts[i] {
					t.Errorf("Account[%d] = %v, want %v", i, addr, tt.testAccounts[i])
				}
			}
		})
	}
}

func TestEthAPI_SendTransaction_Validation(t *testing.T) {
	// Load test accounts from JSON file
	testAccounts, testKeys, err := loadTestAccountsFromFile("../../testdata/test-accounts.json")
	if err != nil {
		t.Fatalf("Failed to load test accounts: %v", err)
	}

	// Use first account for testing
	testAddr := testAccounts[0]
	unknownAddr := common.HexToAddress("0x0000000000000000000000000000000000000000")
	toAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")

	tests := []struct {
		name    string
		args    TransactionArgs
		wantErr bool
		errMsg  string
	}{
		{
			name: "missing from address",
			args: TransactionArgs{
				To: &toAddr,
			},
			wantErr: true,
			errMsg:  "missing 'from' field",
		},
		{
			name: "unknown from address",
			args: TransactionArgs{
				From: &unknownAddr,
				To:   &toAddr,
			},
			wantErr: true,
			errMsg:  "no private key available",
		},
		{
			name: "valid transaction parameters",
			args: TransactionArgs{
				From: &testAddr,
				To:   &toAddr,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create API with mock backend that accepts any transaction
			mockBackend := &mockBackend{
				sendTxFunc: func(tx *types.Transaction) error {
					return nil
				},
			}

			api := NewEthAPI(mockBackend, testAccounts, testKeys)

			_, err := api.SendTransaction(context.TODO(), tt.args)

			if tt.wantErr {
				if err == nil {
					t.Errorf("SendTransaction() expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("SendTransaction() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("SendTransaction() unexpected error = %v", err)
				}
			}
		})
	}
}

// mockBackend is a minimal Backend implementation for testing
type mockBackend struct {
	Backend
	sendTxFunc func(*types.Transaction) error
}

func (m *mockBackend) SendTransaction(_ context.Context, tx *types.Transaction) error {
	if m.sendTxFunc != nil {
		return m.sendTxFunc(tx)
	}
	return nil
}

func (m *mockBackend) ChainID(_ context.Context) (*big.Int, error) {
	return big.NewInt(31337), nil
}

func (m *mockBackend) NonceAt(_ context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	return 0, nil // Return nonce 0 for testing
}
