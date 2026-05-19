/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/hyperledger/fabric-x-evm/endorser"
)

// Default prime value: 1 billion tokens (with 6 decimals for USDC)
var primeValue = new(uint256.Int).Mul(uint256.NewInt(1_000_000_000), uint256.NewInt(1_000_000))

// BalancePrimingWrapper wraps a StateDB and intercepts GetState calls to prime
// ERC-20 balance slots with a high value when they are zero.
type BalancePrimingWrapper struct {
	endorser.ExtendedStateDB
	contractAddr    common.Address // The ERC-20 contract address
	senderAddr      common.Address // The sender address to prime
	balanceSlot     common.Hash    // The storage slot for the sender's balance
	mappingPosition uint64         // The position of the balances mapping in storage
	enabled         bool           // Whether priming is enabled
	expectedNonce   uint64         // The nonce the current transaction expects
	nonceEnabled    bool           // Whether nonce priming is active
}

// NewBalancePrimingWrapper creates a new wrapper that primes ERC-20 balance slots.
func NewBalancePrimingWrapper(stateDB endorser.ExtendedStateDB, contractAddr common.Address, mappingPosition uint64) *BalancePrimingWrapper {
	return &BalancePrimingWrapper{
		ExtendedStateDB: stateDB,
		contractAddr:    contractAddr,
		mappingPosition: mappingPosition,
		enabled:         false,
	}
}

// SetSender sets the sender address and calculates the balance slot.
func (w *BalancePrimingWrapper) SetSender(sender common.Address) {
	w.senderAddr = sender
	w.balanceSlot = GetERC20BalanceSlot(sender, w.mappingPosition)
	w.enabled = true

	if false {
		fmt.Printf("[BalancePriming] SetSender called: sender=%s, balanceSlot=%s, contractAddr=%s\n",
			sender.Hex(), w.balanceSlot.Hex(), w.contractAddr.Hex())
	}
}

// GetState intercepts storage reads and primes the balance slot if needed.
func (w *BalancePrimingWrapper) GetState(addr common.Address, slot common.Hash) common.Hash {
	// Check if this is a read of our target balance slot
	if w.enabled && addr == w.contractAddr && slot == w.balanceSlot {
		if false {
			fmt.Printf("[BalancePriming] GetState intercepted: addr=%s, slot=%s (matches target)\n",
				addr.Hex(), slot.Hex())
		}

		// Get the current value
		currentValue := w.ExtendedStateDB.GetState(addr, slot)

		if false {
			fmt.Printf("[BalancePriming] Current value: %s\n", currentValue.Hex())
		}

		// If it's zero, prime it with a high value
		if currentValue == (common.Hash{}) {
			if false {
				fmt.Printf("[BalancePriming] *** PRIMING BALANCE *** sender=%s, slot=%s, value=%s\n",
					w.senderAddr.Hex(), slot.Hex(), primeValue.String())
			}

			// Intentionally not calling SetState here. Writing the primed value to the
			// StateDB would include it in the transaction's write set and commit a fake
			// balance to the ledger, affecting future transactions. Returning it only
			// from GetState keeps the priming invisible to the ledger while still
			// allowing the EVM execution to proceed with a non-zero balance.

			// Return the primed value
			return common.BytesToHash(primeValue.Bytes())
		} else {
			if false {
				fmt.Printf("[BalancePriming] Balance already set, not priming\n")
			}
		}
	}

	// Otherwise, just pass through to the underlying StateDB
	return w.ExtendedStateDB.GetState(addr, slot)
}

// SetExpectedNonce stores the nonce the current transaction expects.
func (w *BalancePrimingWrapper) SetExpectedNonce(nonce uint64) {
	// fmt.Printf("[NoncePriming] SetExpectedNonce sender=%s nonce=%d enabled(before)=%t\n", w.senderAddr.Hex(), nonce, w.nonceEnabled)
	w.expectedNonce = nonce
	w.nonceEnabled = true
}

// GetNonce intercepts nonce reads and returns the expected transaction nonce
// when nonce priming is enabled. The underlying read still happens to preserve
// the MVCC read dependency on the real nonce key version.
func (w *BalancePrimingWrapper) GetNonce(addr common.Address) uint64 {
	// Always delegate first to record the MVCC read dependency
	// realNonce := w.ExtendedStateDB.GetNonce(addr)
	// fmt.Printf("[NoncePriming] GetNonce addr=%s sender=%s enabled=%t expected=%d real=%d\n", addr.Hex(), w.senderAddr.Hex(), w.nonceEnabled, w.expectedNonce, realNonce)

	// If nonce priming is enabled and this is the sender, return the
	// expected nonce so the tx.Nonce() == ledgerNonce check in Executor.Send passes.
	if w.nonceEnabled && addr == w.senderAddr {
		return w.expectedNonce
	}

	// For all other addresses, delegate normally
	return w.ExtendedStateDB.GetNonce(addr)
}

// GetERC20BalanceSlot computes the storage slot for a balance in an ERC-20 mapping(address => uint256).
// This uses the Solidity storage layout: keccak256(abi.encodePacked(address, mappingPosition))
func GetERC20BalanceSlot(account common.Address, mappingPosition uint64) common.Hash {
	// Concatenate: address (32 bytes) + mapping position (32 bytes)
	data := append(
		common.LeftPadBytes(account.Bytes(), 32),
		common.LeftPadBytes(new(big.Int).SetUint64(mappingPosition).Bytes(), 32)...,
	)
	return crypto.Keccak256Hash(data)
}
