/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later

WARNING: This package contains test-only/unsafe RPC implementations.
DO NOT use in production environments.
*/

package testimpl

import (
	"context"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-evm/gateway/api"
)

func TestTestEthAPI_Accounts(t *testing.T) {
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
			// Create production API
			prodAPI := api.NewEthAPI(nil)
			// Wrap with test API
			testAPI := NewTestEthAPI(prodAPI, nil, tt.testAccounts, nil)

			accounts, err := testAPI.Accounts(context.TODO())
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

func TestTestEthAPI_SendTransaction_Validation(t *testing.T) {
	// Load test accounts from JSON file
	testAccountMgr, err := LoadTestAccounts("../../testdata/test_accounts.json")
	if err != nil {
		t.Fatalf("Failed to load test accounts: %v", err)
	}

	// Use first account for testing
	testAddr := testAccountMgr.Addresses[0]
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

			// Create production API
			prodAPI := api.NewEthAPI(mockBackend)
			// Wrap with test API
			testAPI := NewTestEthAPI(prodAPI, mockBackend, testAccountMgr.Addresses, testAccountMgr.PrivateKeys)

			_, err := testAPI.SendTransaction(context.TODO(), tt.args)

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
	api.Backend
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
