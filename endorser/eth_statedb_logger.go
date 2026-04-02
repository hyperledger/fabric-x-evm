/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package endorser

import (
	"github.com/ethereum/go-ethereum/common"
	ethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie/utils"
	"github.com/holiman/uint256"
	"github.com/hyperledger/fabric-x-evm/utils/logger"
)

// EthStateDBLogger wraps an Ethereum StateDB and logs all method calls
type EthStateDBLogger struct {
	inner  *ethstate.StateDB
	logger logger.Logger
}

// NewEthStateDBLogger creates a new logging wrapper
func NewEthStateDBLogger(inner *ethstate.StateDB) *EthStateDBLogger {
	return &EthStateDBLogger{
		inner:  inner,
		logger: logger.NewLogger("EthStateDB"),
	}
}

func (l *EthStateDBLogger) CreateAccount(addr common.Address) {
	l.logger.Debugf("[EthStateDB] CreateAccount: addr=%s", addr.Hex())
	l.inner.CreateAccount(addr)
}

func (l *EthStateDBLogger) CreateContract(addr common.Address) {
	l.logger.Debugf("[EthStateDB] CreateContract: addr=%s", addr.Hex())
	l.inner.CreateContract(addr)
}

func (l *EthStateDBLogger) SubBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	l.logger.Debugf("[EthStateDB] SubBalance: addr=%s, amount=%s, reason=%v", addr.Hex(), amount.String(), reason)
	return l.inner.SubBalance(addr, amount, reason)
}

func (l *EthStateDBLogger) AddBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	l.logger.Debugf("[EthStateDB] AddBalance: addr=%s, amount=%s, reason=%v", addr.Hex(), amount.String(), reason)
	return l.inner.AddBalance(addr, amount, reason)
}

func (l *EthStateDBLogger) GetBalance(addr common.Address) *uint256.Int {
	result := l.inner.GetBalance(addr)
	l.logger.Debugf("[EthStateDB] GetBalance: addr=%s -> %s", addr.Hex(), result.String())
	return result
}

func (l *EthStateDBLogger) GetNonce(addr common.Address) uint64 {
	result := l.inner.GetNonce(addr)
	l.logger.Debugf("[EthStateDB] GetNonce: addr=%s -> %d", addr.Hex(), result)
	return result
}

func (l *EthStateDBLogger) SetNonce(addr common.Address, nonce uint64, reason tracing.NonceChangeReason) {
	l.logger.Debugf("[EthStateDB] SetNonce: addr=%s, nonce=%d, reason=%v", addr.Hex(), nonce, reason)
	l.inner.SetNonce(addr, nonce, reason)
}

func (l *EthStateDBLogger) GetCodeHash(addr common.Address) common.Hash {
	result := l.inner.GetCodeHash(addr)
	l.logger.Debugf("[EthStateDB] GetCodeHash: addr=%s -> %s", addr.Hex(), result.Hex())
	return result
}

func (l *EthStateDBLogger) GetCode(addr common.Address) []byte {
	result := l.inner.GetCode(addr)
	l.logger.Debugf("[EthStateDB] GetCode: addr=%s -> len=%d", addr.Hex(), len(result))
	return result
}

func (l *EthStateDBLogger) SetCode(addr common.Address, code []byte, reason tracing.CodeChangeReason) []byte {
	l.logger.Debugf("[EthStateDB] SetCode: addr=%s, code len=%d, reason=%v", addr.Hex(), len(code), reason)
	return l.inner.SetCode(addr, code, reason)
}

func (l *EthStateDBLogger) GetCodeSize(addr common.Address) int {
	result := l.inner.GetCodeSize(addr)
	l.logger.Debugf("[EthStateDB] GetCodeSize: addr=%s -> %d", addr.Hex(), result)
	return result
}

func (l *EthStateDBLogger) AddRefund(gas uint64) {
	l.logger.Debugf("[EthStateDB] AddRefund: gas=%d", gas)
	l.inner.AddRefund(gas)
}

