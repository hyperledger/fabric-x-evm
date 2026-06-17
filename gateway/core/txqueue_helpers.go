/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package core

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// participantsForTx extracts the sender and recipient addresses from a transaction with sender.
// Returns a slice containing unique participant addresses (1 or 2 elements).
// The sender is already verified and provided, avoiding redundant signature verification.
func participantsForTx(txWithSender TxWithSender) []common.Address {
	participants := make([]common.Address, 0, 2)

	// Use the pre-verified sender address
	participants = append(participants, txWithSender.From)

	if recipient, ok := recipientForTx(txWithSender.Tx); ok && !containsAddress(participants, recipient) {
		participants = append(participants, recipient)
	}

	return participants
}

// recipientForTx extracts the recipient address from a transaction.
// For ERC20 transfers, it extracts the recipient from the calldata.
// Returns (address, true) if successful, (zero address, false) otherwise.
func recipientForTx(tx *types.Transaction) (common.Address, bool) {
	if tx.To() == nil {
		return common.Address{}, false
	}

	data := tx.Data()
	if len(data) < 4+32+32 {
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
