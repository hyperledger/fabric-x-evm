/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

func TestHardhatAPI_Mine_RPCRegistration(t *testing.T) {
	srv := rpc.NewServer()
	if err := srv.RegisterName("hardhat", NewHardhatAPI()); err != nil {
		t.Fatalf("RegisterName hardhat: %v", err)
	}

	client := rpc.DialInProc(srv)
	defer client.Close()

	// Hardhat accepts zero, one, or two optional hex quantities.
	cases := []struct {
		name   string
		params []any
	}{
		{"no params", nil},
		{"blocks only", []any{"0x100"}},
		{"blocks and interval", []any{"0x3e8", "0x3c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var result any
			var err error
			if len(tc.params) == 0 {
				err = client.CallContext(context.Background(), &result, "hardhat_mine")
			} else {
				err = client.CallContext(context.Background(), &result, "hardhat_mine", tc.params...)
			}
			if err != nil {
				t.Fatalf("hardhat_mine: %v", err)
			}
			if result != nil {
				t.Fatalf("result = %#v, want nil", result)
			}
		})
	}
}

// mockRevertibleKVS / mockRevertibleStore are the two halves of the ledger that
// evm_revert rewinds, stubbed down to what EvmAPI actually calls.
type mockRevertibleKVS struct {
	onRevert func(blockNumber uint64)
}

func (m *mockRevertibleKVS) BlockNumber(context.Context) (uint64, error) { return 1, nil }

func (m *mockRevertibleKVS) RevertToBlock(blockNumber uint64) error {
	if m.onRevert != nil {
		m.onRevert(blockNumber)
	}
	return nil
}

type mockRevertibleStore struct{}

func (m *mockRevertibleStore) Snapshot(context.Context) (uint64, error)    { return 1, nil }
func (m *mockRevertibleStore) RevertToBlock(context.Context, uint64) error { return nil }

// TestEvmAPI_RevertWaitsForInFlightTransaction pins the invariant the txFence
// exists for: a transaction the test RPC has accepted must finish committing
// before evm_revert rewinds the ledger. Without the fence the revert lands
// first and the transaction commits into the state the next test believes it
// just reset.
func TestEvmAPI_RevertWaitsForInFlightTransaction(t *testing.T) {
	testAccountMgr, err := LoadTestAccounts("../../testdata/test_accounts.json")
	if err != nil {
		t.Fatalf("LoadTestAccounts: %v", err)
	}
	from := testAccountMgr.Addresses[0]
	to := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// The transaction stays pending until commit is closed, standing in for a
	// tx sitting in the gateway's queue.
	commit := make(chan struct{})
	polled := make(chan struct{})
	var polledOnce sync.Once

	backend := &mockBackend{
		txByHashFunc: func(common.Hash) (*domain.Transaction, error) {
			polledOnce.Do(func() { close(polled) })
			select {
			case <-commit:
				return &domain.Transaction{BlockNumber: 1}, nil
			default:
				time.Sleep(time.Millisecond) // keep the poll loop off a hot spin
				return &domain.Transaction{BlockNumber: 0}, nil
			}
		},
	}

	var mu sync.Mutex
	var events []string
	record := func(what string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, what)
	}

	fence := &txFence{}
	kvs := &mockRevertibleKVS{onRevert: func(uint64) { record("revert") }}
	testAPI := NewTestEthAPI(api.NewEthAPI(backend), backend, testAccountMgr.Addresses, testAccountMgr.PrivateKeys, fence)
	evmAPI := NewEvmAPI(kvs, &mockRevertibleStore{}, fence)

	snapshotID, err := evmAPI.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	sent := make(chan error, 1)
	go func() {
		_, err := testAPI.SendTransaction(context.Background(), TransactionArgs{From: &from, To: &to})
		record("committed")
		sent <- err
	}()

	<-polled // the send is in flight and holding the fence

	reverted := make(chan error, 1)
	go func() {
		_, err := evmAPI.Revert(context.Background(), snapshotID)
		reverted <- err
	}()

	// The revert must not get through while the transaction is still in flight.
	select {
	case err := <-reverted:
		t.Fatalf("Revert completed with a transaction still in flight (err=%v)", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(commit)

	if err := <-sent; err != nil {
		t.Fatalf("SendTransaction: %v", err)
	}
	select {
	case err := <-reverted:
		if err != nil {
			t.Fatalf("Revert: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Revert did not complete after the transaction committed")
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"committed", "revert"}
	if len(events) != len(want) || events[0] != want[0] || events[1] != want[1] {
		t.Fatalf("event order = %v, want %v", events, want)
	}
}
