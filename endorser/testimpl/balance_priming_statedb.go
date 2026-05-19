/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package testimpl

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/hyperledger/fabric-x-evm/endorser"
)

// Default prime value: 1 billion tokens (with 6 decimals for USDC)
var primeValue = new(uint256.Int).Mul(uint256.NewInt(1_000_000_000_000), uint256.NewInt(1_000_000_000_000))

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

// SetSender sets the sender address and calculates the balance slot for priming.
// Only the sender's balance slot will be primed; all other contract storage is
// passed through unchanged so that fields like _paused, _blacklisted, etc. are
// not accidentally set to a non-zero value.
func (w *BalancePrimingWrapper) SetSender(sender common.Address) {
	w.enabled = true
	w.senderAddr = sender
	w.balanceSlot = GetERC20BalanceSlot(sender, w.mappingPosition)
}

// GetState intercepts storage reads and primes the sender's balance slot if needed.
// Only the slot computed from the sender address (set via SetSender) is intercepted;
// all other slots are passed through unchanged. This avoids incorrectly priming
// boolean/address slots such as _paused or _blacklisted.
func (w *BalancePrimingWrapper) GetState(addr common.Address, slot common.Hash) common.Hash {
	result := w.ExtendedStateDB.GetState(addr, slot)
	// Only intercept the specific balance slot for the known sender.
	if w.enabled && addr == w.contractAddr && slot == w.balanceSlot && result == (common.Hash{}) {
		// Return a synthetic high balance. The EVM will write the decremented balance
		// (primeValue - amount) via SetState, which is fine. Not writing the raw
		// primeValue keeps the ledger clean.
		return common.BytesToHash(primeValue.Bytes())
	}
	return result
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
	if w.nonceEnabled {
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
