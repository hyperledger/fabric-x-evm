/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later

WARNING: This package contains test-only/unsafe RPC implementations.
DO NOT use in production environments. These methods perform server-side
message signing which is inherently insecure and should only be used
for development and testing purposes.
*/

package testimpl

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// typedDataArg accepts EIP-712 payloads either as a JSON object or as a
// JSON-encoded string. ethers.js sends the latter via eth_signTypedData_v4.
type typedDataArg apitypes.TypedData

func (t *typedDataArg) UnmarshalJSON(data []byte) error {
	// ethers.js sends a JSON-encoded string; Hardhat may send an object.
	// Prefer string decode first so leading whitespace still works (encoding/json
	// allows it; a data[0]=='"' check would miss that).
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		return json.Unmarshal([]byte(s), (*apitypes.TypedData)(t))
	}
	return json.Unmarshal(data, (*apitypes.TypedData)(t))
}

// SignTypedData_v4 implements eth_signTypedData_v4 (EIP-712).
// Registers as eth_signTypedData_v4 via geth's formatName (first letter lowercased).
//
// SECURITY WARNING: Server-side signing with held private keys is for tests only.
func (api *TestEthAPI) SignTypedData_v4(ctx context.Context, addr common.Address, data typedDataArg) (hexutil.Bytes, error) {
	privateKey, ok := api.testAccountKeys[addr]
	if !ok {
		return nil, fmt.Errorf("no private key available for address %s", addr.Hex())
	}

	sighash, _, err := apitypes.TypedDataAndHash(apitypes.TypedData(data))
	if err != nil {
		return nil, fmt.Errorf("failed to hash typed data: %w", err)
	}
	return signWithLegacyV(sighash, privateKey)
}

// PersonalAPI provides the personal_* RPC namespace for Hardhat/ethers tests.
type PersonalAPI struct {
	testAccountKeys map[common.Address]*ecdsa.PrivateKey
}

// NewPersonalAPI creates a personal API bound to the given test account keys.
func NewPersonalAPI(keys map[common.Address]*ecdsa.PrivateKey) *PersonalAPI {
	return &PersonalAPI{testAccountKeys: keys}
}

// Sign implements personal_sign (EIP-191 personal message).
// Parameter order matches the personal_sign convention: data, address, optional password.
// The password is accepted for RPC compatibility and ignored in the test backend.
//
// SECURITY WARNING: Server-side signing with held private keys is for tests only.
func (api *PersonalAPI) Sign(ctx context.Context, data hexutil.Bytes, addr common.Address, passwd *string) (hexutil.Bytes, error) {
	privateKey, ok := api.testAccountKeys[addr]
	if !ok {
		return nil, fmt.Errorf("no private key available for address %s", addr.Hex())
	}
	return signWithLegacyV(accounts.TextHash(data), privateKey)
}

// signWithLegacyV signs hash and rewrites V from 0/1 to 27/28 (yellow paper).
func signWithLegacyV(hash []byte, key *ecdsa.PrivateKey) (hexutil.Bytes, error) {
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		return nil, fmt.Errorf("failed to sign: %w", err)
	}
	sig[64] += 27
	return sig, nil
}
