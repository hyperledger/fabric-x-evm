/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-evm/gateway/storage/trie"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

func NewEthBlockPersister(db BlockInserter, ts *trie.Store) *EthBlockPersister {
	return &EthBlockPersister{db: db, ts: ts}
}

type EthBlockPersister struct {
	db BlockInserter
	ts *trie.Store
}

type BlockInserter interface {
	InsertBlock(ctx context.Context, b domain.Block) error
}

func (e EthBlockPersister) Handle(ctx context.Context, b blocks.Block) error {
	ebl := convertToDomain(b)

	stateRoot, err := e.ts.Commit(ctx, b)
	if err != nil {
		return err // irrecoverable
	}
	ebl.StateRoot = stateRoot.Bytes()
	// TODO: use proper headers. We now add some mock values to make sure we're not violating unique constraints.
	ebl.ParentHash = ebl.BlockHash

	// store
	// p.log.Debugf("%d tx in block %d", len(b.Transactions), b.BlockNumber)
	if err := e.db.InsertBlock(ctx, ebl); err != nil {
		//e.log.Warnf("error inserting block: %s (ignoring)", err.Error()) // TODO: handle
		return err
	}

	return nil
}

func convertToDomain(b blocks.Block) domain.Block {
	ebl := domain.Block{
		BlockNumber:  b.Number,
		BlockHash:    b.Hash,
		ParentHash:   b.ParentHash,
		Timestamp:    b.Timestamp,
		Transactions: make([]domain.Transaction, 0),
	}

	for _, tx := range b.Transactions {
		// TODO: filter on namespace?

		// retrieve the Ethereum transaction from the chaincode invocation
		if len(tx.InputArgs) < 2 {
			continue // TBD
		}
		status := uint8(0)
		if tx.Valid {
			status = 1
		}

		etx, err := convertTransaction(tx.InputArgs[1], b.Hash, b.Number, tx.Number, tx.ID, status, tx.Status, tx.Events)
		if err != nil {
			continue // ?
		}

		ebl.Transactions = append(ebl.Transactions, etx)
	}

	return ebl
}

// convertTransaction converts an Ethereum transaction to a domain.Transaction.
func convertTransaction(ethTxBytes []byte, blockHash []byte, blockNumber uint64, txIndex int64, txID string, ethStatus uint8, validationCode int, events []byte) (domain.Transaction, error) {
	ethTx := &types.Transaction{}
	if err := ethTx.UnmarshalBinary(ethTxBytes); err != nil {
		return domain.Transaction{}, fmt.Errorf("invalid tx: %w", err)
	}

	signer := types.MakeSigner(common.ChainConfig, new(big.Int).SetUint64(blockNumber), 0)
	from, err := types.Sender(signer, ethTx)
	if err != nil {
		return domain.Transaction{}, fmt.Errorf("invalid sender: %w", err)
	}
	var to []byte
	var contractAddr []byte
	if ethTx.To() != nil {
		to = ethTx.To().Bytes()
	} else {
		contractAddr = crypto.CreateAddress(from, ethTx.Nonce()).Bytes()
	}

	hash := ethTx.Hash().Bytes()

	var logs []domain.Log
	if len(events) > 0 {
		rawLogs, err := common.UnmarshalLogs(events)
		if err != nil {
			// ?
			//return fmt.Errorf("parse logs: %w", err)
		}

		// Convert common.Log to domain.Log with full context
		logs = []domain.Log{}
		for i, l := range rawLogs {
			logs = append(logs, domain.Log{
				BlockNumber: blockNumber,
				TxHash:      hash,
				TxIndex:     txIndex,
				LogIndex:    int64(i),
				Address:     l.Address,
				Topics:      l.Topics,
				Data:        l.Data,
			})
		}
	}

	return domain.Transaction{
		TxHash:          hash,
		BlockHash:       blockHash,
		BlockNumber:     blockNumber,
		TxIndex:         txIndex,
		RawTx:           ethTxBytes,
		FromAddress:     from.Bytes(),
		ToAddress:       to,
		ContractAddress: contractAddr,
		Status:          ethStatus,
		FabricTxID:      txID,
		FabricTxStatus:  validationCode,
	}, nil
}