func (l *EthStateDBLogger) SubRefund(gas uint64) {
	l.logger.Debugf("[EthStateDB] SubRefund: gas=%d", gas)
	l.inner.SubRefund(gas)
}

func (l *EthStateDBLogger) GetRefund() uint64 {
	result := l.inner.GetRefund()
	l.logger.Debugf("[EthStateDB] GetRefund -> %d", result)
	return result
}

func (l *EthStateDBLogger) GetStateAndCommittedState(addr common.Address, hash common.Hash) (common.Hash, common.Hash) {
	current, committed := l.inner.GetStateAndCommittedState(addr, hash)
	l.logger.Debugf("[EthStateDB] GetStateAndCommittedState: addr=%s, hash=%s -> current=%s, committed=%s",
		addr.Hex(), hash.Hex(), current.Hex(), committed.Hex())
	return current, committed
}

func (l *EthStateDBLogger) GetState(addr common.Address, hash common.Hash) common.Hash {
	result := l.inner.GetState(addr, hash)
	l.logger.Debugf("[EthStateDB] GetState: addr=%s, hash=%s -> %s", addr.Hex(), hash.Hex(), result.Hex())
	return result
}

func (l *EthStateDBLogger) SetState(addr common.Address, key common.Hash, value common.Hash) common.Hash {
	prev := l.inner.SetState(addr, key, value)
	l.logger.Debugf("[EthStateDB] SetState: addr=%s, key=%s, value=%s -> prev=%s",
		addr.Hex(), key.Hex(), value.Hex(), prev.Hex())
	return prev
}

func (l *EthStateDBLogger) GetStorageRoot(addr common.Address) common.Hash {
	result := l.inner.GetStorageRoot(addr)
	l.logger.Debugf("[EthStateDB] GetStorageRoot: addr=%s -> %s", addr.Hex(), result.Hex())
	return result
}

func (l *EthStateDBLogger) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	result := l.inner.GetTransientState(addr, key)
	l.logger.Debugf("[EthStateDB] GetTransientState: addr=%s, key=%s -> %s", addr.Hex(), key.Hex(), result.Hex())
	return result
}

func (l *EthStateDBLogger) SetTransientState(addr common.Address, key, value common.Hash) {
	l.logger.Debugf("[EthStateDB] SetTransientState: addr=%s, key=%s, value=%s", addr.Hex(), key.Hex(), value.Hex())
	l.inner.SetTransientState(addr, key, value)
}

func (l *EthStateDBLogger) SelfDestruct(addr common.Address) uint256.Int {
	balance := l.inner.SelfDestruct(addr)
	l.logger.Debugf("[EthStateDB] SelfDestruct: addr=%s -> balance=%s", addr.Hex(), balance.String())
	return balance
}

func (l *EthStateDBLogger) HasSelfDestructed(addr common.Address) bool {
	result := l.inner.HasSelfDestructed(addr)
	l.logger.Debugf("[EthStateDB] HasSelfDestructed: addr=%s -> %t", addr.Hex(), result)
	return result
}

func (l *EthStateDBLogger) SelfDestruct6780(addr common.Address) (uint256.Int, bool) {
	balance, destructed := l.inner.SelfDestruct6780(addr)
	l.logger.Debugf("[EthStateDB] SelfDestruct6780: addr=%s -> balance=%s, destructed=%t",
		addr.Hex(), balance.String(), destructed)
	return balance, destructed
}

func (l *EthStateDBLogger) Exist(addr common.Address) bool {
	result := l.inner.Exist(addr)
	l.logger.Debugf("[EthStateDB] Exist: addr=%s -> %t", addr.Hex(), result)
	return result
}

func (l *EthStateDBLogger) Empty(addr common.Address) bool {
	result := l.inner.Empty(addr)
	l.logger.Debugf("[EthStateDB] Empty: addr=%s -> %t", addr.Hex(), result)
	return result
}

func (l *EthStateDBLogger) AddressInAccessList(addr common.Address) bool {
	result := l.inner.AddressInAccessList(addr)
	l.logger.Debugf("[EthStateDB] AddressInAccessList: addr=%s -> %t", addr.Hex(), result)
	return result
}

