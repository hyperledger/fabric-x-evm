/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package integration

import (
	"encoding/json"
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
func buildTransaction(testTx *stTransaction, dataIndex, gasIndex, valueIndex int) (*types.Transaction, error) {
	// Parse transaction fields - stTransaction already has parsed values
	nonce := testTx.Nonce

	if gasIndex >= len(testTx.GasLimit) {
		return nil, fmt.Errorf("gasLimit index %d out of bounds", gasIndex)
	}
	gasLimit := testTx.GasLimit[gasIndex]

	gasPrice := testTx.GasPrice
	if gasPrice == nil {
		gasPrice = big.NewInt(0)
	}

	if valueIndex >= len(testTx.Value) {
		return nil, fmt.Errorf("value index %d out of bounds", valueIndex)
	}
	value, err := hexToBigInt(testTx.Value[valueIndex])
	if err != nil {
		return nil, fmt.Errorf("invalid value: %w", err)
	}

	if dataIndex >= len(testTx.Data) {
		return nil, fmt.Errorf("data index %d out of bounds", dataIndex)
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
	if len(testTx.PrivateKey) > 0 {
		key, err := crypto.ToECDSA(testTx.PrivateKey)
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
func buildBlockInfo(env *stEnv) (*utils.BlockInfo, error) {
	blockNum := big.NewInt(int64(env.Number))
	blockTime := env.Timestamp

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

// ParseTestFile reads and parses an Ethereum test JSON file into StateTest format
func ParseTestFile(path string) (map[string]*StateTest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var tests map[string]*StateTest
	if err := json.Unmarshal(data, &tests); err != nil {
		return nil, err
	}

	return tests, nil
}
