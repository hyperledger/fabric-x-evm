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
	"github.com/hyperledger/fabric-lib-go/common/flogging"
)

// EthStateDBLogger wraps an Ethereum StateDB and logs all method calls
type EthStateDBLogger struct {
	inner  *ethstate.StateDB
	logger *flogging.FabricLogger
}

// NewEthStateDBLogger creates a new logging wrapper
func NewEthStateDBLogger(inner *ethstate.StateDB) *EthStateDBLogger {
	return &EthStateDBLogger{
		inner:  inner,
		logger: flogging.MustGetLogger("EthStateDB"),
	}
}

func (l *EthStateDBLogger) CreateAccount(addr common.Address) {
	l.logger.Debugf("CreateAccount: addr=%s", addr.Hex())
	l.inner.CreateAccount(addr)
	l.logger.Debugf("CreateAccount: completed")
}

func (l *EthStateDBLogger) CreateContract(addr common.Address) {
	l.logger.Debugf("CreateContract: addr=%s", addr.Hex())
	l.inner.CreateContract(addr)
	l.logger.Debugf("CreateContract: completed")
}

func (l *EthStateDBLogger) SubBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	l.logger.Debugf("SubBalance: addr=%s, amount=%s, reason=%v", addr.Hex(), amount.String(), reason)
	prev := l.inner.SubBalance(addr, amount, reason)
	l.logger.Debugf("SubBalance: output prev=%s", prev.String())
	return prev
}

func (l *EthStateDBLogger) AddBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	l.logger.Debugf("AddBalance: addr=%s, amount=%s, reason=%v", addr.Hex(), amount.String(), reason)
	prev := l.inner.AddBalance(addr, amount, reason)
	l.logger.Debugf("AddBalance: output prev=%s", prev.String())
	return prev
}

func (l *EthStateDBLogger) GetBalance(addr common.Address) *uint256.Int {
	l.logger.Debugf("GetBalance: addr=%s", addr.Hex())
	result := l.inner.GetBalance(addr)
	l.logger.Debugf("GetBalance: output result=%s", result.String())
	return result
}

func (l *EthStateDBLogger) GetNonce(addr common.Address) uint64 {
	l.logger.Debugf("GetNonce: addr=%s", addr.Hex())
	result := l.inner.GetNonce(addr)
	l.logger.Debugf("GetNonce: output result=%d", result)
	return result
}

func (l *EthStateDBLogger) SetNonce(addr common.Address, nonce uint64, reason tracing.NonceChangeReason) {
	l.logger.Debugf("SetNonce: addr=%s, nonce=%d, reason=%v", addr.Hex(), nonce, reason)
	l.inner.SetNonce(addr, nonce, reason)
	l.logger.Debugf("SetNonce: completed")
}

func (l *EthStateDBLogger) GetCodeHash(addr common.Address) common.Hash {
	l.logger.Debugf("GetCodeHash: addr=%s", addr.Hex())
	result := l.inner.GetCodeHash(addr)
	l.logger.Debugf("GetCodeHash: output result=%s", result.Hex())
	return result
}

func (l *EthStateDBLogger) GetCode(addr common.Address) []byte {
	l.logger.Debugf("GetCode: addr=%s", addr.Hex())
	result := l.inner.GetCode(addr)
	l.logger.Debugf("GetCode: output result len=%d", len(result))
	return result
}

func (l *EthStateDBLogger) SetCode(addr common.Address, code []byte, reason tracing.CodeChangeReason) []byte {
	l.logger.Debugf("SetCode: addr=%s, code len=%d, reason=%v", addr.Hex(), len(code), reason)
	prev := l.inner.SetCode(addr, code, reason)
	l.logger.Debugf("SetCode: output prev len=%d", len(prev))
	return prev
}

func (l *EthStateDBLogger) GetCodeSize(addr common.Address) int {
	l.logger.Debugf("GetCodeSize: addr=%s", addr.Hex())
	result := l.inner.GetCodeSize(addr)
	l.logger.Debugf("GetCodeSize: output result=%d", result)
	return result
}

func (l *EthStateDBLogger) AddRefund(gas uint64) {
	l.logger.Debugf("AddRefund: gas=%d", gas)
	l.inner.AddRefund(gas)
	l.logger.Debugf("AddRefund: completed")
}

func (l *EthStateDBLogger) SubRefund(gas uint64) {
	l.logger.Debugf("SubRefund: gas=%d", gas)
	l.inner.SubRefund(gas)
	l.logger.Debugf("SubRefund: completed")
}

