/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"bytes"
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

	blockRetention uint64 // number of most recent blocks to keep; 0 keeps every block
	pruneInterval  uint64 // prune once every this many blocks
	lastPruned     uint64 // block number at the last prune, so a skipped multiple can't disable pruning
}

// DefaultBlockPruneInterval is how often, in blocks, a Chain with block retention
// enabled prunes old blocks when no interval is configured.
const DefaultBlockPruneInterval = 1000

// NewChain opens the SQLite database and trie store, seeds state from the latest committed
// block, and returns a ready Chain. dbConnStr uses the modernc SQLite DSN format;
// triePath is the directory for the PebbleDB trie (empty string = in-memory).
// The caller must register the SQLite driver (e.g. _ "modernc.org/sqlite") before calling.
func NewChain(dbConnStr, triePath string, withTrie bool) (*Chain, error) {
	db, err := sqlite.Open(dbConnStr)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// Keep SQLite temp files in memory. The release image is built FROM scratch
	// and has no temp directory, so file-backed temp storage fails with
	// SQLITE_IOERR_GETTEMPPATH on statements that need it, such as large DELETEs.
	// sqlite.Open uses a single long-lived connection, so this applies to all queries.
	if _, err := db.Exec("PRAGMA temp_store=MEMORY"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set temp_store pragma: %w", err)
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

// Handle implements blocks.BlockHandler. Order matters: the trie
// commits before InsertBlock, so a crash between them never leaves SQL ahead
// of the MPT, and prevHash only advances after InsertBlock succeeds, so a
// failed insert never publishes a tip the store doesn't hold.
//
// Re-delivering an already-stored block is safe: the trie apply and
// InsertBlock/InsertTransaction/InsertLog are all idempotent under replay.
func (c *Chain) Handle(ctx context.Context, b blocks.Block) error {
	ebl := ConvertToDomain(b)

	ebl.ParentHash = c.prevHash.Bytes()
	if c.ts != nil {
		stateRoot, err := c.ts.Commit(ctx, b)
		if err != nil {
			return err // irrecoverable
		}
		ebl.StateRoot = stateRoot.Bytes()
	} else {
		ebl.StateRoot = types.EmptyRootHash[:]
	}

	if err := c.Store.InsertBlock(ctx, ebl); err != nil {
		return err
	}

	c.prevHash = common.BytesToHash(ebl.BlockHash)

	// Prune once the tip has advanced a full interval past the last prune. This is a
	// threshold rather than ebl.BlockNumber%pruneInterval == 0 because the numbers
	// reaching Handle are not contiguous: the notification dispatcher skips blocks
	// carrying no EVM transactions, so an exact multiple can be stepped over and
	// pruning would then never run at all.
	if c.blockRetention > 0 && c.pruneInterval > 0 &&
		ebl.BlockNumber > c.blockRetention &&
		ebl.BlockNumber >= c.lastPruned && // a re-delivered older block must not underflow below
		ebl.BlockNumber-c.lastPruned >= c.pruneInterval {
		upTo := ebl.BlockNumber - c.blockRetention
		// Pruning is housekeeping: a failure must not stop block ingestion. Leaving
		// lastPruned alone makes the next interval retry with a wider range.
		if err := c.Store.TruncateBlocks(ctx, upTo); err != nil {
			logger.Warnf("pruning blocks before %d failed: %v", upTo+1, err)
		} else {
			c.lastPruned = ebl.BlockNumber
		}
	}
	return nil
}

// SetBlockRetention makes the chain keep a window of the most recent keep block
// numbers, pruning older blocks with their transactions and logs once the tip has
// advanced interval blocks past the previous prune. keep == 0 keeps every block;
// interval == 0 uses DefaultBlockPruneInterval.
//
// The window is a span of block numbers, not a count of stored rows: only blocks
// carrying EVM transactions are stored, so a chain with other traffic keeps fewer
// than keep blocks. Pruned blocks are gone from the JSON-RPC API — queries for them
// return nothing rather than an error, so a client asking for a range that starts
// below the window gets a truncated answer. Only the SQLite store is pruned; the
// state trie, when enabled, is untouched.
//
// It must be called before the chain starts handling blocks.
func (c *Chain) SetBlockRetention(keep, interval uint64) {
	if interval == 0 {
		interval = DefaultBlockPruneInterval
	}
	c.blockRetention = keep
	c.pruneInterval = interval
}

// EnsureGenesisBlock inserts an empty block 0 if the store has no blocks yet, so
// eth_getBlockByNumber("latest") is never null before the first transaction commits.
// Real Fabric/fabric-x channels always deliver a genesis block already; this is only
// for backends (like fabrictest) that don't.
func (c *Chain) EnsureGenesisBlock(ctx context.Context) error {
	latest, err := c.Store.LatestBlock(ctx, false)
	if err != nil {
		return err
	}
	if latest != nil {
		return nil
	}

	genesisHash := crypto.Keccak256([]byte("fxevm-testnode-genesis"))
	if err := c.Store.InsertBlock(ctx, domain.Block{
		BlockNumber: 0,
		BlockHash:   genesisHash,
		ParentHash:  make([]byte, common.HashLength),
		StateRoot:   types.EmptyRootHash[:],
	}); err != nil {
		return err
	}
	c.prevHash = common.BytesToHash(genesisHash)
	return nil
}

// Close releases the trie and database resources.
func (c *Chain) Close() error {
	if c.ts != nil {
		c.ts.Close()
	}
	return c.db.Close()
}

// ConvertToDomain maps a Fabric SDK block to the gateway domain model,
// extracting and decoding the embedded Ethereum transactions.
// This is a standalone function so it can be reused by other components like Gateway.
func ConvertToDomain(b blocks.Block) domain.Block {
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
		if len(tx.InputArgs) < 2 || !bytes.Equal(tx.InputArgs[0], []byte{byte(fc.ProposalTypeEVMTx)}) {
			// skip non-eth tx
			continue
		}
		status := uint8(0)
		if tx.Valid && !fc.IsRevertEvent(tx.Events) && !fc.IsExecFailureEvent(tx.Events) {
			status = 1
		}

		etx, err := convertTransaction(tx.InputArgs[1], b.Hash, b.Number, tx.Number, tx.ID, status, tx.Status, tx.Valid, tx.Events, &logIndex)
		if err != nil {
			panic(err) // we surface this for now instead of swallowing it
		}

		ebl.Transactions = append(ebl.Transactions, etx)
	}

	return ebl
}

// convertTransaction converts an Ethereum transaction to a domain.Transaction.
func convertTransaction(ethTxBytes []byte, blockHash []byte, blockNumber uint64, txIndex int64, txID string, ethStatus uint8, validationCode int, fabricValid bool, events []byte, logIndex *int64) (domain.Transaction, error) {
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
	// transactions with status 0 (revert, execution failure) never emit logs.
	if ethStatus == 1 && len(events) > 0 {
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
		FabricValid:     fabricValid,
		Logs:            logs,
	}, nil
}
