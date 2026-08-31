/*
Copyright IBM Corp. 2016 All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package common

type ProposalType byte

const (
	ProposalTypeEVMTx ProposalType = 0xfb
	// ProposalTypeSetBalance marks a setBalance invocation's first arg so
	// ConvertToDomain can filter it out; it is not a discriminator.
	ProposalTypeSetBalance ProposalType = 0xfa
	// ProposalTypeSetCode marks a setCode invocation's first arg so
	// ConvertToDomain can filter it out; it is not a discriminator.
	ProposalTypeSetCode ProposalType = 0xf9
)

const (
	StatusOK          int32 = 200
	StatusEVMRevert   int32 = 201
	StatusTxRejected  int32 = 400 // invalid tx, rejected before execution (nonce, funds, intrinsic gas, ...)
	StatusExecFailure int32 = 460 // valid tx whose EVM execution failed (out of gas, invalid opcode, ...); should be mined (not yet)
	StatusServerError int32 = 500
)
