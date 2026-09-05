/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

// stubDirectiveSubmitter records the last directive submitted, standing in for
// the gateway in tests that only exercise RPC registration.
type stubDirectiveSubmitter struct {
	lastAddr   common.Address
	lastAmount *big.Int
	lastCode   []byte
	lastKey    common.Hash
	lastValue  common.Hash
	err        error
}

func (s *stubDirectiveSubmitter) SetBalance(_ context.Context, addr common.Address, amount *big.Int) error {
	s.lastAddr = addr
	s.lastAmount = amount
	return s.err
}

func (s *stubDirectiveSubmitter) SetCode(_ context.Context, addr common.Address, code []byte) error {
	s.lastAddr = addr
	s.lastCode = code
	return s.err
}

func (s *stubDirectiveSubmitter) SetStorageAt(_ context.Context, addr common.Address, key, value common.Hash) error {
	s.lastAddr = addr
	s.lastKey = key
	s.lastValue = value
	return s.err
}

func dialHardhat(t *testing.T) *rpc.Client {
	t.Helper()
	srv := rpc.NewServer()
	if err := srv.RegisterName("hardhat", NewHardhatAPI(&stubDirectiveSubmitter{})); err != nil {
		t.Fatalf("RegisterName hardhat: %v", err)
	}
	client := rpc.DialInProc(srv)
	t.Cleanup(client.Close)
	return client
}

func dialEvm(t *testing.T) *rpc.Client {
	t.Helper()
	srv := rpc.NewServer()
	if err := srv.RegisterName("evm", NewEvmAPI(&mockRevertibleKVS{}, &mockRevertibleStore{}, &txFence{})); err != nil {
		t.Fatalf("RegisterName evm: %v", err)
	}
	client := rpc.DialInProc(srv)
	t.Cleanup(client.Close)
	return client
}

func TestHardhatAPI_Mine_RPCRegistration(t *testing.T) {
	client := dialHardhat(t)

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

func TestEvmAPI_SetAutomine_RPCRegistration(t *testing.T) {
	client := dialEvm(t)
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("%v", enabled), func(t *testing.T) {
			var result any
			if err := client.CallContext(context.Background(), &result, "evm_setAutomine", enabled); err != nil {
				t.Fatalf("evm_setAutomine: %v", err)
			}
			if result != nil {
				t.Fatalf("result = %#v, want nil", result)
			}
		})
	}
}

func TestEvmAPI_SetAutomine_MissingArg(t *testing.T) {
	client := dialEvm(t)
	var result any
	if err := client.CallContext(context.Background(), &result, "evm_setAutomine"); err == nil {
		t.Fatal("expected error for missing argument")
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
	testAccountMgr := testSigner(t)
	from := testAccountMgr.Addresses[0]
	to := testToAddr

	// The transaction sits in the queue until commit is closed, standing in for a
	// tx the gateway has taken but not yet put in a block.
	pool := &fakePool{}
	commit := make(chan struct{})
	polled := make(chan struct{})
	var polledOnce sync.Once
	var committedOnce sync.Once

	backend := &mockBackend{
		sendTxFunc: func(*types.Transaction) error {
			pool.enqueue()
			return nil
		},
		txByHashFunc: func(common.Hash) (*domain.Transaction, error) {
			polledOnce.Do(func() { close(polled) })
			select {
			case <-commit:
				committedOnce.Do(pool.commit)
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

	fence := &txFence{pool: pool}
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

func TestHardhatAPI_ImpersonateAccount_RPCRegistration(t *testing.T) {
	client := dialHardhat(t)
	const addr = "0x364d6D0333432C3Ac016Ca832fb8594A8cE43Ca6"

	var result any
	if err := client.CallContext(context.Background(), &result, "hardhat_impersonateAccount", addr); err != nil {
		t.Fatalf("hardhat_impersonateAccount: %v", err)
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}

	if err := client.CallContext(context.Background(), &result, "hardhat_stopImpersonatingAccount", addr); err != nil {
		t.Fatalf("hardhat_stopImpersonatingAccount: %v", err)
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
}

func TestHardhatAPI_ImpersonateAccount_MissingAddress(t *testing.T) {
	client := dialHardhat(t)

	var result any
	if err := client.CallContext(context.Background(), &result, "hardhat_impersonateAccount"); err == nil {
		t.Fatal("expected error for missing address")
	}
	if err := client.CallContext(context.Background(), &result, "hardhat_stopImpersonatingAccount"); err == nil {
		t.Fatal("expected error for missing address")
	}
}
