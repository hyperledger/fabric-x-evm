/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"bytes"
	"context"
	"math/big"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	estorage "github.com/hyperledger/fabric-x-evm/endorser/storage"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/endorsement"
)

const testNS = "basic"

var directiveTestAddr = ethcommon.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")

// noopBuilder is a fake endorsement.Builder that records the read-write set it
// was asked to endorse instead of signing anything.
type noopBuilder struct {
	lastRWS blocks.ReadWriteSet
}

func (b *noopBuilder) Endorse(_ endorsement.Invocation, res endorsement.ExecutionResult) (*peer.ProposalResponse, error) {
	b.lastRWS = res.RWS
	return &peer.ProposalResponse{}, nil
}

// balFromRWS returns the balance a read-write set writes for addr, or nil when
// the set contains no balance write.
func balFromRWS(rws blocks.ReadWriteSet, addr ethcommon.Address) *big.Int {
	key := "acc:" + addr.Hex() + ":bal"
	for _, w := range rws.Writes {
		if w.Key == key {
			if w.IsDelete {
				return big.NewInt(0)
			}
			return new(big.Int).SetBytes(w.Value)
		}
	}
	return nil
}

// commitRWS seeds kvs with rws through the same Handle path real commits use.
func commitRWS(t *testing.T, kvs *estorage.LightKVS, ns string, rws blocks.ReadWriteSet) {
	t.Helper()
	blockNum, err := kvs.BlockNumber(context.Background())
	if err != nil {
		t.Fatalf("BlockNumber: %v", err)
	}
	block := blocks.Block{
		Number: blockNum,
		Transactions: []blocks.Transaction{{
			ID:     "seed",
			Number: 0,
			Valid:  true,
			NsRWS:  []blocks.NsReadWriteSet{{Namespace: ns, RWS: rws}},
		}},
	}
	if err := kvs.Handle(context.Background(), block); err != nil {
		t.Fatalf("Handle: %v", err)
	}
}

// testInvocation builds a minimal invocation good enough for a nil builder's
// Endorse to sign; the endorser under test doesn't inspect its contents.
func testInvocation() endorsement.Invocation {
	return endorsement.Invocation{}
}

func TestSetBalance_RaiseFromZero(t *testing.T) {
	kvs := estorage.NewLightKVS(8)
	b := &noopBuilder{}
	d := NewDirectiveEndorser(nil, kvs, testNS, b, true)

	target := new(big.Int).Mul(big.NewInt(1234), big.NewInt(1e9))
	if _, err := d.SetBalance(context.Background(), testInvocation(), directiveTestAddr, target); err != nil {
		t.Fatalf("SetBalance: %v", err)
	}
	got := balFromRWS(b.lastRWS, directiveTestAddr)
	if got == nil || got.Cmp(target) != 0 {
		t.Fatalf("balance write = %v, want %s", got, target)
	}
}

func TestSetBalance_LowerExisting(t *testing.T) {
	kvs := estorage.NewLightKVS(8)
	b := &noopBuilder{}
	d := NewDirectiveEndorser(nil, kvs, testNS, b, true)

	// Seed the account at a high balance, then lower it.
	high := big.NewInt(1_000_000)
	if _, err := d.SetBalance(context.Background(), testInvocation(), directiveTestAddr, high); err != nil {
		t.Fatalf("seed SetBalance: %v", err)
	}
	commitRWS(t, kvs, testNS, b.lastRWS)

	low := big.NewInt(400)
	if _, err := d.SetBalance(context.Background(), testInvocation(), directiveTestAddr, low); err != nil {
		t.Fatalf("SetBalance: %v", err)
	}
	got := balFromRWS(b.lastRWS, directiveTestAddr)
	if got == nil || got.Cmp(low) != 0 {
		t.Fatalf("balance write = %v, want %s (delta-down)", got, low)
	}
}

// codeFromRWS returns the code a read-write set writes for addr, or nil when
// the set contains no code write.
func codeFromRWS(rws blocks.ReadWriteSet, addr ethcommon.Address) []byte {
	key := "acc:" + addr.Hex() + ":code"
	for _, w := range rws.Writes {
		if w.Key == key {
			if w.IsDelete {
				return []byte{}
			}
			return w.Value
		}
	}
	return nil
}

func TestSetCode_Set(t *testing.T) {
	kvs := estorage.NewLightKVS(8)
	b := &noopBuilder{}
	d := NewDirectiveEndorser(nil, kvs, testNS, b, true)

	code := []byte{0x60, 0x2a, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}
	if _, err := d.SetCode(context.Background(), testInvocation(), directiveTestAddr, code); err != nil {
		t.Fatalf("SetCode: %v", err)
	}
	got := codeFromRWS(b.lastRWS, directiveTestAddr)
	if !bytes.Equal(got, code) {
		t.Fatalf("code write = %x, want %x", got, code)
	}
}

