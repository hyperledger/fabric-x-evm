/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package endorser

import (
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	ethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie/utils"
	"github.com/holiman/uint256"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// getGoroutineID returns the current goroutine ID
func getGoroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	idField := strings.Fields(strings.TrimPrefix(string(buf[:n]), "goroutine "))[0]
	id, _ := strconv.ParseUint(idField, 10, 64)
	return id
}

// StateDBLogger wraps a StateDB and logs all method calls.
// It includes a mutex to serialize all calls, making it easier to debug
// concurrent access patterns and potential race conditions.
type StateDBLogger struct {
	inner  ExtendedStateDB
	logger *flogging.FabricLogger
	mu     sync.Mutex
}

// NewStateDBLogger creates a new logging wrapper
func NewStateDBLogger(inner ExtendedStateDB) ExtendedStateDB {
	return &StateDBLogger{
		inner:  inner,
		logger: flogging.MustGetLogger("StateDB"),
	}
}

func (l *StateDBLogger) CreateAccount(addr common.Address) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] CreateAccount: addr=%s", gid, addr.Hex())
	l.inner.CreateAccount(addr)
	l.logger.Debugf("[G%d] CreateAccount: completed", gid)
}

func (l *StateDBLogger) CreateContract(addr common.Address) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] CreateContract: addr=%s", gid, addr.Hex())
	l.inner.CreateContract(addr)
	l.logger.Debugf("[G%d] CreateContract: completed", gid)
}

func (l *StateDBLogger) SubBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] SubBalance: addr=%s, amount=%s, reason=%v", gid, addr.Hex(), amount.String(), reason)
	prev := l.inner.SubBalance(addr, amount, reason)
	l.logger.Debugf("[G%d] SubBalance: output prev=%s", gid, prev.String())
	return prev
}

func (l *StateDBLogger) AddBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] AddBalance: addr=%s, amount=%s, reason=%v", gid, addr.Hex(), amount.String(), reason)
	prev := l.inner.AddBalance(addr, amount, reason)
	l.logger.Debugf("[G%d] AddBalance: output prev=%s", gid, prev.String())
	return prev
}

func (l *StateDBLogger) GetBalance(addr common.Address) *uint256.Int {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] GetBalance: addr=%s", gid, addr.Hex())
	result := l.inner.GetBalance(addr)
	l.logger.Debugf("[G%d] GetBalance: output result=%s", gid, result.String())
	return result
}

func (l *StateDBLogger) GetNonce(addr common.Address) uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] GetNonce: addr=%s", gid, addr.Hex())
	result := l.inner.GetNonce(addr)
	l.logger.Debugf("[G%d] GetNonce: output result=%d", gid, result)
	return result
}

func (l *StateDBLogger) SetNonce(addr common.Address, nonce uint64, reason tracing.NonceChangeReason) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] SetNonce: addr=%s, nonce=%d, reason=%v", gid, addr.Hex(), nonce, reason)
	l.inner.SetNonce(addr, nonce, reason)
	l.logger.Debugf("[G%d] SetNonce: completed", gid)
}

func (l *StateDBLogger) GetCodeHash(addr common.Address) common.Hash {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] GetCodeHash: addr=%s", gid, addr.Hex())
	result := l.inner.GetCodeHash(addr)
	l.logger.Debugf("[G%d] GetCodeHash: output result=%s", gid, result.Hex())
	return result
}

func (l *StateDBLogger) GetCode(addr common.Address) []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] GetCode: addr=%s", gid, addr.Hex())
	result := l.inner.GetCode(addr)
	l.logger.Debugf("[G%d] GetCode: output result len=%d", gid, len(result))
	return result
}

func (l *StateDBLogger) SetCode(addr common.Address, code []byte, reason tracing.CodeChangeReason) []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] SetCode: addr=%s, code len=%d, reason=%v", gid, addr.Hex(), len(code), reason)
	prev := l.inner.SetCode(addr, code, reason)
	l.logger.Debugf("[G%d] SetCode: output prev len=%d", gid, len(prev))
	return prev
}

func (l *StateDBLogger) GetCodeSize(addr common.Address) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] GetCodeSize: addr=%s", gid, addr.Hex())
	result := l.inner.GetCodeSize(addr)
	l.logger.Debugf("[G%d] GetCodeSize: output result=%d", gid, result)
	return result
}

func (l *StateDBLogger) AddRefund(gas uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] AddRefund: gas=%d", gid, gas)
	l.inner.AddRefund(gas)
	l.logger.Debugf("[G%d] AddRefund: completed", gid)
}

