/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

func TestNilBlockNum(t *testing.T) {
	nonce := uint64(0)
	to := common.HexToAddress("0xcafe")
	value := big.NewInt(1e18)
	gasLimit := uint64(21000)
	maxFeePerGas := big.NewInt(20000000000)
	maxPriorityFeePerGas := big.NewInt(2000000000)
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     nonce,
		GasFeeCap: maxFeePerGas,
		GasTipCap: maxPriorityFeePerGas,
		Gas:       gasLimit,
		To:        &to,
		Value:     value,
		Data:      nil,
	})

	privateKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("HexToECDSA failed: %s", err)
	}

	rawTx, err := tx.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %s", err)
	}

	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)

	r := receipt(&domain.Transaction{
		TxHash:      tx.Hash().Bytes(),
		BlockHash:   nil, // nil signals pending to API layer
		BlockNumber: 0,   // 0 signals pending to API layer
		TxIndex:     0,   // Value doesn't matter - API layer checks BlockNumber==0 for pending
		RawTx:       rawTx,
		FromAddress: fromAddress.Bytes(),
		ToAddress:   to.Bytes(),
	})

	if r != nil {
		t.Fatalf("Expected nil receipt, got %v", r)
	}
}
