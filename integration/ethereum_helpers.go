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
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
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
func buildTransaction(testTx *stTransaction, dataIndex, gasIndex, valueIndex int, config *params.ChainConfig, blockNumber *big.Int, blockTime uint64) (*types.Transaction, error) {
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

	// Handle blob transactions (EIP-4844) - type 3
	// Only create blob transaction if BlobGasFeeCap is explicitly set (not nil)
	// Note: BlobVersionedHashes can be empty for validation tests
	if testTx.BlobGasFeeCap != nil {
		maxFeePerGas := testTx.MaxFeePerGas
		if maxFeePerGas == nil {
			maxFeePerGas = gasPrice
		}
		maxPriorityFeePerGas := testTx.MaxPriorityFeePerGas
		if maxPriorityFeePerGas == nil {
			maxPriorityFeePerGas = gasPrice
		}
		blobFeeCap := testTx.BlobGasFeeCap
		if blobFeeCap == nil {
			blobFeeCap = big.NewInt(0)
		}

		// Handle access list
		var accessList types.AccessList
		if testTx.AccessLists != nil && dataIndex < len(testTx.AccessLists) && testTx.AccessLists[dataIndex] != nil {
			accessList = *testTx.AccessLists[dataIndex]
		}

		tx := types.NewTx(&types.BlobTx{
			Nonce:      nonce,
			GasTipCap:  uint256.MustFromBig(maxPriorityFeePerGas),
			GasFeeCap:  uint256.MustFromBig(maxFeePerGas),
			Gas:        gasLimit,
			To:         *to, // Blob transactions must have a recipient
			Value:      uint256.MustFromBig(value),
			Data:       data,
			AccessList: accessList,
			BlobFeeCap: uint256.MustFromBig(blobFeeCap),
			BlobHashes: testTx.BlobVersionedHashes,
		})

		// Sign transaction if secret key is provided
		if len(testTx.PrivateKey) > 0 {
			key, err := crypto.ToECDSA(testTx.PrivateKey)
			if err != nil {
				return nil, fmt.Errorf("invalid secret key: %w", err)
			}

			signer := types.LatestSignerForChainID(big.NewInt(1))
			signedTx, err := types.SignTx(tx, signer, key)
			if err != nil {
				return nil, fmt.Errorf("failed to sign tx: %w", err)
			}
			return signedTx, nil
		}

		return tx, nil
	}

	// Handle EIP-1559 transactions
	if testTx.MaxFeePerGas != nil || testTx.MaxPriorityFeePerGas != nil {
		maxFeePerGas := testTx.MaxFeePerGas
		if maxFeePerGas == nil {
			maxFeePerGas = gasPrice
		}
		maxPriorityFeePerGas := testTx.MaxPriorityFeePerGas
		if maxPriorityFeePerGas == nil {
			maxPriorityFeePerGas = gasPrice
		}

		// Handle access list
		var accessList types.AccessList
		if testTx.AccessLists != nil && dataIndex < len(testTx.AccessLists) && testTx.AccessLists[dataIndex] != nil {
			accessList = *testTx.AccessLists[dataIndex]
		}

		tx := types.NewTx(&types.DynamicFeeTx{
			Nonce:      nonce,
			GasTipCap:  maxPriorityFeePerGas,
			GasFeeCap:  maxFeePerGas,
			Gas:        gasLimit,
			To:         to,
			Value:      value,
			Data:       data,
			AccessList: accessList,
		})

		// Sign transaction if secret key is provided
		if len(testTx.PrivateKey) > 0 {
			key, err := crypto.ToECDSA(testTx.PrivateKey)
			if err != nil {
				return nil, fmt.Errorf("invalid secret key: %w", err)
			}

			// Use appropriate signer for EIP-1559
			signer := types.LatestSignerForChainID(big.NewInt(1))
			signedTx, err := types.SignTx(tx, signer, key)
			if err != nil {
				return nil, fmt.Errorf("failed to sign tx: %w", err)
			}
			return signedTx, nil
		}

		return tx, nil
	}

	// Check if we have an access list - if so, create AccessListTx (EIP-2930)
	// instead of legacy transaction
	var accessList types.AccessList
	if testTx.AccessLists != nil && dataIndex < len(testTx.AccessLists) && testTx.AccessLists[dataIndex] != nil {
		accessList = *testTx.AccessLists[dataIndex]
	}

	// Get the appropriate signer based on chain config and block context
	// This ensures we use the same signer for signing and validation
	signer := types.MakeSigner(config, blockNumber, blockTime)

	// Sign transaction if secret key is provided
	if len(testTx.PrivateKey) > 0 {
		key, err := crypto.ToECDSA(testTx.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("invalid secret key: %w", err)
		}

		if len(accessList) > 0 {
			// Create and sign EIP-2930 AccessListTx
			txData := &types.AccessListTx{
				ChainID:    config.ChainID,
				Nonce:      nonce,
				GasPrice:   gasPrice,
				Gas:        gasLimit,
				To:         to,
				Value:      value,
				Data:       data,
				AccessList: accessList,
			}
			return types.SignNewTx(key, signer, txData)
		} else {
			// Create and sign legacy transaction
			txData := &types.LegacyTx{
				Nonce:    nonce,
				GasPrice: gasPrice,
				Gas:      gasLimit,
				To:       to,
				Value:    value,
				Data:     data,
			}
			return types.SignNewTx(key, signer, txData)
		}
	}

	// No private key - return unsigned transaction
	if len(accessList) > 0 {
		return types.NewTx(&types.AccessListTx{
			ChainID:    config.ChainID,
			Nonce:      nonce,
			GasPrice:   gasPrice,
			Gas:        gasLimit,
			To:         to,
			Value:      value,
			Data:       data,
			AccessList: accessList,
		}), nil
	}

	return types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: gasPrice,
		Gas:      gasLimit,
		To:       to,
		Value:    value,
		Data:     data,
	}), nil
}

// buildBlockInfo creates block context from test environment
func buildBlockInfo(env *stEnv) (*utils.BlockInfo, error) {
	blockNum := big.NewInt(int64(env.Number))
	blockTime := env.Timestamp

	return &utils.BlockInfo{
		BlockNumber: blockNum,
		BlockTime:   blockTime,
		GasLimit:    env.GasLimit,
	}, nil
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
