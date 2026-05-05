/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

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
func NewChain(dbConnStr, triePath string, withTrie bool) (*Chain, error) {
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

	var ts *trie.Store
	if withTrie {
		ts, err = trie.New(triePath, initialRoot)
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("open trie store: %w", err)
		}
	}

	return &Chain{Store: blockStore, db: db, ts: ts, prevHash: prevHash}, nil
}

// Handle implements blocks.BlockHandler. It commits the block's write sets to the trie,
// then persists the block and its transactions to the database.
func (c *Chain) Handle(ctx context.Context, b blocks.Block) error {
	ebl := c.convertToDomain(b)

	if c.ts != nil {
		stateRoot, err := c.ts.Commit(ctx, b)
		if err != nil {
			return err // irrecoverable
		}
		ebl.StateRoot = stateRoot.Bytes()
		ebl.ParentHash = c.prevHash.Bytes()
	} else {
		ebl.StateRoot = types.EmptyRootHash[:]
		ebl.ParentHash = types.EmptyRootHash[:]
	}
	c.prevHash = common.BytesToHash(ebl.BlockHash)

	if err := c.Store.InsertBlock(ctx, ebl); err != nil {
		return err
	}

	return nil
}

// Close releases the trie and database resources.
func (c *Chain) Close() error {
	if c.ts != nil {
		c.ts.Close()
	}
	return c.db.Close()
}

// convertToDomain maps a Fabric SDK block to the gateway domain model,
// extracting and decoding the embedded Ethereum transactions.
func (c *Chain) convertToDomain(b blocks.Block) domain.Block {
	ebl := domain.Block{
		BlockNumber:  b.Number,
		BlockHash:    b.Hash,
		ParentHash:   b.ParentHash,
		Timestamp:    b.Timestamp,
		Transactions: make([]domain.Transaction, 0),
	}

	logIndex := int64(0) // logIndex is the index of the log in the block
	for _, tx := range b.Transactions {
		// TODO: filter on namespace?

		// retrieve the Ethereum transaction from the chaincode invocation
		if len(tx.InputArgs) < 2 {
			continue
		}
		status := uint8(0)
		if tx.Valid {
			status = 1
		}

		etx, err := convertTransaction(tx.InputArgs[1], b.Hash, b.Number, tx.Number, tx.ID, status, tx.Status, tx.Events, &logIndex)
		if err != nil {
			continue // ?
		}

		ebl.Transactions = append(ebl.Transactions, etx)
	}

	return ebl
}

// convertTransaction converts an Ethereum transaction to a domain.Transaction.
func convertTransaction(ethTxBytes []byte, blockHash []byte, blockNumber uint64, txIndex int64, txID string, ethStatus uint8, validationCode int, events []byte, logIndex *int64) (domain.Transaction, error) {
	ethTx := &types.Transaction{}
	if err := ethTx.UnmarshalBinary(ethTxBytes); err != nil {
		return domain.Transaction{}, fmt.Errorf("invalid tx: %w", err)
	}

	var signer types.Signer
	if id := ethTx.ChainId(); id.Sign() > 0 {
		signer = types.LatestSignerForChainID(id)
	} else {
		signer = types.HomesteadSigner{}
	}
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
		for _, l := range rawLogs {
			logs = append(logs, domain.Log{
				BlockNumber: blockNumber,
				BlockHash:   blockHash,
				TxHash:      hash,
				TxIndex:     txIndex,
				LogIndex:    *logIndex,
				Address:     l.Address,
				Topics:      l.Topics,
				Data:        l.Data,
			})
			*logIndex++
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
		Logs:            logs,
	}, nil
}