func (l *EthStateDBLogger) SlotInAccessList(addr common.Address, slot common.Hash) (bool, bool) {
	addressOk, slotOk := l.inner.SlotInAccessList(addr, slot)
	l.logger.Debugf("[EthStateDB] SlotInAccessList: addr=%s, slot=%s -> addressOk=%t, slotOk=%t",
		addr.Hex(), slot.Hex(), addressOk, slotOk)
	return addressOk, slotOk
}

func (l *EthStateDBLogger) AddAddressToAccessList(addr common.Address) {
	l.logger.Debugf("[EthStateDB] AddAddressToAccessList: addr=%s", addr.Hex())
	l.inner.AddAddressToAccessList(addr)
}

func (l *EthStateDBLogger) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	l.logger.Debugf("[EthStateDB] AddSlotToAccessList: addr=%s, slot=%s", addr.Hex(), slot.Hex())
	l.inner.AddSlotToAccessList(addr, slot)
}

func (l *EthStateDBLogger) PointCache() *utils.PointCache {
	l.logger.Debugf("[EthStateDB] PointCache")
	return l.inner.PointCache()
}

func (l *EthStateDBLogger) Prepare(rules params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses types.AccessList) {
	destStr := "nil"
	if dest != nil {
		destStr = dest.Hex()
	}
	l.logger.Debugf("[EthStateDB] Prepare: sender=%s, coinbase=%s, dest=%s, precompiles len=%d, txAccesses len=%d",
		sender.Hex(), coinbase.Hex(), destStr, len(precompiles), len(txAccesses))
	l.inner.Prepare(rules, sender, coinbase, dest, precompiles, txAccesses)
}

func (l *EthStateDBLogger) RevertToSnapshot(snapshot int) {
	l.logger.Debugf("[EthStateDB] RevertToSnapshot: snapshot=%d", snapshot)
	l.inner.RevertToSnapshot(snapshot)
}

func (l *EthStateDBLogger) Snapshot() int {
	result := l.inner.Snapshot()
	l.logger.Debugf("[EthStateDB] Snapshot -> %d", result)
	return result
}

func (l *EthStateDBLogger) AddLog(logEntry *types.Log) {
	l.logger.Debugf("[EthStateDB] AddLog: log=%+v", logEntry)
	l.inner.AddLog(logEntry)
}

func (l *EthStateDBLogger) AddPreimage(hash common.Hash, preimage []byte) {
	l.logger.Debugf("[EthStateDB] AddPreimage: hash=%s, preimage len=%d", hash.Hex(), len(preimage))
	l.inner.AddPreimage(hash, preimage)
}

func (l *EthStateDBLogger) Witness() *stateless.Witness {
	l.logger.Debugf("[EthStateDB] Witness")
	return l.inner.Witness()
}

func (l *EthStateDBLogger) AccessEvents() *ethstate.AccessEvents {
	l.logger.Debugf("[EthStateDB] AccessEvents")
	return l.inner.AccessEvents()
}

func (l *EthStateDBLogger) Finalise(deleteEmptyObjects bool) {
	l.logger.Debugf("[EthStateDB] Finalise: deleteEmptyObjects=%t", deleteEmptyObjects)
	l.inner.Finalise(deleteEmptyObjects)
}

// Logs returns the logs from the inner StateDB
func (l *EthStateDBLogger) Logs() []*types.Log {
	return l.inner.Logs()
}

// Commit commits the state changes
func (l *EthStateDBLogger) Commit(block uint64, deleteEmptyObjects bool, cancun bool) (common.Hash, error) {
	l.logger.Debugf("[EthStateDB] Commit: block=%d, deleteEmptyObjects=%t, cancun=%t", block, deleteEmptyObjects, cancun)
	root, err := l.inner.Commit(block, deleteEmptyObjects, cancun)
	l.logger.Debugf("[EthStateDB] Commit -> root=%s, err=%v", root.Hex(), err)
	return root, err
}

// Ensure EthStateDBLogger implements vm.StateDB
var _ vm.StateDB = (*EthStateDBLogger)(nil)
