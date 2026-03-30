/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hyperledger/fabric-x-evm/utils"
)

// hexToBigInt converts "0x..." string to *big.Int
func hexToBigInt(s string) (*big.Int, error) {
	if s == "" || s == "0x" || s == "0x0" {
		return big.NewInt(0), nil
	}

	s = strings.TrimPrefix(s, "0x")
	n := new(big.Int)
	if _, ok := n.SetString(s, 16); !ok {
		return nil, fmt.Errorf("invalid hex: %s", s)
	}
	return n, nil
}

// hexToUint64 converts "0x..." string to uint64
func hexToUint64(s string) (uint64, error) {
	n, err := hexToBigInt(s)
	if err != nil {
		return 0, err
	}
	if !n.IsUint64() {
		return 0, fmt.Errorf("value too large for uint64: %s", s)
	}
	return n.Uint64(), nil
}

// hexToBytes converts "0x..." string to []byte
func hexToBytes(s string) ([]byte, error) {
	if s == "" || s == "0x" {
		return []byte{}, nil
	}
	return hexutil.Decode(s)
}

// hexToAddress converts "0x..." string to common.Address
func hexToAddress(s string) common.Address {
	return common.HexToAddress(s)
}

// buildTransaction creates a signed transaction from test data
func buildTransaction(testTx TestTransaction, dataIndex int) (*types.Transaction, error) {
	// Parse transaction fields
	nonce, err := hexToUint64(testTx.Nonce)
	if err != nil {
		return nil, fmt.Errorf("invalid nonce: %w", err)
	}

	gasLimit, err := hexToUint64(testTx.GasLimit[dataIndex])
	if err != nil {
		return nil, fmt.Errorf("invalid gasLimit: %w", err)
	}

	gasPrice, err := hexToBigInt(testTx.GasPrice)
	if err != nil {
		return nil, fmt.Errorf("invalid gasPrice: %w", err)
	}

	value, err := hexToBigInt(testTx.Value[dataIndex])
	if err != nil {
		return nil, fmt.Errorf("invalid value: %w", err)
	}

	data, err := hexToBytes(testTx.Data[dataIndex])
	if err != nil {
		return nil, fmt.Errorf("invalid data: %w", err)
	}

	// Parse recipient (empty for contract creation)
	var to *common.Address
	if testTx.To != "" && testTx.To != "0x" {
		addr := hexToAddress(testTx.To)
		to = &addr
	}

	// Create transaction
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: gasPrice,
		Gas:      gasLimit,
		To:       to,
		Value:    value,
		Data:     data,
	})

	// Sign transaction if secret key is provided
	if testTx.SecretKey != "" {
		key, err := crypto.HexToECDSA(strings.TrimPrefix(testTx.SecretKey, "0x"))
		if err != nil {
			return nil, fmt.Errorf("invalid secret key: %w", err)
		}

		// Use HomesteadSigner for simple tests (pre-EIP155)
		signer := types.HomesteadSigner{}
		signedTx, err := types.SignTx(tx, signer, key)
		if err != nil {
			return nil, fmt.Errorf("failed to sign tx: %w", err)
		}
		return signedTx, nil
	}

	return tx, nil
}

// buildBlockInfo creates block context from test environment
func buildBlockInfo(env TestEnv) (*utils.BlockInfo, error) {
	blockNum, err := hexToBigInt(env.CurrentNumber)
	if err != nil {
		return nil, fmt.Errorf("invalid block number: %w", err)
	}

	blockTime, err := hexToUint64(env.CurrentTimestamp)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp: %w", err)
	}

	return &utils.BlockInfo{
		BlockNumber: blockNum,
		BlockTime:   blockTime,
	}, nil
}

// GetTestPath resolves a test file path, checking if it exists
func GetTestPath(relativePath string) (string, error) {
	// Try relative to current directory
	if _, err := os.Stat(relativePath); err == nil {
		return relativePath, nil
	}

	// Try relative to project root
	projectRoot, err := findProjectRoot()
	if err == nil {
		fullPath := filepath.Join(projectRoot, relativePath)
		if _, err := os.Stat(fullPath); err == nil {
			return fullPath, nil
		}
	}

	return "", fmt.Errorf("test file not found: %s", relativePath)
}
