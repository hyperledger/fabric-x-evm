/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type ProposalType byte

const (
	ProposalTypeEVMTx ProposalType = 0xfb
	ProposalTypeCall
	ProposalTypeState
)

const (
	StatusOK          int32 = 200
	StatusEVMRevert   int32 = 201
	StatusTxRejected  int32 = 400 // invalid tx, rejected before execution (nonce, funds, intrinsic gas, ...)
	StatusExecFailure int32 = 460 // valid tx whose EVM execution failed (out of gas, invalid opcode, ...); should be mined (not yet)
	StatusServerError int32 = 500
)

// StateQuery defines what to query from the ledger state, and at which snapshot.
type StateQuery struct {
	Account     common.Address
	Key         common.Hash
	BlockNumber *big.Int
	Type        QueryType
}

type QueryType byte

const (
	QueryTypeBalance QueryType = iota
	QueryTypeStorage
	QueryTypeCode
	QueryTypeNonce
)
