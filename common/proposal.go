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
	ProposalTypeEVMTx ProposalType = iota
	ProposalTypeCall
	ProposalTypeState
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
