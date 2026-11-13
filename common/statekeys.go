/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"encoding/binary"
	"math/big"
	"strings"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// The EVM world state is stored as Fabric keys and values. The endorser writes
// them and anything replaying a write set reads them back.

// Keys:
//   - account fields: "acc:<address>:<field>", field one of the Field constants
//   - storage slots:  "str:<address>:<slot>"
//
// with address and slot in their 0x-prefixed hex form.
const (
	FieldBalance = "bal"
	FieldNonce   = "nonce"
	FieldCode    = "code"

	accountPrefix = "acc"
	storagePrefix = "str"
)

// AccountKey is the key of one field of an account.
func AccountKey(addr ethcommon.Address, field string) string {
	return accountPrefix + ":" + addr.Hex() + ":" + field
}

// StorageKey is the key of one storage slot.
func StorageKey(addr ethcommon.Address, slot ethcommon.Hash) string {
	return storagePrefix + ":" + addr.Hex() + ":" + slot.Hex()
}

// StateKey is a parsed AccountKey or StorageKey. Field is set for an account
// key, Slot for a storage key.
type StateKey struct {
	Address ethcommon.Address
	Field   string
	Slot    ethcommon.Hash
	Storage bool
}

// ParseStateKey parses an AccountKey or StorageKey, and reports false for any
// other key.
func ParseStateKey(key string) (StateKey, bool) {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 {
		return StateKey{}, false
	}
	addr := ethcommon.HexToAddress(parts[1])
	switch parts[0] {
	case accountPrefix:
		return StateKey{Address: addr, Field: parts[2]}, true
	case storagePrefix:
		return StateKey{Address: addr, Slot: ethcommon.HexToHash(parts[2]), Storage: true}, true
	}
	return StateKey{}, false
}

// EncodeBalance is the minimal big-endian form; zero encodes as empty.
func EncodeBalance(u *uint256.Int) []byte {
	if u == nil {
		return nil
	}
	return u.ToBig().Bytes()
}

// DecodeBalance reverses EncodeBalance; empty decodes as zero.
func DecodeBalance(b []byte) *uint256.Int {
	if len(b) == 0 {
		return new(uint256.Int)
	}
	u, _ := uint256.FromBig(new(big.Int).SetBytes(b))
	return u
}

// EncodeNonce is the 8-byte big-endian form.
func EncodeNonce(nonce uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, nonce)
	return b
}

// DecodeNonce reverses EncodeNonce; empty decodes as zero.
func DecodeNonce(b []byte) uint64 {
	if len(b) == 0 {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

// EncodeStorage is the raw 32 bytes of the value, or nil for zero, which the
// write set records as a delete.
func EncodeStorage(value ethcommon.Hash) []byte {
	if value == (ethcommon.Hash{}) {
		return nil
	}
	return value.Bytes()
}

// DecodeStorage reverses EncodeStorage; empty decodes as zero.
func DecodeStorage(b []byte) ethcommon.Hash {
	if len(b) == 0 {
		return ethcommon.Hash{}
	}
	return ethcommon.BytesToHash(b)
}
