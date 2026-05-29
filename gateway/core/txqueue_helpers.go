/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// participantsForTx extracts the sender and recipient addresses from a transaction.
// Returns a slice containing unique participant addresses (1 or 2 elements).
func participantsForTx(tx *types.Transaction) []common.Address {
	participants := make([]common.Address, 0, 2)

	if sender, ok := senderForTx(tx); ok {
		participants = append(participants, sender)
	}

	if recipient, ok := recipientForTx(tx); ok && !containsAddress(participants, recipient) {
		participants = append(participants, recipient)
	}

	return participants
}

// senderForTx extracts the sender address from a transaction.
// Returns (address, true) if successful, (zero address, false) otherwise.
func senderForTx(tx *types.Transaction) (common.Address, bool) {
	if !tx.Protected() && tx.Type() == types.LegacyTxType {
		return common.Address{}, false
	}

	signer := types.LatestSignerForChainID(tx.ChainId())
	sender, err := types.Sender(signer, tx)
	if err != nil {
		return common.Address{}, false
	}

	return sender, true
}

// erc20TransferSelector is the 4-byte function selector for ERC20 transfer(address,uint256).
var erc20TransferSelector = [4]byte{0xa9, 0x05, 0x9c, 0xbb}

// recipientForTx extracts the recipient address from an ERC20 transfer(address,uint256) call.
// Returns (address, true) only when the calldata matches the ERC20 transfer selector and is the
// expected length. Without the selector check, any call with >=68 bytes of data would be decoded
// as a transfer and create false-positive dependency edges between unrelated transactions.
func recipientForTx(tx *types.Transaction) (common.Address, bool) {
	if tx.To() == nil {
		return common.Address{}, false
	}

	data := tx.Data()
	if len(data) < 4+32+32 {
		return common.Address{}, false
	}
	if data[0] != erc20TransferSelector[0] || data[1] != erc20TransferSelector[1] ||
		data[2] != erc20TransferSelector[2] || data[3] != erc20TransferSelector[3] {
		return common.Address{}, false
	}

	// Extract recipient from ERC20 transfer calldata (offset 4 + 12 bytes)
	recipientOffset := 4 + 12
	return common.BytesToAddress(data[recipientOffset : recipientOffset+20]), true
}

// containsAddress checks if a slice of addresses contains the target address.
func containsAddress(addresses []common.Address, target common.Address) bool {
	for _, address := range addresses {
		if address == target {
			return true
		}
	}
	return false
}

// Made with Bob
