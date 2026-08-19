/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/hyperledger/fabric-x-evm/gateway/api"
)

func loadSigningFixture(t *testing.T) (*TestAccountManager, common.Address) {
	t.Helper()
	mgr, err := LoadTestAccounts("../../testdata/test_accounts.json")
	if err != nil {
		t.Fatalf("LoadTestAccounts: %v", err)
	}
	return mgr, mgr.Addresses[0]
}

func dialSigningServer(t *testing.T, mgr *TestAccountManager) *rpc.Client {
	t.Helper()
	srv := rpc.NewServer()
	prodAPI := api.NewEthAPI(nil)
	testAPI := NewTestEthAPI(prodAPI, nil, mgr.Addresses, mgr.PrivateKeys, &txFence{})
	if err := srv.RegisterName("eth", testAPI); err != nil {
		t.Fatalf("RegisterName eth: %v", err)
	}
	if err := srv.RegisterName("personal", NewPersonalAPI(mgr.PrivateKeys)); err != nil {
		t.Fatalf("RegisterName personal: %v", err)
	}
	client := rpc.DialInProc(srv)
	t.Cleanup(client.Close)
	return client
}

func recoverSigner(t *testing.T, hash, sig []byte) common.Address {
	t.Helper()
	if len(sig) != 65 {
		t.Fatalf("signature length = %d, want 65", len(sig))
	}
	// Clone so we do not mutate the caller's slice.
	s := make([]byte, 65)
	copy(s, sig)
	if s[64] != 27 && s[64] != 28 {
		t.Fatalf("V = %d, want 27 or 28", s[64])
	}
	s[64] -= 27
	pub, err := crypto.SigToPub(hash, s)
	if err != nil {
		t.Fatalf("SigToPub: %v", err)
	}
	return crypto.PubkeyToAddress(*pub)
}

func TestPersonalAPI_Sign_RecoversSigner(t *testing.T) {
	mgr, addr := loadSigningFixture(t)
	api := NewPersonalAPI(mgr.PrivateKeys)

	msg := []byte("hello fabric-evm")
	sig, err := api.Sign(context.Background(), hexutil.Bytes(msg), addr, nil)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	got := recoverSigner(t, accounts.TextHash(msg), sig)
	if got != addr {
		t.Fatalf("recovered %s, want %s", got.Hex(), addr.Hex())
	}
}

func TestPersonalAPI_Sign_UnknownAddress(t *testing.T) {
	mgr, _ := loadSigningFixture(t)
	api := NewPersonalAPI(mgr.PrivateKeys)
	unknown := common.HexToAddress("0x0000000000000000000000000000000000000001")
	_, err := api.Sign(context.Background(), hexutil.Bytes("x"), unknown, nil)
	if err == nil || !strings.Contains(err.Error(), "no private key") {
		t.Fatalf("error = %v, want no private key", err)
	}
}

func TestPersonalAPI_Sign_RPCRegistration(t *testing.T) {
	mgr, addr := loadSigningFixture(t)
	client := dialSigningServer(t, mgr)

	msg := []byte("rpc personal_sign")
	var result hexutil.Bytes
	if err := client.CallContext(context.Background(), &result, "personal_sign", hexutil.Bytes(msg), addr); err != nil {
		t.Fatalf("personal_sign: %v", err)
	}
	got := recoverSigner(t, accounts.TextHash(msg), result)
	if got != addr {
		t.Fatalf("recovered %s, want %s", got.Hex(), addr.Hex())
	}
}

func TestPersonalAPI_Sign_RPCWithPassword(t *testing.T) {
	mgr, addr := loadSigningFixture(t)
	client := dialSigningServer(t, mgr)

	msg := []byte("with password")
	var result hexutil.Bytes
	// Optional third arg accepted and ignored.
	if err := client.CallContext(context.Background(), &result, "personal_sign", hexutil.Bytes(msg), addr, ""); err != nil {
		t.Fatalf("personal_sign with password: %v", err)
	}
	if len(result) != 65 {
		t.Fatalf("sig len = %d, want 65", len(result))
	}
}

func TestSignTypedData_v4_RecoversSigner(t *testing.T) {
	mgr, addr := loadSigningFixture(t)
	testAPI := NewTestEthAPI(api.NewEthAPI(nil), nil, mgr.Addresses, mgr.PrivateKeys, &txFence{})

	td := permitTypedData(addr.Hex(), "1")
	sig, err := testAPI.SignTypedData_v4(context.Background(), addr, typedDataArg(td))
	if err != nil {
		t.Fatalf("SignTypedData_v4: %v", err)
	}
	hash, _, err := apitypes.TypedDataAndHash(td)
	if err != nil {
		t.Fatalf("TypedDataAndHash: %v", err)
	}
	got := recoverSigner(t, hash, sig)
	if got != addr {
		t.Fatalf("recovered %s, want %s", got.Hex(), addr.Hex())
	}
}

