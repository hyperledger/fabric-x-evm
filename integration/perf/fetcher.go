/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package main

import (
	"compress/gzip"
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"path"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/hyperledger/fabric-x-evm/integration"
	"github.com/hyperledger/fabric-x-evm/integration/contracts"
)

// USDC proxy address
var usdcAddress = common.HexToAddress("0xA0b86991c6218b36c1d19d4a2e9eb0ce3606eb48")

// EIP-1967 implementation slot:
// bytes32(uint256(keccak256("eip1967.proxy.implementation")) - 1)
var implementationSlot = common.HexToHash(
	"0x7050c9e0f4ca769c69bd3a8ef740bc37934f8e2c036e5a723fd8ee048ed3f8c3",
)

// NonceTracker tracks nonces for multiple addresses
type NonceTracker struct {
	nonces map[common.Address]uint64
}

// NewNonceTracker creates a new nonce tracker
func NewNonceTracker() *NonceTracker {
	return &NonceTracker{
		nonces: make(map[common.Address]uint64),
	}
}

// NonceAt returns the current nonce for an address and increments it
func (nt *NonceTracker) NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error) {
	nonce := nt.nonces[account]
	nt.nonces[account] = nonce + 1
	return nonce, nil
}

// mapAddress creates a deterministic mapping from an original address to a new address
// that we can control (i.e., we have the private key for). This is done by using the
// original address as a seed to generate a new ECDSA keypair.
func mapAddress(originalAddr common.Address) common.Address {
	// Use the address bytes as a seed to generate a deterministic private key
	priv, err := crypto.ToECDSA(crypto.Keccak256(originalAddr.Bytes()))
	if err != nil {
		panic(fmt.Sprintf("failed to generate key from address: %v", err))
	}
	// Return the address derived from the generated public key
	return crypto.PubkeyToAddress(priv.PublicKey)
}

func main() {
	// Define CLI flags
	mode := flag.String("mode", "fetch", "Operation mode: 'fetch' to fetch contract from Ethereum, 'generate' to generate dataset")
	inputFile := flag.String("input", "", "Input file path (required for 'generate' mode)")
	outputFile := flag.String("output", "", "Output file path (optional, defaults based on mode)")
	rpcURL := flag.String("rpc", "https://1rpc.io/eth", "Ethereum RPC URL (for 'fetch' mode)")

	flag.Parse()

	switch *mode {
	case "fetch":
		if err := fetchContract(*rpcURL, *outputFile); err != nil {
			log.Fatalf("fetch mode failed: %v", err)
		}
	case "generate":
		if *inputFile == "" {
			log.Fatal("input file is required for generate mode (use -input flag)")
		}
		if err := generateDatasetMode(*inputFile, *outputFile); err != nil {
			log.Fatalf("generate mode failed: %v", err)
		}
	default:
		log.Fatalf("unknown mode: %s (use 'fetch' or 'generate')", *mode)
	}
}

// fetchContract fetches USDC contract bytecode and implementation from Ethereum mainnet
func fetchContract(rpcURL, outputFile string) error {
	ctx := context.Background()

	// Set default output file if not specified
	if outputFile == "" {
		outputFile = path.Join("integration", "perf", "testdata", "USDC_contract.json")
	}

	log.Printf("Connecting to Ethereum at %s...", rpcURL)

	// Connect to Ethereum mainnet
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return fmt.Errorf("failed to connect to Ethereum client: %w", err)
	}

	// ------------------------------------------------------------------
	// 1. Fetch contract bytecode
	// ------------------------------------------------------------------
	log.Printf("Fetching USDC proxy bytecode at %s...", usdcAddress.Hex())
	code, err := client.CodeAt(ctx, usdcAddress, nil)
	if err != nil {
		return fmt.Errorf("failed to fetch contract code: %w", err)
	}

	log.Printf("USDC proxy bytecode length: %d bytes", len(code))
	hexCode := hex.EncodeToString(code)
	usdcCode := hexCode

	// ------------------------------------------------------------------
	// 2. Read implementation address from storage (EIP-1967)
	// ------------------------------------------------------------------
	log.Printf("Reading implementation address from storage slot %s...", implementationSlot.Hex())
	rawStorage, err := client.StorageAt(ctx, usdcAddress, implementationSlot, nil)
	if err != nil {
		return fmt.Errorf("failed to read storage slot: %w", err)
	}

	if len(rawStorage) != 32 {
		return fmt.Errorf("unexpected storage size: %d", len(rawStorage))
	}

	// Implementation address is in the lower 20 bytes
	implAddress := common.BytesToAddress(rawStorage[12:])

	log.Printf("USDC implementation address: %s", implAddress.Hex())

	code, err = client.CodeAt(ctx, implAddress, nil)
	if err != nil {
		return fmt.Errorf("failed to fetch implementation code: %w", err)
	}

	log.Printf("USDC implementation bytecode length: %d bytes", len(code))
	hexCode = hex.EncodeToString(code)

	alloc := map[string]integration.AllocEntry{
		usdcAddress.Hex(): {
			Code: usdcCode,
			Storage: map[string]string{
				implementationSlot.Hex(): implAddress.Hex(),
			},
		},
		implAddress.Hex(): {
			Code: hexCode,
		},
	}

	b, err := json.MarshalIndent(alloc, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal data structure: %w", err)
	}

	log.Printf("Writing contract data to %s...", outputFile)
	err = os.WriteFile(outputFile, b, 0644)
	if err != nil {
		return fmt.Errorf("could not write to file: %w", err)
	}

	log.Printf("Successfully saved USDC contract data to %s", outputFile)
	return nil
}

