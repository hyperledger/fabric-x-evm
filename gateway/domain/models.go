/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package domain

import (
	"github.com/ethereum/go-ethereum/core/types"
)

type Block struct {
	BlockNumber  uint64
	BlockHash    []byte
	ParentHash   []byte
	Timestamp    int64
	Transactions []Transaction // populated when full=true in API requests
}

type Transaction struct {
	TxHash          []byte
	BlockHash       []byte
	BlockNumber     uint64
	TxIndex         int64
	RawTx           []byte
	FromAddress     []byte
	ToAddress       []byte
	ContractAddress []byte
	Status          uint8
	FabricTxID      string
	FabricTxStatus  int
	Logs            []Log // populated for receipt queries
}

// ToEthTx converts a domain Transaction to an ethereum types.Transaction.
// This unmarshals the raw RLP-encoded transaction.
func (t *Transaction) ToEthTx() *types.Transaction {
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(t.RawTx); err != nil {
		// Return nil on error - callers should handle this gracefully
		return nil
	}
	return tx
}

// TxType extracts the transaction type from the raw transaction bytes.
// Returns 0 for legacy transactions, or the type byte for typed transactions (EIP-2718).
func (t *Transaction) TxType() uint8 {
	if len(t.RawTx) == 0 {
		return 0
	}
	// EIP-2718: if first byte < 0xc0, it's the type; otherwise legacy (0)
	if t.RawTx[0] < 0xc0 {
		return t.RawTx[0]
	}
	return 0
}

// Log represents an ethereum log entry.
type Log struct {
	BlockNumber uint64
	TxHash      []byte
	TxIndex     int64
	LogIndex    int64
	Address     []byte
	Topics      [][]byte
	Data        []byte
}

// LogFilter contains options for filtering logs.
// Modeled after go-ethereum's FilterQuery.
type LogFilter struct {
	BlockHash *[]byte  // return logs only from block with this hash
	FromBlock *uint64  // beginning of the queried range, nil means genesis block
	ToBlock   *uint64  // end of the range, nil means latest block
	Addresses [][]byte // restricts matches to events created by specific contracts

	// Topics restricts matches to particular event topics. Each event has a list
	// of topics. Topics matches a prefix of that list. An empty element slice matches any
	// topic. Non-empty elements represent an alternative that matches any of the
	// contained topics.
	//
	// Examples:
	// {} or nil          matches any topic list
	// {{A}}              matches topic A in first position
	// {{}, {B}}          matches any topic in first position AND B in second position
	// {{A}, {B}}         matches topic A in first position AND B in second position
	// {{A, B}, {C, D}}   matches topic (A OR B) in first position AND (C OR D) in second position
	Topics [][][]byte
}
