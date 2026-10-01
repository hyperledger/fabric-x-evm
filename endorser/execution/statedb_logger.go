/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package execution

import (
	"github.com/ethereum/go-ethereum/common"
	ethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/types/bal"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// StateDBLogger wraps a StateDB and logs all method calls at Debug level.
type StateDBLogger struct {
	inner  ExtendedStateDB
	logger *flogging.FabricLogger
}

// NewStateDBLogger creates a new logging wrapper around inner.
func NewStateDBLogger(inner ExtendedStateDB) ExtendedStateDB {
	return &StateDBLogger{
		inner:  inner,
		logger: flogging.MustGetLogger("endorser.execution.statedb"),
	}
}

func (l *StateDBLogger) CreateAccount(addr common.Address) {
	l.logger.Debugf("CreateAccount: addr=%s", addr.Hex())
	l.inner.CreateAccount(addr)
}

func (l *StateDBLogger) CreateContract(addr common.Address) {
	l.logger.Debugf("CreateContract: addr=%s", addr.Hex())
	l.inner.CreateContract(addr)
}

func (l *StateDBLogger) SubBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	l.logger.Debugf("SubBalance: addr=%s amount=%s reason=%v", addr.Hex(), amount.String(), reason)
	prev := l.inner.SubBalance(addr, amount, reason)
	l.logger.Debugf("SubBalance: prev=%s", prev.String())
	return prev
}

func (l *StateDBLogger) AddBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	l.logger.Debugf("AddBalance: addr=%s amount=%s reason=%v", addr.Hex(), amount.String(), reason)
	prev := l.inner.AddBalance(addr, amount, reason)
	l.logger.Debugf("AddBalance: prev=%s", prev.String())
	return prev
}

func (l *StateDBLogger) GetBalance(addr common.Address) *uint256.Int {
	result := l.inner.GetBalance(addr)
	l.logger.Debugf("GetBalance: addr=%s result=%s", addr.Hex(), result.String())
	return result
}

func (l *StateDBLogger) GetNonce(addr common.Address) uint64 {
	result := l.inner.GetNonce(addr)
	l.logger.Debugf("GetNonce: addr=%s result=%d", addr.Hex(), result)
	return result
}

func (l *StateDBLogger) SetNonce(addr common.Address, nonce uint64, reason tracing.NonceChangeReason) {
	l.logger.Debugf("SetNonce: addr=%s nonce=%d reason=%v", addr.Hex(), nonce, reason)
	l.inner.SetNonce(addr, nonce, reason)
}

func (l *StateDBLogger) GetCodeHash(addr common.Address) common.Hash {
	result := l.inner.GetCodeHash(addr)
	l.logger.Debugf("GetCodeHash: addr=%s result=%s", addr.Hex(), result.Hex())
	return result
}

func (l *StateDBLogger) GetCode(addr common.Address) []byte {
	result := l.inner.GetCode(addr)
	l.logger.Debugf("GetCode: addr=%s len=%d", addr.Hex(), len(result))
	return result
}

func (l *StateDBLogger) SetCode(addr common.Address, code []byte, reason tracing.CodeChangeReason) []byte {
	l.logger.Debugf("SetCode: addr=%s codeLen=%d reason=%v", addr.Hex(), len(code), reason)
	prev := l.inner.SetCode(addr, code, reason)
	l.logger.Debugf("SetCode: prevLen=%d", len(prev))
	return prev
}

func (l *StateDBLogger) GetCodeSize(addr common.Address) int {
	result := l.inner.GetCodeSize(addr)
	l.logger.Debugf("GetCodeSize: addr=%s result=%d", addr.Hex(), result)
	return result
}

func (l *StateDBLogger) AddRefund(gas uint64) {
	l.logger.Debugf("AddRefund: gas=%d", gas)
	l.inner.AddRefund(gas)
}

func (l *StateDBLogger) SubRefund(gas uint64) {
	l.logger.Debugf("SubRefund: gas=%d", gas)
	l.inner.SubRefund(gas)
}

func (l *StateDBLogger) GetRefund() uint64 {
	result := l.inner.GetRefund()
	l.logger.Debugf("GetRefund: result=%d", result)
	return result
}

func (l *StateDBLogger) GetStateAndCommittedState(addr common.Address, hash common.Hash) (common.Hash, common.Hash) {
	current, committed := l.inner.GetStateAndCommittedState(addr, hash)
	l.logger.Debugf("GetStateAndCommittedState: addr=%s hash=%s current=%s committed=%s", addr.Hex(), hash.Hex(), current.Hex(), committed.Hex())
	return current, committed
}

func (l *StateDBLogger) GetState(addr common.Address, hash common.Hash) common.Hash {
	result := l.inner.GetState(addr, hash)
	l.logger.Debugf("GetState: addr=%s key=%s result=%s", addr.Hex(), hash.Hex(), result.Hex())
	return result
}

func (l *StateDBLogger) SetState(addr common.Address, key common.Hash, value common.Hash) common.Hash {
	l.logger.Debugf("SetState: addr=%s key=%s value=%s", addr.Hex(), key.Hex(), value.Hex())
	prev := l.inner.SetState(addr, key, value)
	l.logger.Debugf("SetState: prev=%s", prev.Hex())
	return prev
}

func (l *StateDBLogger) GetStorageRoot(addr common.Address) common.Hash {
	result := l.inner.GetStorageRoot(addr)
	l.logger.Debugf("GetStorageRoot: addr=%s result=%s", addr.Hex(), result.Hex())
	return result
}

