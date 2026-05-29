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
// The expected TSV format is:
// block_id	transaction_hash	time	token_address	sender	recipient	value	token_name	token_symbol	token_decimals
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

	// Validate header
	expectedHeader := []string{
		"block_id", "transaction_hash", "time", "token_address",
		"sender", "recipient", "value", "token_name", "token_symbol", "token_decimals",
	}
	if len(header) != len(expectedHeader) {
		return nil, fmt.Errorf("invalid header: expected %d columns, got %d", len(expectedHeader), len(header))
	}

	var transfers []TokenTransfer
	lineNum := 1 // Start at 1 since we already read the header

	// Read data lines
	for {
		lineNum++
		record, err := tsvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading line %d: %w", lineNum, err)
		}

		if len(record) != 10 {
			return nil, fmt.Errorf("line %d: expected 10 columns, got %d", lineNum, len(record))
		}

		transfer, err := parseRecord(record, lineNum)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}

		transfers = append(transfers, transfer)
	}

	return transfers, nil
}

// parseRecord parses a single TSV record into a TokenTransfer struct
func parseRecord(record []string, lineNum int) (TokenTransfer, error) {
	var transfer TokenTransfer

	// Parse block_id (uint64)
	blockID, err := strconv.ParseUint(strings.TrimSpace(record[0]), 10, 64)
	if err != nil {
		return transfer, fmt.Errorf("invalid block_id: %w", err)
	}
	transfer.BlockID = blockID

	// Parse transaction_hash (common.Hash)
	txHashStr := strings.TrimSpace(record[1])
	if !strings.HasPrefix(txHashStr, "0x") {
		txHashStr = "0x" + txHashStr
	}
	if !common.IsHexAddress(txHashStr) && len(txHashStr) == 66 { // 0x + 64 hex chars
		transfer.TransactionHash = common.HexToHash(txHashStr)
	} else {
		return transfer, fmt.Errorf("invalid transaction hash: %s", record[1])
	}

	// Parse time (time.Time)
	timeStr := strings.TrimSpace(record[2])
	parsedTime, err := time.Parse("2006-01-02 15:04:05", timeStr)
	if err != nil {
		return transfer, fmt.Errorf("invalid time format: %w", err)
	}
	transfer.Time = parsedTime

	// Parse token_address (common.Address)
	tokenAddrStr := strings.TrimSpace(record[3])
	if !strings.HasPrefix(tokenAddrStr, "0x") {
		tokenAddrStr = "0x" + tokenAddrStr
	}
	if !common.IsHexAddress(tokenAddrStr) {
		return transfer, fmt.Errorf("invalid token address: %s", record[3])
	}
	transfer.TokenAddress = common.HexToAddress(tokenAddrStr)

	// Parse sender (common.Address)
	senderStr := strings.TrimSpace(record[4])
	if !strings.HasPrefix(senderStr, "0x") {
		senderStr = "0x" + senderStr
	}
	if !common.IsHexAddress(senderStr) {
		return transfer, fmt.Errorf("invalid sender address: %s", record[4])
	}
	transfer.Sender = common.HexToAddress(senderStr)

	// Parse recipient (common.Address)
	recipientStr := strings.TrimSpace(record[5])
	if !strings.HasPrefix(recipientStr, "0x") {
		recipientStr = "0x" + recipientStr
	}
	if !common.IsHexAddress(recipientStr) {
		return transfer, fmt.Errorf("invalid recipient address: %s", record[5])
	}
	transfer.Recipient = common.HexToAddress(recipientStr)

	// Parse value (uint256.Int for large ERC-20 values)
	valueStr := strings.TrimSpace(record[6])
	valueBig := new(big.Int)
	valueBig, ok := valueBig.SetString(valueStr, 10)
	if !ok {
		return transfer, fmt.Errorf("invalid value: %s", record[6])
	}
	value, overflow := uint256.FromBig(valueBig)
	if overflow {
		return transfer, fmt.Errorf("value overflow: %s", record[6])
	}
	transfer.Value = value

	// Parse token_name (string)
	transfer.TokenName = strings.TrimSpace(record[7])

	// Parse token_symbol (string)
	transfer.TokenSymbol = strings.TrimSpace(record[8])

	// Parse token_decimals (int)
	decimals, err := strconv.Atoi(strings.TrimSpace(record[9]))
	if err != nil {
		return transfer, fmt.Errorf("invalid token_decimals: %w", err)
	}
	transfer.TokenDecimals = decimals

	return transfer, nil
}