func (l *StateDBLogger) SubRefund(gas uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] SubRefund: gas=%d", gid, gas)
	l.inner.SubRefund(gas)
	l.logger.Debugf("[G%d] SubRefund: completed", gid)
}

func (l *StateDBLogger) GetRefund() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] GetRefund", gid)
	result := l.inner.GetRefund()
	l.logger.Debugf("[G%d] GetRefund: output result=%d", gid, result)
	return result
}

func (l *StateDBLogger) GetStateAndCommittedState(addr common.Address, hash common.Hash) (common.Hash, common.Hash) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] GetStateAndCommittedState: addr=%s, hash=%s", gid, addr.Hex(), hash.Hex())
	current, committed := l.inner.GetStateAndCommittedState(addr, hash)
	l.logger.Debugf("[G%d] GetStateAndCommittedState: ethCurrent=%s, ethCommitted=%s", gid, current.Hex(), committed.Hex())
	return current, committed
}

func (l *StateDBLogger) GetState(addr common.Address, hash common.Hash) common.Hash {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] GetState: addr=%s, hash=%s", gid, addr.Hex(), hash.Hex())
	result := l.inner.GetState(addr, hash)
	l.logger.Debugf("[G%d] GetState: ethResult=%s, snapResult=%s", gid, result.Hex(), result.Hex())
	return result
}

func (l *StateDBLogger) SetState(addr common.Address, key common.Hash, value common.Hash) common.Hash {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] SetState: addr=%s, key=%s, value=%s", gid, addr.Hex(), key.Hex(), value.Hex())
	prev := l.inner.SetState(addr, key, value)
	l.logger.Debugf("[G%d] SetState: output prev=%s", gid, prev.Hex())
	return prev
}

func (l *StateDBLogger) GetStorageRoot(addr common.Address) common.Hash {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] GetStorageRoot: addr=%s", gid, addr.Hex())
	result := l.inner.GetStorageRoot(addr)
	l.logger.Debugf("[G%d] GetStorageRoot: output result=%s", gid, result.Hex())
	return result
}

func (l *StateDBLogger) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] GetTransientState: addr=%s, key=%s", gid, addr.Hex(), key.Hex())
	result := l.inner.GetTransientState(addr, key)
	l.logger.Debugf("[G%d] GetTransientState: output result=%s", gid, result.Hex())
	return result
}

func (l *StateDBLogger) SetTransientState(addr common.Address, key, value common.Hash) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] SetTransientState: addr=%s, key=%s, value=%s", gid, addr.Hex(), key.Hex(), value.Hex())
	l.inner.SetTransientState(addr, key, value)
	l.logger.Debugf("[G%d] SetTransientState: completed", gid)
}

func (l *StateDBLogger) SelfDestruct(addr common.Address) uint256.Int {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] SelfDestruct: addr=%s", gid, addr.Hex())
	balance := l.inner.SelfDestruct(addr)
	l.logger.Debugf("[G%d] SelfDestruct: output balance=%s", gid, balance.String())
	return balance
}

func (l *StateDBLogger) HasSelfDestructed(addr common.Address) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] HasSelfDestructed: addr=%s", gid, addr.Hex())
	result := l.inner.HasSelfDestructed(addr)
	l.logger.Debugf("[G%d] HasSelfDestructed: output result=%t", gid, result)
	return result
}

func (l *StateDBLogger) SelfDestruct6780(addr common.Address) (uint256.Int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] SelfDestruct6780: addr=%s", gid, addr.Hex())
	balance, destructed := l.inner.SelfDestruct6780(addr)
	l.logger.Debugf("[G%d] SelfDestruct6780: output balance=%s, destructed=%t", gid, balance.String(), destructed)
	return balance, destructed
}

func (l *StateDBLogger) Exist(addr common.Address) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] Exist: addr=%s", gid, addr.Hex())
	result := l.inner.Exist(addr)
	l.logger.Debugf("[G%d] Exist: output result=%t", gid, result)
	return result
}

func (l *StateDBLogger) Empty(addr common.Address) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] Empty: addr=%s", gid, addr.Hex())
	result := l.inner.Empty(addr)
	l.logger.Debugf("[G%d] Empty: output result=%t", gid, result)
	return result
}

func (l *StateDBLogger) AddressInAccessList(addr common.Address) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] AddressInAccessList: addr=%s", gid, addr.Hex())
	result := l.inner.AddressInAccessList(addr)
	l.logger.Debugf("[G%d] AddressInAccessList: output result=%t", gid, result)
	return result
}

