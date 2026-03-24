/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package ledger

import "github.com/ethereum/go-ethereum/core/types"

type ParseResult struct {
	EthTx *types.Transaction
	Logs  []byte
	TxID  string
}