func (l *StateDBLogger) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	result := l.inner.GetTransientState(addr, key)
	l.logger.Debugf("GetTransientState: addr=%s key=%s result=%s", addr.Hex(), key.Hex(), result.Hex())
	return result
}

func (l *StateDBLogger) SetTransientState(addr common.Address, key, value common.Hash) {
	l.logger.Debugf("SetTransientState: addr=%s key=%s value=%s", addr.Hex(), key.Hex(), value.Hex())
	l.inner.SetTransientState(addr, key, value)
}

func (l *StateDBLogger) SelfDestruct(addr common.Address) {
	l.logger.Debugf("SelfDestruct: addr=%s", addr.Hex())
	l.inner.SelfDestruct(addr)
}

func (l *StateDBLogger) HasSelfDestructed(addr common.Address) bool {
	result := l.inner.HasSelfDestructed(addr)
	l.logger.Debugf("HasSelfDestructed: addr=%s result=%t", addr.Hex(), result)
	return result
}

func (l *StateDBLogger) Exist(addr common.Address) bool {
	result := l.inner.Exist(addr)
	l.logger.Debugf("Exist: addr=%s result=%t", addr.Hex(), result)
	return result
}

func (l *StateDBLogger) Empty(addr common.Address) bool {
	result := l.inner.Empty(addr)
	l.logger.Debugf("Empty: addr=%s result=%t", addr.Hex(), result)
	return result
}

func (l *StateDBLogger) AddressInAccessList(addr common.Address) bool {
	result := l.inner.AddressInAccessList(addr)
	l.logger.Debugf("AddressInAccessList: addr=%s result=%t", addr.Hex(), result)
	return result
}

func (l *StateDBLogger) SlotInAccessList(addr common.Address, slot common.Hash) (bool, bool) {
	addressOk, slotOk := l.inner.SlotInAccessList(addr, slot)
	l.logger.Debugf("SlotInAccessList: addr=%s slot=%s addressOk=%t slotOk=%t", addr.Hex(), slot.Hex(), addressOk, slotOk)
	return addressOk, slotOk
}

func (l *StateDBLogger) AddAddressToAccessList(addr common.Address) {
	l.logger.Debugf("AddAddressToAccessList: addr=%s", addr.Hex())
	l.inner.AddAddressToAccessList(addr)
}

func (l *StateDBLogger) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	l.logger.Debugf("AddSlotToAccessList: addr=%s slot=%s", addr.Hex(), slot.Hex())
	l.inner.AddSlotToAccessList(addr, slot)
}

func (l *StateDBLogger) Touch(addr common.Address) {
	l.logger.Debugf("Touch: addr=%s", addr.Hex())
	l.inner.Touch(addr)
}

func (l *StateDBLogger) IsNewContract(addr common.Address) bool {
	result := l.inner.IsNewContract(addr)
	l.logger.Debugf("IsNewContract: addr=%s result=%t", addr.Hex(), result)
	return result
}

func (l *StateDBLogger) Prepare(rules params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses types.AccessList) {
	destStr := "nil"
	if dest != nil {
		destStr = dest.Hex()
	}
	l.logger.Debugf("Prepare: sender=%s coinbase=%s dest=%s precompiles=%d txAccesses=%d",
		sender.Hex(), coinbase.Hex(), destStr, len(precompiles), len(txAccesses))
	l.inner.Prepare(rules, sender, coinbase, dest, precompiles, txAccesses)
}

func (l *StateDBLogger) RevertToSnapshot(snapshot int) {
	l.logger.Debugf("RevertToSnapshot: snapshot=%d", snapshot)
	l.inner.RevertToSnapshot(snapshot)
}

func (l *StateDBLogger) Snapshot() int {
	result := l.inner.Snapshot()
	l.logger.Debugf("Snapshot: result=%d", result)
	return result
}

func (l *StateDBLogger) AddLog(logEntry *types.Log) {
	l.logger.Debugf("AddLog: %+v", logEntry)
	l.inner.AddLog(logEntry)
}

func (l *StateDBLogger) AddPreimage(hash common.Hash, preimage []byte) {
	l.logger.Debugf("AddPreimage: hash=%s preimageLen=%d", hash.Hex(), len(preimage))
	l.inner.AddPreimage(hash, preimage)
}

func (l *StateDBLogger) Witness() *stateless.Witness {
	l.logger.Debugf("Witness")
	return l.inner.Witness()
}

func (l *StateDBLogger) AccessEvents() *ethstate.AccessEvents {
	l.logger.Debugf("AccessEvents")
	return l.inner.AccessEvents()
}

func (l *StateDBLogger) Finalise(deleteEmptyObjects bool) *bal.ConstructionBlockAccessList {
	l.logger.Debugf("Finalise: deleteEmptyObjects=%t", deleteEmptyObjects)
	return l.inner.Finalise(deleteEmptyObjects)
}

func (l *StateDBLogger) Result() blocks.ReadWriteSet {
	result := l.inner.Result()
	l.logger.Debugf("Result: reads=%d writes=%d", len(result.Reads), len(result.Writes))
	return result
}

func (l *StateDBLogger) Logs() []Log {
	result := l.inner.Logs()
	l.logger.Debugf("Logs: len=%d", len(result))
	return result
}

// Ensure StateDBLogger implements ExtendedStateDB
var _ ExtendedStateDB = (*StateDBLogger)(nil)

func (l *StateDBLogger) SetTxContext(h common.Hash, i int, bai uint32) {
	l.inner.SetTxContext(h, i, bai)
}
