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
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/state/sqlite"
)

// Chain owns the block storage. It implements blocks.BlockHandler (for block
// ingestion) and core.Store (via the embedded *storage.Store, for API queries).
//
// It computes no state root: every block reports the empty-trie root (see
// docs/COMPATIBILITY.md). gateway/storage/trie stays for the conformance tests,
// which can replay read-write sets into it (-verify_root).
type Chain struct {
	*storage.Store
	db       *sql.DB
	prevHash common.Hash // Ethereum hash of last committed block; seeded from DB on startup
}

// NewChain opens the SQLite database, seeds the parent hash from the latest committed
// block, and returns a ready Chain. dbConnStr uses the modernc SQLite DSN format.
// The caller must register the SQLite driver (e.g. _ "modernc.org/sqlite") before calling.
func NewChain(dbConnStr string) (*Chain, error) {
	db, err := sqlite.Open(dbConnStr)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	blockStore := storage.NewStore(db)
	if err := blockStore.Init(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init block store: %w", err)
	}

	// Seed the parent hash from the latest committed block so the chain resumes
	// correctly after a restart.
	var prevHash common.Hash
	if latest, err := blockStore.LatestBlock(context.Background(), false); err == nil && latest != nil {
		prevHash = common.BytesToHash(latest.BlockHash)
	}

	return &Chain{Store: blockStore, db: db, prevHash: prevHash}, nil
}

// Handle implements blocks.BlockHandler. prevHash only advances after InsertBlock
// succeeds, so a failed insert never publishes a tip the store doesn't hold.
//
// Re-delivering an already-stored block is safe: InsertBlock/InsertTransaction/
// InsertLog are all idempotent under replay.
func (c *Chain) Handle(ctx context.Context, b blocks.Block) error {
	logger.Debugf("Chain.Handle() block=%d txs=%d", b.Number, len(b.Transactions))
	ebl, err := ConvertToDomain(b)
	if err != nil {
		// Unrecoverable: a committed tx we cannot parse. Log, debug-panic, then
		// return so release builds stay up while we decide a production policy.
		msg := fmt.Sprintf("Chain.Handle() block=%d convert error: %v", b.Number, err)
		logger.Error(msg)
		fc.DebugPanic(msg)
		return err
	}

	// An MVCC-rejected transaction wrote nothing, did not consume its nonce, and
	// is about to be executed again, so storing a row would publish a status=0
	// receipt for a transaction still in flight -- and the client would resubmit
	// at a nonce that is still ours. Unstored, the hash keeps answering "pending"
	// while the queue tracks it across retries.
	//
	// Only the row is dropped: later handlers still get the whole block, so
	// DepGraphQueue sees the conflict and retries it.
	ebl.Transactions = withoutRetriableConflicts(ebl.Transactions)

	ebl.ParentHash = c.prevHash.Bytes()
	ebl.StateRoot = types.EmptyRootHash[:]

	if err := c.Store.InsertBlock(ctx, ebl); err != nil {
		logger.Warnf("Chain.Handle() block=%d insert error: %v", b.Number, err)
		return err
	}

	c.prevHash = common.BytesToHash(ebl.BlockHash)
	logger.Debugf("Chain.Handle() block=%d committed prevHash=%s", b.Number, c.prevHash.Hex())
	return nil
}

// withoutRetriableConflicts drops the transactions that will be retried, so no
// receipt is published for an attempt that is about to be superseded.
//
// Only MVCC conflicts qualify: every other failure is final and the client is
// entitled to its receipt. A conflict past the retry budget is final too, but
// DepGraphQueue owns the budget so this cannot tell, and drops that row as well.
// That one reads as unknown rather than pending: untrackOrRetry untracks it
// while this same block is handled, and the nonce gate never held it (anything
// the queue tracks was enqueued at the expected nonce, not parked). No row, no
// entry, nothing parked — TransactionByHash finds nothing and the hash answers
// null.
//
// The gate's reaper drops the sender's *later* transactions instead: the conflict
// did not consume its nonce, so they stay parked behind a gap until a
// resubmission at that nonce fills it, or defaultParkedTTL expires.
func withoutRetriableConflicts(txs []domain.Transaction) []domain.Transaction {
	kept := make([]domain.Transaction, 0, len(txs))
	for _, tx := range txs {
		if tx.FabricTxStatus == blocks.StatusMVCCConflict {
			logger.Warnf("Chain.Handle() tx %s rejected by an MVCC conflict: not storing a receipt, it will be retried",
				common.BytesToHash(tx.TxHash).Hex())
			continue
		}
		kept = append(kept, tx)
	}
	return kept
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

// Close releases the database.
func (c *Chain) Close() error {
	return c.db.Close()
}

// ConvertToDomain maps a Fabric SDK block to the gateway domain model,
// extracting and decoding the embedded Ethereum transactions.
// This is a standalone function so it can be reused by other components like Gateway.
//
// A convertTransaction failure after endorsement/order/commit is a data integrity
// problem that must not crash the process; callers get an error instead of a panic.
func ConvertToDomain(b blocks.Block) (domain.Block, error) {
	ebl := domain.Block{
		BlockNumber:  b.Number,
		BlockHash:    b.Hash,
		ParentHash:   b.ParentHash,
		Timestamp:    b.Timestamp,
		Transactions: make([]domain.Transaction, 0),
	}

	logIndex := int64(0) // logIndex is the index of the log in the block
	cumulativeGas := uint64(0)
	for _, tx := range b.Transactions {
		// retrieve the Ethereum transaction from the chaincode invocation
		ethTxBytes, ok := fc.EVMTx(tx)
		if !ok {
			// skip non-eth tx
			continue
		}
		succeeded, gasUsed := fc.ExecutionOutcome(tx)
		status := uint8(0)
		if succeeded {
			status = 1
		}
		cumulativeGas += gasUsed

		etx, err := convertTransaction(ethTxBytes, b.Hash, b.Number, tx.Number, tx.ID, status, tx.Status, tx.Event, &logIndex)
		if err != nil {
			return domain.Block{}, fmt.Errorf("convert tx %s in block %d: %w", tx.ID, b.Number, err)
		}
		etx.GasUsed = gasUsed
		etx.CumulativeGasUsed = cumulativeGas

		ebl.Transactions = append(ebl.Transactions, etx)
	}

	return ebl, nil
}

// convertTransaction converts an Ethereum transaction to a domain.Transaction.
//
// fabricStatus is the SDK's protocol-neutral commit status, not a ledger-specific
// validation code: it is one blocks.Status whichever path — delivery or notification —
// produced the block. It is the sole record of whether the commit was Fabric-valid,
// there being exactly one valid status.
func convertTransaction(ethTxBytes []byte, blockHash []byte, blockNumber uint64, txIndex int64, txID string, ethStatus uint8, fabricStatus blocks.Status, events []byte, logIndex *int64) (domain.Transaction, error) {
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
			return domain.Transaction{}, fmt.Errorf("parse logs: %w", err)
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
		FabricTxStatus:  fabricStatus,
		Logs:            logs,
	}, nil
}
