/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
)

// testPrecompileSupport verifies that the gateway forwards eth_call requests
// to the standard Ethereum precompiled contracts (0x01-0x09) and returns the
// same output go-ethereum's reference implementation produces locally.
//
// Using geth's PrecompiledContracts registry as the oracle means the test
// captures any wiring drift (wrong address routing, output truncation,
// missing precompile, etc.) without baking magic constants into the test
// that would silently rot when go-ethereum updates its reference vectors.
func testPrecompileSupport(t *testing.T, th *TestHarness) {
	ec, err := NewNativeEthClient(th.Gateways[0])
	if err != nil {
		t.Fatal(err)
	}

	// 0x01 ecrecover with a freshly-signed message so the test is
	// deterministic across builds without bundling magic constants.
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	msgHash := crypto.Keccak256Hash([]byte("precompile-smoke-test"))
	sig, err := crypto.Sign(msgHash.Bytes(), key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	// ecrecover input layout: hash (32) | v (32) | r (32) | s (32). v is
	// stored at byte 63 (right-aligned in its 32-byte word) and must be 27/28.
	ecrecoverInput := make([]byte, 128)
	copy(ecrecoverInput[0:32], msgHash.Bytes())
	ecrecoverInput[63] = sig[64] + 27
	copy(ecrecoverInput[64:96], sig[0:32])
	copy(ecrecoverInput[96:128], sig[32:64])

	// 0x05 modexp: 3^2 mod 5 = 4. Layout: lenB | lenE | lenM (each 32 bytes)
	// followed by base || exp || mod.
	modexpInput := append(
		append(append(word(1), word(1)...), word(1)...),
		0x03, 0x02, 0x05,
	)

	// 0x06 bn256Add: P + 0 = P, where P is the curve generator (1, 2).
	bn256AddInput := concat(
		word(1), word(2), // P = G1
		word(0), word(0), // 0
	)

	// 0x07 bn256ScalarMul: 1 * G = G.
	bn256MulInput := concat(word(1), word(2), word(1))

	// 0x09 blake2f: EIP-152 test vector 4 (RFC 7693 "abc").
	// https://eips.ethereum.org/EIPS/eip-152#test-vector-4
	blake2fInput, err := hex.DecodeString(
		"0000000c" +
			"48c9bdf267e6096a3ba7ca8485ae67bb2bf894fe72f36e3cf1361d5f3af54fa5" +
			"d182e6ad7f520e511f6c3e2b8c68059b6bbd41fbabd9831f79217e1319cde05b" +
			"6162638000000000000000000000000000000000000000000000000000000000" +
			"0000000000000000000000000000000000000000000000000000000000000000" +
			"0000000000000000000000000000000000000000000000000000000000000000" +
			"0000000000000000000000000000000000000000000000000000000000000000" +
			"03000000000000000000000000000000" +
			"01")
	if err != nil {
		t.Fatalf("decode blake2f input: %v", err)
	}

	type precompile struct {
		name string
		addr byte
		in   []byte
	}
	precompiles := []precompile{
		{"ecrecover", 0x01, ecrecoverInput},
		{"sha256", 0x02, []byte("abc")},
		{"ripemd160", 0x03, []byte("abc")},
		{"identity", 0x04, []byte("the quick brown fox")},
		{"modexp", 0x05, modexpInput},
		{"bn256Add", 0x06, bn256AddInput},
		{"bn256ScalarMul", 0x07, bn256MulInput},
		{"bn256Pairing", 0x08, []byte{}},
		{"blake2f", 0x09, blake2fInput},
	}

	for _, p := range precompiles {
		t.Run(p.name, func(t *testing.T) {
			to := common.BytesToAddress([]byte{p.addr})

			ref, ok := vm.PrecompiledContractsCancun[to]
			if !ok {
				t.Fatalf("no reference precompile registered at 0x%02x", p.addr)
			}
			want, err := ref.Run(p.in)
			if err != nil {
				t.Fatalf("reference precompile error: %v", err)
			}

			got, err := ec.CallContract(t.Context(), ethereum.CallMsg{
				To:   &to,
				Data: p.in,
			}, nil)
			if err != nil {
				t.Fatalf("eth_call to 0x%02x: %v", p.addr, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("output mismatch:\n got:  %s\n want: %s",
					hex.EncodeToString(got), hex.EncodeToString(want))
			}
		})
	}
}

// word returns the 32-byte big-endian encoding of an unsigned-byte integer.
func word(v byte) []byte {
	b := make([]byte, 32)
	b[31] = v
	return b
}

func concat(parts ...[]byte) []byte {
	var n int
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
