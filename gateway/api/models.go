/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package api

import (
	"encoding/json"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

type rpcReceipt struct {
	types.Receipt
	From common.Address  `json:"from"`
	To   *common.Address `json:"to"`
}

// MarshalJSON ensures that the required fields are all present in the correct form,
// preserving the embedded fields and correct handling of nil values.
func (r *rpcReceipt) MarshalJSON() ([]byte, error) {
	base, err := json.Marshal(r.Receipt)
	if err != nil {
		return nil, err
	}

	var m map[string]any
	if err := json.Unmarshal(base, &m); err != nil {
		return nil, err
	}

	m["from"] = r.From.Hex()
	if r.To != nil {
		m["to"] = r.To.Hex()
	} else {
		m["to"] = nil
	}

	return json.Marshal(m)
}

// receipt returns a receipt in the form the RPC API can return. Some values are mocked.
func receipt(r *domain.Transaction) *rpcReceipt {
	if r == nil {
		return nil
	}

	contractAddr := common.Address{}
	if len(r.ContractAddress) == common.AddressLength {
		contractAddr = common.BytesToAddress(r.ContractAddress)
	}
	var to *common.Address
	if len(r.ToAddress) == common.AddressLength {
		a := common.BytesToAddress(r.ToAddress)
		to = &a
	}

	logs := make([]*types.Log, len(r.Logs))
	for i, l := range r.Logs {
		logs[i] = domainLogToTypesLog(l)
	}

	return &rpcReceipt{
		Receipt: types.Receipt{
			Status:            uint64(r.Status),
			CumulativeGasUsed: 0,
			// Bloom:             types.Bloom(r.LogsBloom),
			Logs:              logs,
			TxHash:            common.BytesToHash(r.TxHash),
			ContractAddress:   contractAddr,
			GasUsed:           0,
			Type:              uint8(r.TxType()),
			PostState:         nil,
			EffectiveGasPrice: big.NewInt(0),
			BlobGasUsed:       0,
			BlobGasPrice:      big.NewInt(0),
			BlockHash:         common.Hash(r.BlockHash),
			BlockNumber:       new(big.Int).SetUint64(uint64(r.BlockNumber)),
			TransactionIndex:  uint(r.TxIndex),
		},
		From: common.BytesToAddress(r.FromAddress),
		To:   to,
	}
}

// RPCTransaction represents a transaction that will be returned to an RPC client.
// This includes the transaction itself plus block metadata.
type RPCTransaction struct {
	tx               *types.Transaction // not exported, won't be marshaled
	From             common.Address     `json:"from"`
	BlockHash        *common.Hash       `json:"blockHash"`
	BlockNumber      *hexutil.Big       `json:"blockNumber"`
	TransactionIndex *hexutil.Uint64    `json:"transactionIndex"`
}

// MarshalJSON marshals the transaction with block metadata
func (r *RPCTransaction) MarshalJSON() ([]byte, error) {
	// Marshal the embedded transaction first
	txJSON, err := r.tx.MarshalJSON()
	if err != nil {
		return nil, err
	}

	// Unmarshal into a map so we can add our fields
	var m map[string]any
	if err := json.Unmarshal(txJSON, &m); err != nil {
		return nil, err
	}

	// Remove internal go-ethereum fields that shouldn't be exposed
	delete(m, "ignore")

	// Add block metadata and sender - these override any fields from the transaction
	m["from"] = r.From
	m["blockHash"] = r.BlockHash
	m["blockNumber"] = r.BlockNumber
	m["transactionIndex"] = r.TransactionIndex

	return json.Marshal(m)
}

type RPCBlock struct {
	Number           hexutil.Uint64   `json:"number"`
	Hash             common.Hash      `json:"hash"`
	ParentHash       common.Hash      `json:"parentHash"`
	Sha3Uncles       common.Hash      `json:"sha3Uncles"`
	LogsBloom        hexutil.Bytes    `json:"logsBloom"`
	TransactionsRoot common.Hash      `json:"transactionsRoot"`
	StateRoot        common.Hash      `json:"stateRoot"`
	ReceiptsRoot     common.Hash      `json:"receiptsRoot"`
	Miner            common.Address   `json:"miner"`
	Difficulty       hexutil.Big      `json:"difficulty"`
	TotalDifficulty  hexutil.Big      `json:"totalDifficulty"`
	ExtraData        hexutil.Bytes    `json:"extraData"`
	Size             hexutil.Uint64   `json:"size"`
	GasLimit         hexutil.Uint64   `json:"gasLimit"`
	GasUsed          hexutil.Uint64   `json:"gasUsed"`
	BaseFeePerGas    hexutil.Big      `json:"baseFeePerGas"`
	Timestamp        hexutil.Uint64   `json:"timestamp"`
	Transactions     []any            `json:"transactions"`
	Uncles           []common.Hash    `json:"uncles"`
	MixHash          common.Hash      `json:"mixHash"`
	Nonce            types.BlockNonce `json:"nonce"`
}

// rpcTransaction converts a domain.Transaction to an RPCTransaction with block metadata.
// Returns nil if the transaction cannot be converted.
func rpcTransaction(tx *domain.Transaction) *RPCTransaction {
	if tx == nil {
		return nil
	}

	ethTx := tx.ToEthTx()
	if ethTx == nil {
		return nil
	}

	blockHash := common.Hash(tx.BlockHash)
	blockNumber := hexutil.Big(*big.NewInt(int64(tx.BlockNumber)))
	txIndex := hexutil.Uint64(tx.TxIndex)

	return &RPCTransaction{
		tx:               ethTx,
		From:             common.BytesToAddress(tx.FromAddress),
		BlockHash:        &blockHash,
		BlockNumber:      &blockNumber,
		TransactionIndex: &txIndex,
	}
}

// rpcBlock returns a block in the form the RPC API can return. Some values are mocked.
// If b.Transactions is populated, it includes full transaction objects (when full=true).
// If b.Transactions is empty, it returns an empty array (when full=false or no transactions).
func rpcBlock(b *domain.Block, full bool) *RPCBlock {
	if b == nil {
		return nil
	}

	// Build transactions array based on full flag and available data
	var transactions []any
	if full && len(b.Transactions) > 0 {
		// Return full transaction objects with block metadata
		transactions = make([]any, 0, len(b.Transactions))
		blockHash := common.Hash(b.BlockHash)
		blockNumber := hexutil.Big(*big.NewInt(int64(b.BlockNumber)))

		for _, tx := range b.Transactions {
			ethTx := tx.ToEthTx()
			if ethTx != nil {
				txIndex := hexutil.Uint64(tx.TxIndex)
				rpcTx := &RPCTransaction{
					tx:               ethTx,
					From:             common.BytesToAddress(tx.FromAddress),
					BlockHash:        &blockHash,
					BlockNumber:      &blockNumber,
					TransactionIndex: &txIndex,
				}
				transactions = append(transactions, rpcTx)
			}
			// Skip transactions that fail to unmarshal
		}
	} else if !full && len(b.Transactions) > 0 {
		// Return just transaction hashes
		transactions = make([]any, len(b.Transactions))
		for i, tx := range b.Transactions {
			transactions[i] = common.BytesToHash(tx.TxHash)
		}
	} else {
		// No transactions or data not loaded
		transactions = []any{}
	}

	return &RPCBlock{
		Number:           hexutil.Uint64(b.BlockNumber),
		Hash:             (common.Hash)(b.BlockHash),
		ParentHash:       (common.Hash)(b.ParentHash),
		Sha3Uncles:       common.Hash{},
		LogsBloom:        make(hexutil.Bytes, types.BloomByteLength),
		TransactionsRoot: common.Hash{},
		StateRoot:        common.BytesToHash(b.StateRoot),
		ReceiptsRoot:     common.Hash{},
		Miner:            common.Address{}, // b.Miner
		Difficulty:       hexutil.Big(*big.NewInt(0)),
		TotalDifficulty:  hexutil.Big(*big.NewInt(0)),
		ExtraData:        hexutil.Bytes{},
		Size:             0,
		GasLimit:         hexutil.Uint64(0),
		GasUsed:          hexutil.Uint64(0),
		BaseFeePerGas:    hexutil.Big(*big.NewInt(0)),
		Timestamp:        hexutil.Uint64(b.Timestamp),
		Transactions:     transactions,
		Uncles:           []common.Hash{},
	}
}

type FeeHistoryResult struct {
	OldestBlock  *hexutil.Big     `json:"oldestBlock"`
	BaseFee      []*hexutil.Big   `json:"baseFeePerGas"`
	GasUsedRatio []float64        `json:"gasUsedRatio"`
	Reward       [][]*hexutil.Big `json:"reward,omitempty"`
}
