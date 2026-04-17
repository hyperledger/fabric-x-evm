/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"

	fc "github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/gateway/domain"
	"github.com/hyperledger/fabric-x-evm/gateway/storage"
	"github.com/hyperledger/fabric-x-evm/gateway/storage/trie"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/state/sqlite"
)

// Chain owns the block storage and state trie. It implements blocks.BlockHandler
// (for block ingestion) and core.Store (via the embedded *storage.Store, for API queries).
type Chain struct {
	*storage.Store
	db       *sql.DB
	ts       *trie.Store
	prevHash common.Hash // Ethereum hash of last committed block; seeded from DB on startup
}

// NewChain opens the SQLite database and trie store, seeds state from the latest committed
// block, and returns a ready Chain. dbConnStr uses the modernc SQLite DSN format;
// triePath is the directory for the PebbleDB trie (empty string = in-memory).
// The caller must register the SQLite driver (e.g. _ "modernc.org/sqlite") before calling.
func NewChain(dbConnStr, triePath string) (*Chain, error) {
	db, err := sqlite.Open(dbConnStr)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	blockStore := storage.NewStore(db)
	if err := blockStore.Init(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init block store: %w", err)
	}

	// Seed trie root and parent hash from the latest committed block so state
	// resumes correctly after a restart.
	var initialRoot, prevHash common.Hash
	if latest, err := blockStore.LatestBlock(context.Background(), false); err == nil && latest != nil {
		initialRoot = common.BytesToHash(latest.StateRoot)
		prevHash = common.BytesToHash(latest.BlockHash)
	}

	ts, err := trie.New(triePath, initialRoot)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open trie store: %w", err)
	}

	return &Chain{Store: blockStore, db: db, ts: ts, prevHash: prevHash}, nil
}

// Handle implements blocks.BlockHandler. It commits the block's write sets to the trie,
// then persists the block and its transactions to the database.
func (c *Chain) Handle(ctx context.Context, b blocks.Block) error {
	ebl := convertToDomain(b)

	stateRoot, err := c.ts.Commit(ctx, b)
	if err != nil {
		return err // irrecoverable
	}
	ebl.StateRoot = stateRoot.Bytes()
	// TODO: use proper headers. We now add some mock values to make sure we're not violating unique constraints.
	ebl.ParentHash = ebl.BlockHash

	if err := c.Store.InsertBlock(ctx, ebl); err != nil {
		return err
	}

	return nil
}

// Close releases the trie and database resources.
func (c *Chain) Close() error {
	c.ts.Close()
	return c.db.Close()
}

// convertToDomain maps a Fabric SDK block to the gateway domain model,
// extracting and decoding the embedded Ethereum transactions.
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

	var config *params.ChainConfig
	if ethTx.ChainId().Int64() != 0 {
		config = fc.BuildChainConfig(ethTx.ChainId().Int64())
	} else {
		config = fc.ChainConfig
	}

	signer := types.MakeSigner(config, new(big.Int).SetUint64(blockNumber), 0)
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
		rawLogs, err := fc.UnmarshalLogs(events)
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
