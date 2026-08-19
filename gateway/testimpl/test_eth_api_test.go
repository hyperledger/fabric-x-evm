/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later

WARNING: This package contains test-only/unsafe RPC implementations.
DO NOT use in production environments.
*/

package testimpl

import (
	"context"
	"crypto/ecdsa"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
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
			testAPI := NewTestEthAPI(prodAPI, nil, tt.testAccounts, nil, &txFence{})

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
	testAccountMgr := testSigner(t)
	testAddr := testAccountMgr.Addresses[0]
	unknownAddr := common.HexToAddress("0x0000000000000000000000000000000000000000")
	toAddr := testToAddr

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
			testAPI := NewTestEthAPI(prodAPI, mockBackend, testAccountMgr.Addresses, testAccountMgr.PrivateKeys, &txFence{})

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

// testAccountKey is Hardhat's first well-known dev account.
const testAccountKey = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

// testSigner returns a single hardcoded account, so these tests carry their own
// signing material rather than tracking test_accounts.json. The address is derived
// from the key, so the two cannot drift apart.
func testSigner(t *testing.T) *TestAccountManager {
	t.Helper()
	key, err := crypto.HexToECDSA(testAccountKey)
	if err != nil {
		t.Fatalf("HexToECDSA: %v", err)
	}
	addr := crypto.PubkeyToAddress(key.PublicKey)
	return &TestAccountManager{
		Addresses:   []common.Address{addr},
		PrivateKeys: map[common.Address]*ecdsa.PrivateKey{addr: key},
	}
}

// testToAddr is an arbitrary recipient, for transactions whose destination is
// beside the point.
var testToAddr = common.HexToAddress("0x1234567890123456789012345678901234567890")

// mockBackend is a minimal Backend implementation for testing
type mockBackend struct {
	api.Backend
	sendTxFunc   func(*types.Transaction) error
	txByHashFunc func(common.Hash) (*domain.Transaction, error)
}

func (m *mockBackend) SendTransaction(_ context.Context, tx *types.Transaction) error {
	if m.sendTxFunc != nil {
		return m.sendTxFunc(tx)
	}
	return nil
}

func (m *mockBackend) ChainID(_ context.Context) (*big.Int, error) {
	return big.NewInt(4011), nil
}

func (m *mockBackend) NonceAt(_ context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	return 0, nil // Return nonce 0 for testing
}

func (m *mockBackend) TransactionByHash(_ context.Context, hash common.Hash) (*domain.Transaction, error) {
	if m.txByHashFunc != nil {
		return m.txByHashFunc(hash)
	}
	// Default: return a committed transaction (BlockNumber > 0) to satisfy the polling loop
	return &domain.Transaction{
		BlockNumber: 1,
	}, nil
}

// A transaction the gateway worker fails is deleted from the in-progress map and
// never stored, so TransactionByHash reports it as simply absent. That used to hit
// a "should never happen" panic, which the RPC layer turned into -32603; it must
// surface as an ordinary error rather than stalling until the deadline.
func TestTestEthAPI_DroppedTransactionErrors(t *testing.T) {
	testAccountMgr := testSigner(t)
	from := testAccountMgr.Addresses[0]
	to := testToAddr

	// Neither pending nor stored: the worker failed it and dropped it.
	backend := &mockBackend{
		txByHashFunc: func(common.Hash) (*domain.Transaction, error) { return nil, nil },
	}

	pool := &fakePool{}
	testAPI := NewTestEthAPI(api.NewEthAPI(backend), backend, testAccountMgr.Addresses, testAccountMgr.PrivateKeys, &txFence{pool: pool})

	start := time.Now()
	_, err := testAPI.SendTransaction(context.Background(), TransactionArgs{From: &from, To: &to})
	if err == nil {
		t.Fatal("SendTransaction returned nil for a dropped transaction")
	}
	if !strings.Contains(err.Error(), "dropped") {
		t.Errorf("error = %q, want it to say the transaction was dropped", err)
	}
	if elapsed := time.Since(start); elapsed > commitTimeout/2 {
		t.Errorf("took %s, want a prompt error rather than waiting out commitTimeout", elapsed)
	}
}
