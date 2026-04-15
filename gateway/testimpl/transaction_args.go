/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later

This file contains TransactionArgs struct adapted from go-ethereum:
https://github.com/ethereum/go-ethereum/blob/master/internal/ethapi/api.go

WARNING: This package contains test-only/unsafe RPC implementations.
DO NOT use in production environments. These methods perform server-side
transaction signing which is inherently insecure.
*/

package testimpl

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// TransactionArgs represents the arguments to construct a new transaction
// or a message call. Adapted from geth's internal/ethapi/api.go
type TransactionArgs struct {
	From                 *common.Address `json:"from"`
	To                   *common.Address `json:"to"`
	Gas                  *hexutil.Uint64 `json:"gas"`
	GasPrice             *hexutil.Big    `json:"gasPrice"`
	MaxFeePerGas         *hexutil.Big    `json:"maxFeePerGas"`
	MaxPriorityFeePerGas *hexutil.Big    `json:"maxPriorityFeePerGas"`
	Value                *hexutil.Big    `json:"value"`
	Nonce                *hexutil.Uint64 `json:"nonce"`
	Data                 *hexutil.Bytes  `json:"data"`
	Input                *hexutil.Bytes  `json:"input"`

	// Additional fields for access list transactions
	ChainID *hexutil.Big `json:"chainId,omitempty"`
}

// data retrieves the transaction calldata. Input field is preferred.
func (args *TransactionArgs) data() []byte {
	if args.Input != nil {
		return *args.Input
	}
	if args.Data != nil {
		return *args.Data
	}
	return nil
}

// setDefaults fills in default values for unspecified tx fields.
func (args *TransactionArgs) setDefaults() {
	if args.Gas == nil {
		gas := hexutil.Uint64(21000)
		args.Gas = &gas
	}
	if args.GasPrice == nil && args.MaxFeePerGas == nil {
		price := hexutil.Big(*big.NewInt(1))
		args.GasPrice = &price
	}
	if args.Value == nil {
		value := hexutil.Big(*big.NewInt(0))
		args.Value = &value
	}
}