func (l *EthStateDBLogger) GetRefund() uint64 {
	l.logger.Debugf("GetRefund")
	result := l.inner.GetRefund()
	l.logger.Debugf("GetRefund: output result=%d", result)
	return result
}

func (l *EthStateDBLogger) GetStateAndCommittedState(addr common.Address, hash common.Hash) (common.Hash, common.Hash) {
	l.logger.Debugf("GetStateAndCommittedState: addr=%s, hash=%s", addr.Hex(), hash.Hex())
	current, committed := l.inner.GetStateAndCommittedState(addr, hash)
	l.logger.Debugf("GetStateAndCommittedState: ethCurrent=%s, ethCommitted=%s", current.Hex(), committed.Hex())
	return current, committed
}

func (l *EthStateDBLogger) GetState(addr common.Address, hash common.Hash) common.Hash {
	l.logger.Debugf("GetState: addr=%s, hash=%s", addr.Hex(), hash.Hex())
	result := l.inner.GetState(addr, hash)
	l.logger.Debugf("GetState: ethResult=%s, snapResult=%s", result.Hex(), result.Hex())
	return result
}

func (l *EthStateDBLogger) SetState(addr common.Address, key common.Hash, value common.Hash) common.Hash {
	l.logger.Debugf("SetState: addr=%s, key=%s, value=%s", addr.Hex(), key.Hex(), value.Hex())
	prev := l.inner.SetState(addr, key, value)
	l.logger.Debugf("SetState: output prev=%s", prev.Hex())
	return prev
}

func (l *EthStateDBLogger) GetStorageRoot(addr common.Address) common.Hash {
	l.logger.Debugf("GetStorageRoot: addr=%s", addr.Hex())
	result := l.inner.GetStorageRoot(addr)
	l.logger.Debugf("GetStorageRoot: output result=%s", result.Hex())
	return result
}

func (l *EthStateDBLogger) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	l.logger.Debugf("GetTransientState: addr=%s, key=%s", addr.Hex(), key.Hex())
	result := l.inner.GetTransientState(addr, key)
	l.logger.Debugf("GetTransientState: output result=%s", result.Hex())
	return result
}

func (l *EthStateDBLogger) SetTransientState(addr common.Address, key, value common.Hash) {
	l.logger.Debugf("SetTransientState: addr=%s, key=%s, value=%s", addr.Hex(), key.Hex(), value.Hex())
	l.inner.SetTransientState(addr, key, value)
	l.logger.Debugf("SetTransientState: completed")
}

func (l *EthStateDBLogger) SelfDestruct(addr common.Address) uint256.Int {
	l.logger.Debugf("SelfDestruct: addr=%s", addr.Hex())
	balance := l.inner.SelfDestruct(addr)
	l.logger.Debugf("SelfDestruct: output balance=%s", balance.String())
	return balance
}

func (l *EthStateDBLogger) HasSelfDestructed(addr common.Address) bool {
	l.logger.Debugf("HasSelfDestructed: addr=%s", addr.Hex())
	result := l.inner.HasSelfDestructed(addr)
	l.logger.Debugf("HasSelfDestructed: output result=%t", result)
	return result
}

func (l *EthStateDBLogger) SelfDestruct6780(addr common.Address) (uint256.Int, bool) {
	l.logger.Debugf("SelfDestruct6780: addr=%s", addr.Hex())
	balance, destructed := l.inner.SelfDestruct6780(addr)
	l.logger.Debugf("SelfDestruct6780: output balance=%s, destructed=%t", balance.String(), destructed)
	return balance, destructed
}

func (l *EthStateDBLogger) Exist(addr common.Address) bool {
	l.logger.Debugf("Exist: addr=%s", addr.Hex())
	result := l.inner.Exist(addr)
	l.logger.Debugf("Exist: output result=%t", result)
	return result
}

func (l *EthStateDBLogger) Empty(addr common.Address) bool {
	l.logger.Debugf("Empty: addr=%s", addr.Hex())
	result := l.inner.Empty(addr)
	l.logger.Debugf("Empty: output result=%t", result)
	return result
}

func (l *EthStateDBLogger) AddressInAccessList(addr common.Address) bool {
	l.logger.Debugf("AddressInAccessList: addr=%s", addr.Hex())
	result := l.inner.AddressInAccessList(addr)
	l.logger.Debugf("AddressInAccessList: output result=%t", result)
	return result
}

