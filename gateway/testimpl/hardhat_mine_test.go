/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/hyperledger/fabric-x-evm/gateway/api"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

// fakeCutter advances height on each CutBlock so a stub backend can observe it.
type fakeCutter struct {
	mu     sync.Mutex
	cuts   int
	height *uint64
	err    error
}

func (f *fakeCutter) CutBlock(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.cuts++
	if f.height != nil {
		*f.height++
	}
	return nil
}

// mineBackend is a Backend that only implements BlockNumber for mine wait tests.
type mineBackend struct {
	api.Backend
	mu     sync.Mutex
	height *uint64
}

func (m *mineBackend) BlockNumber(context.Context) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *m.height, nil
}

func (m *mineBackend) ChainID(context.Context) (*big.Int, error) { return big.NewInt(4011), nil }

func (m *mineBackend) GetBlockByNumber(_ context.Context, num uint64, _ bool) (*domain.Block, error) {
	return &domain.Block{BlockNumber: num, BlockHash: common.BytesToHash([]byte{byte(num)}).Bytes()}, nil
}

func TestHardhatAPI_Mine_NoCutter(t *testing.T) {
	api := NewHardhatAPI(nil, &mineBackend{height: new(uint64)}, nil)
	err := api.Mine(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error when cutter is nil")
	}
}

func TestHardhatAPI_Mine_DefaultOneBlock(t *testing.T) {
	height := uint64(3)
	cutter := &fakeCutter{height: &height}
	backend := &mineBackend{height: &height}
	api := NewHardhatAPI(nil, backend, cutter)

	if err := api.Mine(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if cutter.cuts != 1 {
		t.Fatalf("cuts=%d, want 1", cutter.cuts)
	}
	if height != 4 {
		t.Fatalf("height=%d, want 4", height)
	}
}

func TestHardhatAPI_Mine_ZeroBlocksNoOp(t *testing.T) {
	height := uint64(5)
	cutter := &fakeCutter{height: &height}
	api := NewHardhatAPI(nil, &mineBackend{height: &height}, cutter)

	zero := hexutil.Uint64(0)
	if err := api.Mine(context.Background(), &zero, nil); err != nil {
		t.Fatal(err)
	}
	if cutter.cuts != 0 {
		t.Fatalf("cuts=%d, want 0", cutter.cuts)
	}
}

func TestHardhatAPI_Mine_MultipleBlocks(t *testing.T) {
	height := uint64(10)
	cutter := &fakeCutter{height: &height}
	api := NewHardhatAPI(nil, &mineBackend{height: &height}, cutter)

	n := hexutil.Uint64(5)
	if err := api.Mine(context.Background(), &n, nil); err != nil {
		t.Fatal(err)
	}
	if cutter.cuts != 5 {
		t.Fatalf("cuts=%d, want 5", cutter.cuts)
	}
	if height != 15 {
		t.Fatalf("height=%d, want 15", height)
	}
}

func TestHardhatAPI_Mine_WaitsForSync(t *testing.T) {
	// Cutter advances a private counter; gateway height catches up asynchronously.
	var gatewayHeight uint64 = 1
	var cutCount int
	cutter := &delayedCutter{
		onCut: func() {
			cutCount++
			// Simulate synchronizer lag: bump gateway height shortly after cut.
			go func(target uint64) {
				time.Sleep(30 * time.Millisecond)
				gatewayHeight = target
			}(gatewayHeight + 1)
		},
	}
	backend := &funcBackend{blockNumber: func(context.Context) (uint64, error) { return gatewayHeight, nil }}
	api := NewHardhatAPI(nil, backend, cutter)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := api.Mine(ctx, nil, nil); err != nil {
		t.Fatal(err)
	}
	if cutCount != 1 {
		t.Fatalf("cuts=%d", cutCount)
	}
	if gatewayHeight != 2 {
		t.Fatalf("height=%d, want 2", gatewayHeight)
	}
}

type delayedCutter struct{ onCut func() }

func (d *delayedCutter) CutBlock(context.Context) error {
	d.onCut()
	return nil
}

type funcBackend struct {
	api.Backend
	blockNumber func(context.Context) (uint64, error)
}

func (f *funcBackend) BlockNumber(ctx context.Context) (uint64, error) { return f.blockNumber(ctx) }
func (f *funcBackend) ChainID(context.Context) (*big.Int, error)       { return big.NewInt(1), nil }

func TestEvmAPI_Mine_UsesCutter(t *testing.T) {
	height := uint64(7)
	cutter := &fakeCutter{height: &height}
	backend := &mineBackend{height: &height}
	api := NewEvmAPI(&mockRevertibleKVS{}, &mockRevertibleStore{}, &txFence{}, &recordingNonces{}, cutter, backend)

	got, err := api.Mine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cutter.cuts != 1 {
		t.Fatalf("cuts=%d, want 1", cutter.cuts)
	}
	if got == "" || got == "0x0" {
		t.Fatalf("expected non-stub return, got %q", got)
	}
}

func TestHardhatAPI_Mine_RPCWithCutter(t *testing.T) {
	height := uint64(0)
	cutter := &fakeCutter{height: &height}
	backend := &mineBackend{height: &height}
	srv := rpc.NewServer()
	if err := srv.RegisterName("hardhat", NewHardhatAPI(nil, backend, cutter)); err != nil {
		t.Fatal(err)
	}
	client := rpc.DialInProc(srv)
	t.Cleanup(client.Close)

	var result any
	if err := client.CallContext(context.Background(), &result, "hardhat_mine", "0x2"); err != nil {
		t.Fatalf("hardhat_mine: %v", err)
	}
	if cutter.cuts != 2 {
		t.Fatalf("cuts=%d, want 2", cutter.cuts)
	}
}