func (l *StateDBLogger) SlotInAccessList(addr common.Address, slot common.Hash) (bool, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] SlotInAccessList: addr=%s, slot=%s", gid, addr.Hex(), slot.Hex())
	addressOk, slotOk := l.inner.SlotInAccessList(addr, slot)
	l.logger.Debugf("[G%d] SlotInAccessList: output addressOk=%t, slotOk=%t", gid, addressOk, slotOk)
	return addressOk, slotOk
}

func (l *StateDBLogger) AddAddressToAccessList(addr common.Address) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] AddAddressToAccessList: addr=%s", gid, addr.Hex())
	l.inner.AddAddressToAccessList(addr)
	l.logger.Debugf("[G%d] AddAddressToAccessList: completed", gid)
}

func (l *StateDBLogger) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] AddSlotToAccessList: addr=%s, slot=%s", gid, addr.Hex(), slot.Hex())
	l.inner.AddSlotToAccessList(addr, slot)
	l.logger.Debugf("[G%d] AddSlotToAccessList: completed", gid)
}

func (l *StateDBLogger) PointCache() *utils.PointCache {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] PointCache", gid)
	result := l.inner.PointCache()
	l.logger.Debugf("[G%d] PointCache: output result=%p", gid, result)
	return result
}

func (l *StateDBLogger) Prepare(rules params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses types.AccessList) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	destStr := "nil"
	if dest != nil {
		destStr = dest.Hex()
	}
	l.logger.Debugf("[G%d] Prepare: sender=%s, coinbase=%s, dest=%s, precompiles len=%d, txAccesses len=%d",
		gid, sender.Hex(), coinbase.Hex(), destStr, len(precompiles), len(txAccesses))
	l.inner.Prepare(rules, sender, coinbase, dest, precompiles, txAccesses)
	l.logger.Debugf("[G%d] Prepare: completed", gid)
}

func (l *StateDBLogger) RevertToSnapshot(snapshot int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] RevertToSnapshot: snapshot=%d", gid, snapshot)
	l.inner.RevertToSnapshot(snapshot)
	l.logger.Debugf("[G%d] RevertToSnapshot: completed", gid)
}

func (l *StateDBLogger) Snapshot() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] Snapshot: called", gid)
	result := l.inner.Snapshot()
	l.logger.Debugf("[G%d] Snapshot: output result=%d", gid, result)
	return result
}

func (l *StateDBLogger) AddLog(logEntry *types.Log) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] AddLog: log=%+v", gid, logEntry)
	l.inner.AddLog(logEntry)
	l.logger.Debugf("[G%d] AddLog: completed", gid)
}

func (l *StateDBLogger) AddPreimage(hash common.Hash, preimage []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] AddPreimage: hash=%s, preimage len=%d", gid, hash.Hex(), len(preimage))
	l.inner.AddPreimage(hash, preimage)
	l.logger.Debugf("[G%d] AddPreimage: completed", gid)
}

func (l *StateDBLogger) Witness() *stateless.Witness {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] Witness", gid)
	result := l.inner.Witness()
	l.logger.Debugf("[G%d] Witness: output result=%p", gid, result)
	return result
}

func (l *StateDBLogger) AccessEvents() *ethstate.AccessEvents {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] AccessEvents", gid)
	result := l.inner.AccessEvents()
	l.logger.Debugf("[G%d] AccessEvents: output result=%p", gid, result)
	return result
}

func (l *StateDBLogger) Finalise(deleteEmptyObjects bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] Finalise: deleteEmptyObjects=%t", gid, deleteEmptyObjects)
	l.inner.Finalise(deleteEmptyObjects)
	l.logger.Debugf("[G%d] Finalise: completed", gid)
}

// Result returns the read-write set from the inner StateDB
func (l *StateDBLogger) Result() blocks.ReadWriteSet {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] Result", gid)
	result := l.inner.Result()
	l.logger.Debugf("[G%d] Result: output reads len=%d, writes len=%d", gid, len(result.Reads), len(result.Writes))
	return result
}

// Logs returns the logs from the inner StateDB
func (l *StateDBLogger) Logs() []Log {
	l.mu.Lock()
	defer l.mu.Unlock()
	gid := getGoroutineID()
	l.logger.Debugf("[G%d] Logs", gid)
	result := l.inner.Logs()
	l.logger.Debugf("[G%d] Logs: output result len=%d", gid, len(result))
	return result
}

// Ensure StateDBLogger implements ExtendedStateDB
var _ ExtendedStateDB = (*StateDBLogger)(nil)
