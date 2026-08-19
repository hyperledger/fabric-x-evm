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