// generateDatasetMode generates a JSON dataset from a TSV file with Ethereum transactions
func generateDatasetMode(inputFile, outputFile string) error {
	ctx := context.Background()

	// Set default output file if not specified
	if outputFile == "" {
		outputFile = path.Join("integration", "perf", "testdata", "USDC_dataset.json.gz")
	}

	// Load the contract allocation (we need this for the test harness setup)
	// For now, we'll create a minimal test harness without full Fabric setup
	// since we only need the EthClient functionality

	log.Printf("Parsing dataset from %s...", inputFile)

	// Parse the dataset
	transfers, err := ParseTSVGZ(inputFile)
	if err != nil {
		return fmt.Errorf("failed to parse TSV file: %w", err)
	}

	if len(transfers) == 0 {
		return fmt.Errorf("dataset is empty")
	}

	log.Printf("Loaded %d transfers from dataset", len(transfers))

	// Create nonce tracker to manage nonces per sender
	nonceTracker := NewNonceTracker()

	successCount := 0
	failCount := 0

	log.Printf("Generating transactions...")

	for i := range transfers {
		transfer := &transfers[i] // Get pointer to modify in place

		chain := params.AllEthashProtocolChanges
		chain.ChainID = big.NewInt(4011)

		// Create an EthClient for the sender using the mapped address
		ethSender, err := integration.NewEthClientFromAddress(transfer.Sender, contracts.FiatTokenV22MetaData, nil)
		if err != nil {
			log.Printf("Transfer %d: Failed to create EthClient for sender %s: %v", i, transfer.Sender.Hex(), err)
			failCount++
			continue
		}

		// fmt.Printf("Real sender %s, mapped sender %s\n", transfer.Sender.Hex(), ethSender.Address().Hex())

		// Map the recipient address as well
		mappedRecipient := mapAddress(transfer.Recipient)

		// Convert uint256.Int to big.Int for the transfer value
		transferValue := transfer.Value.ToBig()

		// Create the transaction using the nonce tracker
		// The nonce tracker will automatically increment the nonce for each sender
		tx, err := ethSender.TxForCall(ctx, nonceTracker, &usdcAddress, "transfer", mappedRecipient, transferValue)
		if err != nil {
			log.Printf("Transfer %d: Failed to create transaction: %v", i, err)
			failCount++
			continue
		}

		// Marshal the transaction to bytes (RLP encoding)
		txBytes, err := tx.MarshalBinary()
		if err != nil {
			log.Printf("Transfer %d: Failed to marshal transaction: %v", i, err)
			failCount++
			continue
		}

		// Store the transaction bytes in the transfer
		transfer.Transaction = txBytes

		successCount++

		// Log progress every 100 transactions
		if (i+1)%100 == 0 {
			log.Printf("Progress: %d/%d transactions generated (%d successful, %d failed)",
				i+1, len(transfers), successCount, failCount)
		}
	}

	log.Printf("Transaction generation complete: %d successful, %d failed out of %d total transfers",
		successCount, failCount, len(transfers))

	if successCount == 0 {
		return fmt.Errorf("no successful transactions generated")
	}

	// Save to gzipped JSON file
	log.Printf("Saving dataset to %s...", outputFile)

	// Create output file
	outFile, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	// Create gzip writer
	gzWriter := gzip.NewWriter(outFile)
	defer gzWriter.Close()

	// Create JSON encoder
	encoder := json.NewEncoder(gzWriter)
	encoder.SetIndent("", "  ") // Pretty print

	// Encode the transfers array
	err = encoder.Encode(transfers)
	if err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	// Flush and close gzip writer
	err = gzWriter.Close()
	if err != nil {
		return fmt.Errorf("failed to close gzip writer: %w", err)
	}

	err = outFile.Close()
	if err != nil {
		return fmt.Errorf("failed to close output file: %w", err)
	}

	log.Printf("Successfully saved %d transfers to %s", len(transfers), outputFile)
	return nil
}