func TestSetCode_ClearWithEmpty(t *testing.T) {
	kvs := estorage.NewLightKVS(8)
	b := &noopBuilder{}
	d := NewDirectiveEndorser(nil, kvs, testNS, b, true)

	code := []byte{0x60, 0x2a}
	if _, err := d.SetCode(context.Background(), testInvocation(), directiveTestAddr, code); err != nil {
		t.Fatalf("seed SetCode: %v", err)
	}
	commitRWS(t, kvs, testNS, b.lastRWS)

	if _, err := d.SetCode(context.Background(), testInvocation(), directiveTestAddr, nil); err != nil {
		t.Fatalf("SetCode: %v", err)
	}
	got := codeFromRWS(b.lastRWS, directiveTestAddr)
	if len(got) != 0 {
		t.Fatalf("code write after clear = %x, want empty", got)
	}
}

// storageFromRWS returns the storage word a read-write set writes for addr/key.
// A cleared slot and an absent write both yield an empty word, so the second
// result reports whether the write was there at all.
func storageFromRWS(rws blocks.ReadWriteSet, addr ethcommon.Address, key ethcommon.Hash) ([]byte, bool) {
	k := "str:" + addr.Hex() + ":" + key.Hex()
	for _, w := range rws.Writes {
		if w.Key == k {
			if w.IsDelete {
				return []byte{}, true
			}
			return w.Value, true
		}
	}
	return nil, false
}

func TestSetStorageAt_Set(t *testing.T) {
	kvs := estorage.NewLightKVS(8)
	b := &noopBuilder{}
	d := NewDirectiveEndorser(nil, kvs, testNS, b, true)

	key := ethcommon.HexToHash("0x1")
	value := ethcommon.HexToHash("0x2a")
	if _, err := d.SetStorageAt(context.Background(), testInvocation(), directiveTestAddr, key, value); err != nil {
		t.Fatalf("SetStorageAt: %v", err)
	}
	got, ok := storageFromRWS(b.lastRWS, directiveTestAddr, key)
	if !ok {
		t.Fatal("no storage write in the read-write set")
	}
	if !bytes.Equal(got, value.Bytes()) {
		t.Fatalf("storage write = %x, want %x", got, value.Bytes())
	}
}

func TestSetStorageAt_Overwrite(t *testing.T) {
	kvs := estorage.NewLightKVS(8)
	b := &noopBuilder{}
	d := NewDirectiveEndorser(nil, kvs, testNS, b, true)

	key := ethcommon.HexToHash("0x1")
	first := ethcommon.HexToHash("0x2a")
	if _, err := d.SetStorageAt(context.Background(), testInvocation(), directiveTestAddr, key, first); err != nil {
		t.Fatalf("seed SetStorageAt: %v", err)
	}
	commitRWS(t, kvs, testNS, b.lastRWS)

	second := ethcommon.HexToHash("0x2b")
	if _, err := d.SetStorageAt(context.Background(), testInvocation(), directiveTestAddr, key, second); err != nil {
		t.Fatalf("SetStorageAt: %v", err)
	}
	got, ok := storageFromRWS(b.lastRWS, directiveTestAddr, key)
	if !ok {
		t.Fatal("no storage write in the read-write set")
	}
	if !bytes.Equal(got, second.Bytes()) {
		t.Fatalf("storage write = %x, want %x", got, second.Bytes())
	}
}

func TestSetStorageAt_ClearWithZero(t *testing.T) {
	kvs := estorage.NewLightKVS(8)
	b := &noopBuilder{}
	d := NewDirectiveEndorser(nil, kvs, testNS, b, true)

	key := ethcommon.HexToHash("0x1")
	value := ethcommon.HexToHash("0x2a")
	if _, err := d.SetStorageAt(context.Background(), testInvocation(), directiveTestAddr, key, value); err != nil {
		t.Fatalf("seed SetStorageAt: %v", err)
	}
	commitRWS(t, kvs, testNS, b.lastRWS)

	if _, err := d.SetStorageAt(context.Background(), testInvocation(), directiveTestAddr, key, ethcommon.Hash{}); err != nil {
		t.Fatalf("SetStorageAt: %v", err)
	}
	got, ok := storageFromRWS(b.lastRWS, directiveTestAddr, key)
	if !ok {
		t.Fatal("clear produced no storage write in the read-write set")
	}
	if len(got) != 0 {
		t.Fatalf("storage write after clear = %x, want empty", got)
	}
}

func TestSetBalance_AmountOverflowsUint256(t *testing.T) {
	kvs := estorage.NewLightKVS(2)
	d := NewDirectiveEndorser(nil, kvs, testNS, &noopBuilder{}, true)

	tooBig := new(big.Int).Lsh(big.NewInt(1), 256) // 2^256, one past uint256 max
	if _, err := d.SetBalance(context.Background(), testInvocation(), directiveTestAddr, tooBig); err == nil {
		t.Fatal("expected error for amount overflowing uint256, got nil")
	}
}
