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

// SetSender enables priming for transactions from the given sender.
func (w *BalancePrimingWrapper) SetSender(sender common.Address) {
	w.enabled = true
}

// GetState intercepts storage reads and primes the balance slot if needed.
func (w *BalancePrimingWrapper) GetState(addr common.Address, slot common.Hash) common.Hash {
	if w.enabled && addr == w.contractAddr {
		currentValue := w.ExtendedStateDB.GetState(addr, slot)
		if currentValue == (common.Hash{}) {
			// Intentionally not calling SetState here. Writing the primed value to the
			// StateDB would include it in the transaction's write set and commit a fake
			// balance to the ledger, affecting future transactions. Returning it only
			// from GetState keeps the priming invisible to the ledger while still
			// allowing the EVM execution to proceed with a non-zero balance.
			return common.BytesToHash(primeValue.Bytes())
		}
	}

	return w.ExtendedStateDB.GetState(addr, slot)
}

// SetExpectedNonce stores the nonce the current transaction expects.
func (w *BalancePrimingWrapper) SetExpectedNonce(nonce uint64) {
	w.expectedNonce = nonce
	w.nonceEnabled = true
}

// GetNonce intercepts nonce reads and returns the expected transaction nonce
// when nonce priming is enabled. The underlying read still happens to preserve
// the MVCC read dependency on the real nonce key version.
func (w *BalancePrimingWrapper) GetNonce(addr common.Address) uint64 {
	if w.nonceEnabled {
		return w.expectedNonce
	}
	return w.ExtendedStateDB.GetNonce(addr)
}

// GetERC20BalanceSlot computes the storage slot for a balance in an ERC-20 mapping(address => uint256).
// This uses the Solidity storage layout: keccak256(abi.encodePacked(address, mappingPosition))
func GetERC20BalanceSlot(account common.Address, mappingPosition uint64) common.Hash {
	data := append(
		common.LeftPadBytes(account.Bytes(), 32),
		common.LeftPadBytes(new(big.Int).SetUint64(mappingPosition).Bytes(), 32)...,
	)
	return crypto.Keccak256Hash(data)
}
