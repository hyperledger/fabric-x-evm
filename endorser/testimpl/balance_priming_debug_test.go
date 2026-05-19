/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl_test

import (
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	fxcommon "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser"
	"github.com/hyperledger/fabric-x-evm/endorser/testimpl"
)

// TestBalancePrimingDebug reproduces the USDC transfer failure with balance priming.
// It runs locally without a real Fabric network by directly writing contract state to LightKVS.
func TestBalancePrimingDebug(t *testing.T) {
	// ---------- Load USDC contract state from JSON ----------
	contractJSON, err := os.ReadFile("../../integration/perf/testdata/USDC_contract.json")
	if err != nil {
		t.Skipf("USDC contract JSON not found (run from repo root): %v", err)
	}

	var alloc map[string]struct {
		Code    string            `json:"code"`
		Nonce   string            `json:"nonce"`
		Balance string            `json:"balance"`
		Storage map[string]string `json:"storage"`
	}
	if err := json.Unmarshal(contractJSON, &alloc); err != nil {
		t.Fatal(err)
	}

	// ---------- Load Transfer 0 from dataset ----------
	f, err := os.Open("../../integration/perf/testdata/USDC_dataset.json.gz")
	if err != nil {
		t.Skipf("USDC dataset not found: %v", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}

	var transfers []struct {
		Sender      string `json:"Sender"`
		Transaction string `json:"Transaction"`
	}
	if err := json.NewDecoder(gz).Decode(&transfers); err != nil {
		t.Fatal(err)
	}

	if len(transfers) == 0 || transfers[0].Transaction == "" {
		t.Fatal("no Transfer 0 in dataset")
	}

	txBytes, err := base64.StdEncoding.DecodeString(transfers[0].Transaction)
	if err != nil {
		t.Fatalf("base64 decode Transfer 0: %v", err)
	}

	var tx types.Transaction
	if err := tx.UnmarshalBinary(txBytes); err != nil {
		t.Fatalf("unmarshal Transfer 0: %v", err)
	}

	t.Logf("Transfer 0: nonce=%d gasPrice=%s gas=%d to=%s value=%s",
		tx.Nonce(), tx.GasPrice(), tx.Gas(), tx.To(), tx.Value())

	// ---------- Prime LightKVS with USDC state ----------
	namespace := "basic"
	kvs := endorser.NewLightKVS(1)

	var updates []endorser.KeyValueVersion
	blockNum := uint64(1)

	for addrStr, entry := range alloc {
		addr := common.HexToAddress(addrStr)

		if entry.Code != "" && entry.Code != "0x" {
			code := common.FromHex(entry.Code)
			key := namespace + ":acc:" + addr.Hex() + ":code"
			updates = append(updates, endorser.KeyValueVersion{
				Key: key, Value: code, BlockNum: blockNum,
			})
			t.Logf("Priming code for %s: %d bytes", addr.Hex(), len(code))
		}

		for slotStr, valStr := range entry.Storage {
			slotHash := common.HexToHash(slotStr)
			valHash := common.HexToHash(valStr)
			key := namespace + ":str:" + addr.Hex() + ":" + slotHash.Hex()
			updates = append(updates, endorser.KeyValueVersion{
				Key: key, Value: valHash.Bytes(), BlockNum: blockNum,
			})
			t.Logf("Priming storage slot %s @ %s = %s", slotStr[:10]+"...", addr.Hex()[:10]+"...", valStr[:10]+"...")
		}
	}

	if err := kvs.Update(updates); err != nil {
		t.Fatal(err)
	}

	// ---------- Create BalancePrimingExecutor ----------
	usdcAddr := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	bpConfig := &testimpl.BalancePrimingConfig{
		Enabled:         true,
		ContractAddress: usdcAddr,
		MappingPosition: 9,
	}

	chainID := int64(4011)
	evmConfig := endorser.EVMConfig{
		ChainConfig: fxcommon.BuildChainConfig(chainID),
		FreeGas:     false,
	}

	executor, err := testimpl.NewBalancePrimingExecutor(
		namespace, kvs, nil, 0, evmConfig, false, bpConfig,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer executor.Close()

	// ---------- Execute Transfer 0 ----------
	t.Logf("Executing Transfer 0 (hash=%s)...", tx.Hash().Hex())
	result, execErr := executor.Execute(&tx)

	fmt.Printf("=== Result ===\n")
	fmt.Printf("Error: %v\n", execErr)
	fmt.Printf("Status: %d\n", result.Status)
	fmt.Printf("RWS len: %d\n", len(result.RWS.Writes))
	if execErr != nil {
		t.Logf("Execute error: %v", execErr)
	} else {
		t.Logf("Execute success: status=%d rwsWrites=%d", result.Status, len(result.RWS.Writes))
	}
}
