/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/hyperledger/fabric-x-evm/utils"
)

// TestGenUncorrelatedDataset writes testdata/USDC_dataset_uncorrelated.json.gz
// containing N txs each from a unique sender to a unique recipient. Run with:
//
//	go test -run '^TestGenUncorrelatedDataset$' -v ./integration/perf
//
// Configurable via PERF_GEN_N (default 3000).
func TestGenUncorrelatedDataset(t *testing.T) {
	n := 3000
	if v := os.Getenv("PERF_GEN_N"); v != "" {
		_, _ = fmt.Sscanf(v, "%d", &n)
	}
	chainID := big.NewInt(4011)
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	signer := types.LatestSignerForChainID(chainID)

	transferSelector := []byte{0xa9, 0x05, 0x9c, 0xbb} // transfer(address,uint256)
	amount := new(big.Int).SetUint64(1_000_000)        // 1 USDC (6 decimals)

	transfers := make([]utils.TokenTransfer, n)
	for i := 0; i < n; i++ {
		key, err := crypto.GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		sender := crypto.PubkeyToAddress(key.PublicKey)

		recipKey, err := crypto.GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		recipient := crypto.PubkeyToAddress(recipKey.PublicKey)

		callData := make([]byte, 0, 68)
		callData = append(callData, transferSelector...)
		callData = append(callData, common.LeftPadBytes(recipient.Bytes(), 32)...)
		callData = append(callData, common.LeftPadBytes(amount.Bytes(), 32)...)

		tx := types.NewTx(&types.LegacyTx{
			Nonce:    0, // NonceBypassGateway ignores
			GasPrice: big.NewInt(1),
			Gas:      100_000,
			To:       &usdc,
			Value:    big.NewInt(0),
			Data:     callData,
		})
		signed, err := types.SignTx(tx, signer, key)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := signed.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}

		transfers[i] = utils.TokenTransfer{
			BlockID:         uint64(i),
			TransactionHash: signed.Hash(),
			Time:            time.Unix(int64(i), 0),
			TokenAddress:    usdc,
			Sender:          sender,
			Recipient:       recipient,
			Value:           uint256.NewInt(amount.Uint64()),
			TokenName:       "USD//C",
			TokenSymbol:     "USDC",
			TokenDecimals:   6,
			Transaction:     raw,
		}
	}

	out := "testdata/USDC_dataset_uncorrelated.json.gz"
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	enc := json.NewEncoder(gz)
	if err := enc.Encode(transfers); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d uncorrelated txs to %s", n, out)
}