func TestSignTypedData_v4_UnknownAddress(t *testing.T) {
	mgr, addr := loadSigningFixture(t)
	testAPI := NewTestEthAPI(api.NewEthAPI(nil), nil, mgr.Addresses, mgr.PrivateKeys, &txFence{})
	unknown := common.HexToAddress("0x0000000000000000000000000000000000000001")
	_, err := testAPI.SignTypedData_v4(context.Background(), unknown, typedDataArg(permitTypedData(addr.Hex(), "1")))
	if err == nil || !strings.Contains(err.Error(), "no private key") {
		t.Fatalf("error = %v, want no private key", err)
	}
}

func TestSignTypedData_v4_RPCObjectAndString(t *testing.T) {
	mgr, addr := loadSigningFixture(t)
	client := dialSigningServer(t, mgr)
	td := permitTypedData(addr.Hex(), "1")

	// Object form (Hardhat-style).
	var sigObj hexutil.Bytes
	if err := client.CallContext(context.Background(), &sigObj, "eth_signTypedData_v4", addr, td); err != nil {
		t.Fatalf("eth_signTypedData_v4 object: %v", err)
	}
	hash, _, err := apitypes.TypedDataAndHash(td)
	if err != nil {
		t.Fatalf("TypedDataAndHash: %v", err)
	}
	if recoverSigner(t, hash, sigObj) != addr {
		t.Fatalf("object form recovered wrong address")
	}

	// String form (ethers.js style).
	payload, err := json.Marshal(td)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var sigStr hexutil.Bytes
	if err := client.CallContext(context.Background(), &sigStr, "eth_signTypedData_v4", addr, string(payload)); err != nil {
		t.Fatalf("eth_signTypedData_v4 string: %v", err)
	}
	if recoverSigner(t, hash, sigStr) != addr {
		t.Fatalf("string form recovered wrong address")
	}
	if string(sigObj) != string(sigStr) {
		t.Fatalf("object and string signatures differ")
	}
}

func TestSignTypedData_v4_InvalidTypedData(t *testing.T) {
	mgr, addr := loadSigningFixture(t)
	testAPI := NewTestEthAPI(api.NewEthAPI(nil), nil, mgr.Addresses, mgr.PrivateKeys, &txFence{})
	// Empty primary type / types → hash failure.
	_, err := testAPI.SignTypedData_v4(context.Background(), addr, typedDataArg{})
	if err == nil {
		t.Fatal("expected error for invalid typed data")
	}
}

func TestTypedDataArg_UnmarshalJSON(t *testing.T) {
	td := permitTypedData("0xCD2a3d9F938E13CD947Ec05AbC7FE734Df8DD826", "1")
	raw, err := json.Marshal(td)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var asObj typedDataArg
	if err := json.Unmarshal(raw, &asObj); err != nil {
		t.Fatalf("object: %v", err)
	}
	if asObj.PrimaryType != "Permit" {
		t.Fatalf("PrimaryType = %q", asObj.PrimaryType)
	}

	quoted, err := json.Marshal(string(raw))
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	var asStr typedDataArg
	if err := json.Unmarshal(quoted, &asStr); err != nil {
		t.Fatalf("string: %v", err)
	}
	if asStr.PrimaryType != "Permit" {
		t.Fatalf("string PrimaryType = %q", asStr.PrimaryType)
	}

	// Leading whitespace on the raw token (call UnmarshalJSON directly: top-level
	// json.Unmarshal strips spaces before invoking the method).
	var asPadded typedDataArg
	if err := asPadded.UnmarshalJSON(append([]byte("  "), quoted...)); err != nil {
		t.Fatalf("padded string: %v", err)
	}
	if asPadded.PrimaryType != "Permit" {
		t.Fatalf("padded PrimaryType = %q", asPadded.PrimaryType)
	}
}

// permitTypedData is a minimal EIP-2612-style payload for signing tests.
func permitTypedData(owner, chainID string) apitypes.TypedData {
	// math.HexOrDecimal256 unmarshals from JSON numbers/strings; build via JSON
	// so chainId is populated the same way production RPC calls do.
	raw := []byte(`{
		"types": {
			"EIP712Domain": [
				{"name":"name","type":"string"},
				{"name":"version","type":"string"},
				{"name":"chainId","type":"uint256"},
				{"name":"verifyingContract","type":"address"}
			],
			"Permit": [
				{"name":"owner","type":"address"},
				{"name":"spender","type":"address"},
				{"name":"value","type":"uint256"},
				{"name":"nonce","type":"uint256"},
				{"name":"deadline","type":"uint256"}
			]
		},
		"primaryType": "Permit",
		"domain": {
			"name": "MyToken",
			"version": "1",
			"chainId": ` + chainID + `,
			"verifyingContract": "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC"
		},
		"message": {
			"owner": "` + owner + `",
			"spender": "0xbBbBBBBbbBBBbbbBbbBbbbbBBbBbbbbBbBbbBBbB",
			"value": "1000",
			"nonce": "0",
			"deadline": "115792089237316195423570985008687907853269984665640564039457584007913129639935"
		}
	}`)
	var td apitypes.TypedData
	if err := json.Unmarshal(raw, &td); err != nil {
		panic(err)
	}
	return td
}
