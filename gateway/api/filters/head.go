/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package filters

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// rpcHead is the newHeads notification payload. Hash is the Fabric block hash
// (not keccak of the stubbed header fields), matching eth_getBlockBy* identity.
type rpcHead struct {
	Number           *hexutil.Big     `json:"number"`
	Hash             common.Hash      `json:"hash"`
	ParentHash       common.Hash      `json:"parentHash"`
	Sha3Uncles       common.Hash      `json:"sha3Uncles"`
	LogsBloom        hexutil.Bytes    `json:"logsBloom"`
	TransactionsRoot common.Hash      `json:"transactionsRoot"`
	StateRoot        common.Hash      `json:"stateRoot"`
	ReceiptsRoot     common.Hash      `json:"receiptsRoot"`
	Miner            common.Address   `json:"miner"`
	Difficulty       *hexutil.Big     `json:"difficulty"`
	ExtraData        hexutil.Bytes    `json:"extraData"`
	GasLimit         hexutil.Uint64   `json:"gasLimit"`
	GasUsed          hexutil.Uint64   `json:"gasUsed"`
	Timestamp        hexutil.Uint64   `json:"timestamp"`
	MixHash          common.Hash      `json:"mixHash"`
	Nonce            types.BlockNonce `json:"nonce"`
}

func headFromBlock(b blocks.Block) *rpcHead {
	zero := (*hexutil.Big)(new(big.Int))
	return &rpcHead{
		Number:           (*hexutil.Big)(new(big.Int).SetUint64(b.Number)),
		Hash:             common.BytesToHash(b.Hash),
		ParentHash:       common.BytesToHash(b.ParentHash),
		Sha3Uncles:       types.EmptyUncleHash,
		LogsBloom:        make(hexutil.Bytes, 256),
		TransactionsRoot: types.EmptyRootHash,
		StateRoot:        types.EmptyRootHash,
		ReceiptsRoot:     types.EmptyRootHash,
		Miner:            common.HexToAddress("0x0000000000000000000000000000000000000F4B"),
		Difficulty:       zero,
		ExtraData:        hexutil.Bytes{},
		Timestamp:        hexutil.Uint64(b.Timestamp),
	}
}
