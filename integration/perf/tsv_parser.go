/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import (
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// TokenTransfer represents a single ERC-20 token transfer event from the TSV file
type TokenTransfer struct {
	BlockID         uint64         // Ethereum block number
	TransactionHash common.Hash    // Transaction hash
	Time            time.Time      // Timestamp of the transfer
	TokenAddress    common.Address // ERC-20 token contract address
	Sender          common.Address // Sender address
	Recipient       common.Address // Recipient address
	Value           *uint256.Int   // Transfer value (supports large ERC-20 amounts)
	TokenName       string         // Token name (e.g., "USD//C")
	TokenSymbol     string         // Token symbol (e.g., "USDC")
	TokenDecimals   int            // Number of decimals for the token
	Transaction     []byte         // The transaction that implements this transfer
}

// ParseTSVGZ parses a gzipped TSV file containing token transfer data
// and returns a slice of TokenTransfer structs.
//
// The TSV file must have a header row with the following required columns
// (order doesn't matter):
// block_id, transaction_hash, time, token_address, sender, recipient, value, token_name, token_symbol, token_decimals
//
// Parameters:
//   - filename: path to the .tsv.gz file
//
// Returns:
//   - []TokenTransfer: slice of parsed token transfers
//   - error: any error encountered during parsing
func ParseTSVGZ(filename string) ([]TokenTransfer, error) {
	// Open the gzipped file
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Create gzip reader
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	// Create TSV reader
	tsvReader := csv.NewReader(gzReader)
	tsvReader.Comma = '\t'
	tsvReader.LazyQuotes = true
	tsvReader.TrimLeadingSpace = true

	// Read header line
	header, err := tsvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	// Build a map of column names to indices
	columnMap := make(map[string]int)
	for i, col := range header {
		columnMap[strings.TrimSpace(col)] = i
	}

	// Validate that all required columns are present
	requiredColumns := []string{
		"block_id", "transaction_hash", "time", "token_address",
		"sender", "recipient", "value", "token_name", "token_symbol", "token_decimals",
	}
	for _, col := range requiredColumns {
		if _, ok := columnMap[col]; !ok {
			return nil, fmt.Errorf("missing required column: %s", col)
		}
	}

	var transfers []TokenTransfer
	lineNum := 1 // Start at 1 since we already read the header
	skippedLines := 0

	// Read data lines
	for {
		lineNum++
		record, err := tsvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Warning: skipping line %d due to read error: %v", lineNum, err)
			skippedLines++
			continue
		}

		if len(record) != len(header) {
			log.Printf("Warning: skipping line %d: expected %d columns, got %d", lineNum, len(header), len(record))
			skippedLines++
			continue
		}

		transfer, err := parseRecord(record, columnMap, lineNum)
		if err != nil {
			log.Printf("Warning: skipping line %d due to parse error: %v", lineNum, err)
			skippedLines++
			continue
		}

		transfers = append(transfers, transfer)
	}

	if skippedLines > 0 {
		log.Printf("Skipped %d malformed lines out of %d total lines", skippedLines, lineNum-1)
	}

	return transfers, nil
}

// parseRecord parses a single TSV record into a TokenTransfer struct using the column map
func parseRecord(record []string, columnMap map[string]int, lineNum int) (TokenTransfer, error) {
	var transfer TokenTransfer

	// Parse block_id (uint64)
	blockID, err := strconv.ParseUint(strings.TrimSpace(record[columnMap["block_id"]]), 10, 64)
	if err != nil {
		return transfer, fmt.Errorf("invalid block_id: %w", err)
	}
	transfer.BlockID = blockID

	// Parse transaction_hash (common.Hash)
	txHashStr := strings.TrimSpace(record[columnMap["transaction_hash"]])
	if !strings.HasPrefix(txHashStr, "0x") {
		txHashStr = "0x" + txHashStr
	}
	if !common.IsHexAddress(txHashStr) && len(txHashStr) == 66 { // 0x + 64 hex chars
		transfer.TransactionHash = common.HexToHash(txHashStr)
	} else {
		return transfer, fmt.Errorf("invalid transaction hash: %s", record[columnMap["transaction_hash"]])
	}

	// Parse time (time.Time)
	timeStr := strings.TrimSpace(record[columnMap["time"]])
	parsedTime, err := time.Parse("2006-01-02 15:04:05", timeStr)
	if err != nil {
		return transfer, fmt.Errorf("invalid time format: %w", err)
	}
	transfer.Time = parsedTime

	// Parse token_address (common.Address)
	tokenAddrStr := strings.TrimSpace(record[columnMap["token_address"]])
	if !strings.HasPrefix(tokenAddrStr, "0x") {
		tokenAddrStr = "0x" + tokenAddrStr
	}
	if !common.IsHexAddress(tokenAddrStr) {
		return transfer, fmt.Errorf("invalid token address: %s", record[columnMap["token_address"]])
	}
	transfer.TokenAddress = common.HexToAddress(tokenAddrStr)

	// Parse sender (common.Address)
	senderStr := strings.TrimSpace(record[columnMap["sender"]])
	if !strings.HasPrefix(senderStr, "0x") {
		senderStr = "0x" + senderStr
	}
	if !common.IsHexAddress(senderStr) {
		return transfer, fmt.Errorf("invalid sender address: %s", record[columnMap["sender"]])
	}
	transfer.Sender = common.HexToAddress(senderStr)

	// Parse recipient (common.Address)
	recipientStr := strings.TrimSpace(record[columnMap["recipient"]])
	if !strings.HasPrefix(recipientStr, "0x") {
		recipientStr = "0x" + recipientStr
	}
	if !common.IsHexAddress(recipientStr) {
		return transfer, fmt.Errorf("invalid recipient address: %s", record[columnMap["recipient"]])
	}
	transfer.Recipient = common.HexToAddress(recipientStr)

	// Parse value (uint256.Int for large ERC-20 values)
	valueStr := strings.TrimSpace(record[columnMap["value"]])
	valueBig := new(big.Int)
	valueBig, ok := valueBig.SetString(valueStr, 10)
	if !ok {
		return transfer, fmt.Errorf("invalid value: %s", record[columnMap["value"]])
	}
	value, overflow := uint256.FromBig(valueBig)
	if overflow {
		return transfer, fmt.Errorf("value overflow: %s", record[columnMap["value"]])
	}
	transfer.Value = value

	// Parse token_name (string)
	transfer.TokenName = strings.TrimSpace(record[columnMap["token_name"]])

	// Parse token_symbol (string)
	transfer.TokenSymbol = strings.TrimSpace(record[columnMap["token_symbol"]])

	// Parse token_decimals (int)
	decimals, err := strconv.Atoi(strings.TrimSpace(record[columnMap["token_decimals"]]))
	if err != nil {
		return transfer, fmt.Errorf("invalid token_decimals: %w", err)
	}
	transfer.TokenDecimals = decimals

	return transfer, nil
}
