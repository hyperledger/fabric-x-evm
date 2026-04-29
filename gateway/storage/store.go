/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"strings"

	"github.com/hyperledger/fabric-x-evm/gateway/domain"
)

//go:embed schema.sql
var ddl string

type Store struct {
	queries *Queries
	db      *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{
		queries: New(db),
		db:      db,
	}
}

// Conversion functions between storage and domain types

func toStorageBlock(b domain.Block) InsertBlockParams {
	return InsertBlockParams{
		BlockNumber: int64(b.BlockNumber),
		BlockHash:   b.BlockHash,
		ParentHash:  b.ParentHash,
		StateRoot:   b.StateRoot,
		Timestamp:   b.Timestamp,
	}
}

func toDomainBlock(b Block) domain.Block {
	return domain.Block{
		BlockNumber: uint64(b.BlockNumber),
		BlockHash:   b.BlockHash,
		ParentHash:  b.ParentHash,
		StateRoot:   b.StateRoot,
		Timestamp:   b.Timestamp,
	}
}

func toStorageTransaction(t domain.Transaction) (InsertTransactionParams, []InsertLogParams) {
	txp := InsertTransactionParams{
		TxHash:          t.TxHash,
		BlockHash:       t.BlockHash,
		BlockNumber:     int64(t.BlockNumber),
		TxIndex:         t.TxIndex,
		RawTx:           t.RawTx,
		FromAddress:     t.FromAddress,
		ToAddress:       t.ToAddress,
		ContractAddress: t.ContractAddress,
		Status:          int64(t.Status),
		FabricTxID:      t.FabricTxID,
		FabricTxStatus:  int64(t.FabricTxStatus),
	}
	lp := make([]InsertLogParams, len(t.Logs))
	for i, l := range t.Logs {
		lp[i] = toStorageLog(l)
	}
	return txp, lp
}

func toDomainTransaction(t Transaction) domain.Transaction {
	return domain.Transaction{
		TxHash:          t.TxHash,
		BlockHash:       t.BlockHash,
		BlockNumber:     uint64(t.BlockNumber),
		TxIndex:         t.TxIndex,
		RawTx:           t.RawTx,
		FromAddress:     t.FromAddress,
		ToAddress:       t.ToAddress,
		ContractAddress: t.ContractAddress,
		FabricTxID:      t.FabricTxID,
		FabricTxStatus:  int(t.FabricTxStatus),
		Status:          uint8(t.Status),
	}
}

// Init creates the tables.
func (s *Store) Init() error {
	if _, err := s.db.ExecContext(context.TODO(), ddl); err != nil {
		return err
	}
	return nil
}

func (s *Store) InsertBlock(ctx context.Context, block domain.Block) error {
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer sqlTx.Rollback() //nolint:errcheck

	qtx := s.queries.WithTx(sqlTx)

	if err := qtx.InsertBlock(ctx, toStorageBlock(block)); err != nil {
		return err
	}

	// Insert transactions
	for i := range block.Transactions {
		tx, logs := toStorageTransaction(block.Transactions[i])
		if err := qtx.InsertTransaction(ctx, tx); err != nil {
			return err
		}
		for _, l := range logs {
			if err := qtx.InsertLog(ctx, l); err != nil {
				return err
			}
		}
	}

	if err := sqlTx.Commit(); err != nil {
		return err
	}
	return nil
}

func toStorageLog(l domain.Log) InsertLogParams {
	data := l.Data
	if data == nil {
		data = []byte{}
	}
	params := InsertLogParams{
		BlockNumber: int64(l.BlockNumber),
		BlockHash:   l.BlockHash,
		TxHash:      l.TxHash,
		TxIndex:     l.TxIndex,
		LogIndex:    l.LogIndex,
		Address:     l.Address,
		Data:        data,
	}
	if len(l.Topics) > 0 {
		params.Topic0 = l.Topics[0]
	}
	if len(l.Topics) > 1 {
		params.Topic1 = l.Topics[1]
	}
	if len(l.Topics) > 2 {
		params.Topic2 = l.Topics[2]
	}
	if len(l.Topics) > 3 {
		params.Topic3 = l.Topics[3]
	}
	return params
}

// BlockNumber returns the number of the last committed block. If there are no rows, the blockheight is zero.
func (s *Store) BlockNumber(ctx context.Context) (uint64, error) {
	num, err := s.queries.BlockNumber(ctx)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return uint64(num), err
}

