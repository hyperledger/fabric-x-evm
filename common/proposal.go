/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

type ProposalType byte

const (
	ProposalTypeEVMTx ProposalType = 0xfb
	ProposalTypeCall
	ProposalTypeState
)

const (
	StatusOK          int32 = 200
	StatusEVMRevert   int32 = 201
	StatusExecFailure int32 = 202 // valid tx whose EVM execution failed (out of gas, invalid opcode, ...); mined with a failed receipt, like StatusEVMRevert
	StatusTxRejected  int32 = 400 // invalid tx, rejected before execution (nonce, funds, intrinsic gas, ...)
	StatusServerError int32 = 500
)