func (l *EthStateDBLogger) SlotInAccessList(addr common.Address, slot common.Hash) (bool, bool) {
	l.logger.Debugf("SlotInAccessList: addr=%s, slot=%s", addr.Hex(), slot.Hex())
	addressOk, slotOk := l.inner.SlotInAccessList(addr, slot)
	l.logger.Debugf("SlotInAccessList: output addressOk=%t, slotOk=%t", addressOk, slotOk)
	return addressOk, slotOk
}

func (l *EthStateDBLogger) AddAddressToAccessList(addr common.Address) {
	l.logger.Debugf("AddAddressToAccessList: addr=%s", addr.Hex())
	l.inner.AddAddressToAccessList(addr)
	l.logger.Debugf("AddAddressToAccessList: completed")
}

func (l *EthStateDBLogger) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	l.logger.Debugf("AddSlotToAccessList: addr=%s, slot=%s", addr.Hex(), slot.Hex())
	l.inner.AddSlotToAccessList(addr, slot)
	l.logger.Debugf("AddSlotToAccessList: completed")
}

func (l *EthStateDBLogger) PointCache() *utils.PointCache {
	l.logger.Debugf("PointCache")
	result := l.inner.PointCache()
	l.logger.Debugf("PointCache: output result=%p", result)
	return result
}

func (l *EthStateDBLogger) Prepare(rules params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses types.AccessList) {
	destStr := "nil"
	if dest != nil {
		destStr = dest.Hex()
	}
	l.logger.Debugf("Prepare: sender=%s, coinbase=%s, dest=%s, precompiles len=%d, txAccesses len=%d",
		sender.Hex(), coinbase.Hex(), destStr, len(precompiles), len(txAccesses))
	l.inner.Prepare(rules, sender, coinbase, dest, precompiles, txAccesses)
	l.logger.Debugf("Prepare: completed")
}

func (l *EthStateDBLogger) RevertToSnapshot(snapshot int) {
	l.logger.Debugf("RevertToSnapshot: snapshot=%d", snapshot)
	l.inner.RevertToSnapshot(snapshot)
	l.logger.Debugf("RevertToSnapshot: completed")
}

func (l *EthStateDBLogger) Snapshot() int {
	l.logger.Debugf("Snapshot: called")
	result := l.inner.Snapshot()
	l.logger.Debugf("Snapshot: output result=%d", result)
	return result
}

func (l *EthStateDBLogger) AddLog(logEntry *types.Log) {
	l.logger.Debugf("AddLog: log=%+v", logEntry)
	l.inner.AddLog(logEntry)
	l.logger.Debugf("AddLog: completed")
}

func (l *EthStateDBLogger) AddPreimage(hash common.Hash, preimage []byte) {
	l.logger.Debugf("AddPreimage: hash=%s, preimage len=%d", hash.Hex(), len(preimage))
	l.inner.AddPreimage(hash, preimage)
	l.logger.Debugf("AddPreimage: completed")
}

func (l *EthStateDBLogger) Witness() *stateless.Witness {
	l.logger.Debugf("Witness")
	result := l.inner.Witness()
	l.logger.Debugf("Witness: output result=%p", result)
	return result
}

func (l *EthStateDBLogger) AccessEvents() *ethstate.AccessEvents {
	l.logger.Debugf("AccessEvents")
	result := l.inner.AccessEvents()
	l.logger.Debugf("AccessEvents: output result=%p", result)
	return result
}

func (l *EthStateDBLogger) Finalise(deleteEmptyObjects bool) {
	l.logger.Debugf("Finalise: deleteEmptyObjects=%t", deleteEmptyObjects)
	l.inner.Finalise(deleteEmptyObjects)
	l.logger.Debugf("Finalise: completed")
}

// Logs returns the logs from the inner StateDB
func (l *EthStateDBLogger) Logs() []*types.Log {
	l.logger.Debugf("Logs")
	result := l.inner.Logs()
	l.logger.Debugf("Logs: output result len=%d", len(result))
	return result
}

// Commit commits the state changes
func (l *EthStateDBLogger) Commit(block uint64, deleteEmptyObjects bool, cancun bool) (common.Hash, error) {
	l.logger.Debugf("Commit: block=%d, deleteEmptyObjects=%t, cancun=%t", block, deleteEmptyObjects, cancun)
	root, err := l.inner.Commit(block, deleteEmptyObjects, cancun)
	l.logger.Debugf("Commit: output root=%s, err=%v", root.Hex(), err)
	return root, err
}

// Ensure EthStateDBLogger implements vm.StateDB
var _ vm.StateDB = (*EthStateDBLogger)(nil)