// BlockNumberByHash resolves a block hash to its block number.
func (s *Store) BlockNumberByHash(ctx context.Context, blockHash []byte) (*uint64, error) {
	num, err := s.queries.BlockNumberByHash(ctx, blockHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	res := uint64(num)
	return &res, nil
}

// GetBlockByNumber retrieves a block by its number.
// Always loads transactions so the API layer can return either hashes or full objects.
func (s *Store) GetBlockByNumber(ctx context.Context, blockNumber uint64, full bool) (*domain.Block, error) {
	b, err := s.queries.GetBlockByNumber(ctx, int64(blockNumber))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	block := toDomainBlock(b)

	// Always load transactions - the API layer decides whether to return hashes or full objects
	txs, err := s.queries.GetTransactionsByBlockNumber(ctx, int64(blockNumber))
	if err != nil {
		return nil, err
	}
	block.Transactions = make([]domain.Transaction, len(txs))
	for i, tx := range txs {
		block.Transactions[i] = toDomainTransaction(tx)
	}

	return &block, nil
}

// GetBlockByHash retrieves a block by its hash.
// Always loads transactions so the API layer can return either hashes or full objects.
func (s *Store) GetBlockByHash(ctx context.Context, blockHash []byte, full bool) (*domain.Block, error) {
	b, err := s.queries.GetBlockByHash(ctx, blockHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	block := toDomainBlock(b)

	// Always load transactions - the API layer decides whether to return hashes or full objects
	txs, err := s.queries.GetTransactionsByBlockHash(ctx, blockHash)
	if err != nil {
		return nil, err
	}
	block.Transactions = make([]domain.Transaction, len(txs))
	for i, tx := range txs {
		block.Transactions[i] = toDomainTransaction(tx)
	}

	return &block, nil
}

// GetBlockTxCountByHash counts transactions in a block by hash.
func (s *Store) GetBlockTxCountByHash(ctx context.Context, blockHash []byte) (int64, error) {
	return s.queries.GetBlockTxCountByHash(ctx, blockHash)
}

// GetBlockTxCountByNumber counts transactions in a block by number.
func (s *Store) GetBlockTxCountByNumber(ctx context.Context, blockNumber uint64) (int64, error) {
	return s.queries.GetBlockTxCountByNumber(ctx, int64(blockNumber))
}

// GetTransactionByHash retrieves a transaction by its hash.
func (s *Store) GetTransactionByHash(ctx context.Context, txHash []byte) (*domain.Transaction, error) {
	tx, err := s.queries.GetTransactionByHash(ctx, txHash)
	return singleResultToDomainTransaction(tx, err)
}

func singleResultToDomainTransaction(tx Transaction, sqlErr error) (*domain.Transaction, error) {
	if sqlErr != nil {
		if errors.Is(sqlErr, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, sqlErr
	}
	dtx := toDomainTransaction(tx)
	return &dtx, nil
}

// GetTransactionByBlockHashAndIndex retrieves a transaction by block hash and index.
func (s *Store) GetTransactionByBlockHashAndIndex(ctx context.Context, blockHash []byte, txIndex int64) (*domain.Transaction, error) {
	tx, err := s.queries.GetTransactionByBlockHashAndIndex(ctx, GetTransactionByBlockHashAndIndexParams{
		BlockHash: blockHash,
		TxIndex:   txIndex,
	})
	return singleResultToDomainTransaction(tx, err)
}

// GetTransactionByBlockNumberAndIndex retrieves a transaction by block number and index.
func (s *Store) GetTransactionByBlockNumberAndIndex(ctx context.Context, blockNumber uint64, txIndex int64) (*domain.Transaction, error) {
	tx, err := s.queries.GetTransactionByBlockNumberAndIndex(ctx, GetTransactionByBlockNumberAndIndexParams{
		BlockNumber: int64(blockNumber),
		TxIndex:     txIndex,
	})
	return singleResultToDomainTransaction(tx, err)
}

// LatestBlock retrieves the latest block.
// Always loads transactions so the API layer can return either hashes or full objects.
func (s *Store) LatestBlock(ctx context.Context, full bool) (*domain.Block, error) {
	b, err := s.queries.LatestBlock(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	bl := toDomainBlock(b)

	// Always load transactions - the API layer decides whether to return hashes or full objects
	txs, err := s.queries.GetTransactionsByBlockNumber(ctx, b.BlockNumber)
	if err != nil {
		return nil, err
	}
	bl.Transactions = make([]domain.Transaction, len(txs))
	for i, tx := range txs {
		bl.Transactions[i] = toDomainTransaction(tx)
	}

	return &bl, nil
}

func toDomainLog(l Log) domain.Log {
	log := domain.Log{
		BlockNumber: uint64(l.BlockNumber),
		BlockHash:   l.BlockHash,
		TxHash:      l.TxHash,
		TxIndex:     l.TxIndex,
		LogIndex:    l.LogIndex,
		Address:     l.Address,
		Data:        l.Data,
	}
	// Collect non-nil topics
	for _, t := range [][]byte{l.Topic0, l.Topic1, l.Topic2, l.Topic3} {
		if t != nil {
			log.Topics = append(log.Topics, t)
		} else {
			break // topics are contiguous
		}
	}
	return log
}

// GetLogs retrieves logs matching the filter criteria.
// Supports filtering by block hash OR block range, and by addresses.
// See LogFilter docs for detailed explanation.
func (s *Store) GetLogs(ctx context.Context, filter domain.LogFilter) ([]domain.Log, error) {
	var query strings.Builder
	var args []any

	query.WriteString(`SELECT block_number, block_hash, tx_hash, tx_index, log_index, address, topic0, topic1, topic2, topic3, data FROM logs WHERE 1=1`)

	// Block filtering: either by hash or by range (mutually exclusive)
	if filter.BlockHash != nil {
		query.WriteString(` AND block_number = (SELECT block_number FROM blocks WHERE block_hash = ?)`)
		args = append(args, *filter.BlockHash)
	} else {
		if filter.FromBlock != nil {
			query.WriteString(` AND block_number >= ?`)
			args = append(args, *filter.FromBlock)
		}
		if filter.ToBlock != nil {
			query.WriteString(` AND block_number <= ?`)
			args = append(args, *filter.ToBlock)
		}
	}

	// Address filtering (OR logic)
	if len(filter.Addresses) > 0 {
		query.WriteString(` AND address IN (`)
		for i, addr := range filter.Addresses {
			if i > 0 {
				query.WriteString(`, `)
			}
			query.WriteString(`?`)
			args = append(args, addr)
		}
		query.WriteString(`)`)
	}

	// Topic filtering: each position is AND'd, within a position it's OR
	// Topics[0] filters topic0, Topics[1] filters topic1, etc.
	topicColumns := []string{"topic0", "topic1", "topic2", "topic3"}
	for i, alternatives := range filter.Topics {
		if i >= len(topicColumns) {
			break // only 4 topics supported
		}
		if len(alternatives) == 0 {
			continue // empty slice matches any topic at this position
		}
		if len(alternatives) == 1 {
			query.WriteString(` AND `)
			query.WriteString(topicColumns[i])
			query.WriteString(` = ?`)
			args = append(args, alternatives[0])
		} else {
			query.WriteString(` AND `)
			query.WriteString(topicColumns[i])
			query.WriteString(` IN (`)
			for j, topic := range alternatives {
				if j > 0 {
					query.WriteString(`, `)
				}
				query.WriteString(`?`)
				args = append(args, topic)
			}
			query.WriteString(`)`)
		}
	}

	query.WriteString(` ORDER BY block_number, tx_index, log_index`)

	rows, err := s.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []domain.Log
	for rows.Next() {
		var l Log
		if err := rows.Scan(
			&l.BlockNumber,
			&l.BlockHash,
			&l.TxHash,
			&l.TxIndex,
			&l.LogIndex,
			&l.Address,
			&l.Topic0,
			&l.Topic1,
			&l.Topic2,
			&l.Topic3,
			&l.Data,
		); err != nil {
			return nil, err
		}
		logs = append(logs, toDomainLog(l))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return logs, nil
}

// GetLogsByTxHash retrieves all logs for a specific transaction.
func (s *Store) GetLogsByTxHash(ctx context.Context, txHash []byte) ([]domain.Log, error) {
	rows, err := s.queries.GetLogsByTxHash(ctx, txHash)
	if err != nil {
		return nil, err
	}
	logs := make([]domain.Log, len(rows))
	for i, row := range rows {
		logs[i] = toDomainLog(row)
	}
	return logs, nil
}
