/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

func TestStateKeysRoundTrip(t *testing.T) {
	addr := ethcommon.HexToAddress("0x5CaCB0cD1BEa69d7453CaB8dfe870a90f5FfF30B")
	slot := ethcommon.HexToHash("0x01")

	for _, field := range []string{FieldBalance, FieldNonce, FieldCode} {
		got, ok := ParseStateKey(AccountKey(addr, field))
		if !ok || got != (StateKey{Address: addr, Field: field}) {
			t.Errorf("account key %q: got %+v, %v", field, got, ok)
		}
	}
	got, ok := ParseStateKey(StorageKey(addr, slot))
	if !ok || got != (StateKey{Address: addr, Slot: slot, Storage: true}) {
		t.Errorf("storage key: got %+v, %v", got, ok)
	}
	for _, key := range []string{"", "acc:" + addr.Hex(), "other:" + addr.Hex() + ":bal"} {
		if _, ok := ParseStateKey(key); ok {
			t.Errorf("ParseStateKey(%q) accepted a key that is not a state key", key)
		}
	}
}

func TestStateValuesRoundTrip(t *testing.T) {
	for _, bal := range []*uint256.Int{uint256.NewInt(0), uint256.NewInt(1), uint256.MustFromHex("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")} {
		if got := DecodeBalance(EncodeBalance(bal)); !got.Eq(bal) {
			t.Errorf("balance %s: got %s", bal, got)
		}
	}
	for _, nonce := range []uint64{0, 1, 1<<64 - 1} {
		if got := DecodeNonce(EncodeNonce(nonce)); got != nonce {
			t.Errorf("nonce %d: got %d", nonce, got)
		}
	}
	// A value below 0x10 is where an ASCII-hex decoder reads a raw byte as zero.
	for _, value := range []ethcommon.Hash{{}, ethcommon.HexToHash("0x67"), ethcommon.HexToHash("0x01"), ethcommon.MaxHash} {
		if got := DecodeStorage(EncodeStorage(value)); got != value {
			t.Errorf("storage %s: got %s", value, got)
		}
	}
	if EncodeStorage(ethcommon.Hash{}) != nil {
		t.Error("zero storage must encode as nil, which the write set records as a delete")
	}
}
