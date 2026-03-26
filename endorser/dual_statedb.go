/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package endorser

import (
	"log"

	"github.com/ethereum/go-ethereum/common"
	ethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/stateless"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie/utils"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
	"github.com/hyperledger/fabric-x-sdk/blocks"
	"github.com/hyperledger/fabric-x-sdk/state"
)

// ExtendedStateDB extends vm.StateDB with additional methods specific to
// the endorser implementation (Result, Logs, Ops).
type ExtendedStateDB interface {
	vm.StateDB
	Result() blocks.ReadWriteSet
	Logs() []state.Log
	Ops() []StateOp
}

// DualStateDB implements the ExtendedStateDB interface by delegating all calls
// to both a go-ethereum StateDB and an endorser SnapshotDB.
// This allows both state implementations to be kept in sync during execution.
type DualStateDB struct {
	ethStateDB *ethstate.StateDB
	snapshotDB *SnapshotDB
}

// NewDualStateDB creates a new DualStateDB that wraps both state implementations.
// The constructor takes concrete types (not interfaces) so that callers can
// access non-interface methods on both implementations.
func NewDualStateDB(ethStateDB *ethstate.StateDB, snapshotDB *SnapshotDB) *DualStateDB {
	log.Printf("[DualStateDB] NewDualStateDB: input ethStateDB=%p, snapshotDB=%p", ethStateDB, snapshotDB)
	result := &DualStateDB{
		ethStateDB: ethStateDB,
		snapshotDB: snapshotDB,
	}
	log.Printf("[DualStateDB] NewDualStateDB: output result=%p", result)
	return result
}

// EthStateDB returns the underlying go-ethereum StateDB for accessing
// non-interface methods.
func (d *DualStateDB) EthStateDB() *ethstate.StateDB {
	log.Printf("[DualStateDB] EthStateDB")
	result := d.ethStateDB
	log.Printf("[DualStateDB] EthStateDB: output result=%p", result)
	return result
}

// SnapshotDB returns the underlying endorser SnapshotDB for accessing
// non-interface methods.
func (d *DualStateDB) SnapshotDB() *SnapshotDB {
	log.Printf("[DualStateDB] SnapshotDB")
	result := d.snapshotDB
	log.Printf("[DualStateDB] SnapshotDB: output result=%p", result)
	return result
}

// TrieDB returns the trie database from the underlying ethStateDB.
// This is useful for accessing the database for state root verification.
func (d *DualStateDB) TrieDB() *triedb.Database {
	log.Printf("[DualStateDB] TrieDB")
	result := d.ethStateDB.Database().TrieDB()
	log.Printf("[DualStateDB] TrieDB: output result=%p", result)
	return result
}

// CreateAccount creates an account in both state implementations.
func (d *DualStateDB) CreateAccount(addr common.Address) {
	log.Printf("[DualStateDB] CreateAccount: addr=%s", addr.Hex())
	d.ethStateDB.CreateAccount(addr)
	d.snapshotDB.CreateAccount(addr)
	log.Printf("[DualStateDB] CreateAccount: completed")
}

// CreateContract creates a contract account in both state implementations.
func (d *DualStateDB) CreateContract(addr common.Address) {
	log.Printf("[DualStateDB] CreateContract: addr=%s", addr.Hex())
	d.ethStateDB.CreateContract(addr)
	d.snapshotDB.CreateContract(addr)
	log.Printf("[DualStateDB] CreateContract: completed")
}

// SubBalance subtracts balance from an account in both state implementations.
// Returns the previous balance from the eth StateDB.
func (d *DualStateDB) SubBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	log.Printf("[DualStateDB] SubBalance: addr=%s, amount=%s, reason=%v", addr.Hex(), amount.String(), reason)
	prev := d.ethStateDB.SubBalance(addr, amount, reason)
	d.snapshotDB.SubBalance(addr, amount, reason)
	log.Printf("[DualStateDB] SubBalance: output prev=%s", prev.String())
	return prev
}

// AddBalance adds balance to an account in both state implementations.
// Returns the previous balance from the eth StateDB.
func (d *DualStateDB) AddBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	log.Printf("[DualStateDB] AddBalance: addr=%s, amount=%s, reason=%v", addr.Hex(), amount.String(), reason)
	prev := d.ethStateDB.AddBalance(addr, amount, reason)
	d.snapshotDB.AddBalance(addr, amount, reason)
	log.Printf("[DualStateDB] AddBalance: output prev=%s", prev.String())
	return prev
}

// GetBalance returns the balance from the SnapshotDB.
func (d *DualStateDB) GetBalance(addr common.Address) *uint256.Int {
	log.Printf("[DualStateDB] GetBalance: addr=%s", addr.Hex())
	result := d.snapshotDB.GetBalance(addr)
	log.Printf("[DualStateDB] GetBalance: output result=%s", result.String())
	return result
}

// GetNonce returns the nonce from the SnapshotDB.
func (d *DualStateDB) GetNonce(addr common.Address) uint64 {
	log.Printf("[DualStateDB] GetNonce: addr=%s", addr.Hex())
	result := d.snapshotDB.GetNonce(addr)
	log.Printf("[DualStateDB] GetNonce: output result=%d", result)
	return result
}

// SetNonce sets the nonce in both state implementations.
func (d *DualStateDB) SetNonce(addr common.Address, nonce uint64, reason tracing.NonceChangeReason) {
	log.Printf("[DualStateDB] SetNonce: addr=%s, nonce=%d, reason=%v", addr.Hex(), nonce, reason)
	d.ethStateDB.SetNonce(addr, nonce, reason)
	d.snapshotDB.SetNonce(addr, nonce, reason)
	log.Printf("[DualStateDB] SetNonce: completed")
}

// GetCodeHash returns the code hash from the SnapshotDB.
func (d *DualStateDB) GetCodeHash(addr common.Address) common.Hash {
	log.Printf("[DualStateDB] GetCodeHash: addr=%s", addr.Hex())
	result := d.snapshotDB.GetCodeHash(addr)
	log.Printf("[DualStateDB] GetCodeHash: output result=%s", result.Hex())
	return result
}

// GetCode returns the code from the SnapshotDB.
func (d *DualStateDB) GetCode(addr common.Address) []byte {
	log.Printf("[DualStateDB] GetCode: addr=%s", addr.Hex())
	result := d.snapshotDB.GetCode(addr)
	log.Printf("[DualStateDB] GetCode: output result len=%d", len(result))
	return result
}

// SetCode sets the code in both state implementations.
// Returns the previous code from the eth StateDB.
func (d *DualStateDB) SetCode(addr common.Address, code []byte, reason tracing.CodeChangeReason) []byte {
	log.Printf("[DualStateDB] SetCode: addr=%s, code len=%d, reason=%v", addr.Hex(), len(code), reason)
	prev := d.ethStateDB.SetCode(addr, code, reason)
	d.snapshotDB.SetCode(addr, code, reason)
	log.Printf("[DualStateDB] SetCode: output prev len=%d", len(prev))
	return prev
}

// GetCodeSize returns the code size from the SnapshotDB.
func (d *DualStateDB) GetCodeSize(addr common.Address) int {
	log.Printf("[DualStateDB] GetCodeSize: addr=%s", addr.Hex())
	result := d.snapshotDB.GetCodeSize(addr)
	log.Printf("[DualStateDB] GetCodeSize: output result=%d", result)
	return result
}

// AddRefund adds a gas refund in both state implementations.
func (d *DualStateDB) AddRefund(gas uint64) {
	log.Printf("[DualStateDB] AddRefund: gas=%d", gas)
	d.ethStateDB.AddRefund(gas)
	d.snapshotDB.AddRefund(gas)
	log.Printf("[DualStateDB] AddRefund: completed")
}

// SubRefund subtracts a gas refund in both state implementations.
func (d *DualStateDB) SubRefund(gas uint64) {
	log.Printf("[DualStateDB] SubRefund: gas=%d", gas)
	d.ethStateDB.SubRefund(gas)
	d.snapshotDB.SubRefund(gas)
	log.Printf("[DualStateDB] SubRefund: completed")
}

// GetRefund returns the refund counter from the SnapshotDB.
func (d *DualStateDB) GetRefund() uint64 {
	log.Printf("[DualStateDB] GetRefund")
	result := d.snapshotDB.GetRefund()
	log.Printf("[DualStateDB] GetRefund: output result=%d", result)
	return result
}

// GetStateAndCommittedState returns both current and committed state from the SnapshotDB.
func (d *DualStateDB) GetStateAndCommittedState(addr common.Address, hash common.Hash) (common.Hash, common.Hash) {
	log.Printf("[DualStateDB] GetStateAndCommittedState: addr=%s, hash=%s", addr.Hex(), hash.Hex())
	current, committed := d.snapshotDB.GetStateAndCommittedState(addr, hash)
	log.Printf("[DualStateDB] GetStateAndCommittedState: output current=%s, committed=%s", current.Hex(), committed.Hex())
	return current, committed
}

// GetState returns the state from the SnapshotDB.
func (d *DualStateDB) GetState(addr common.Address, hash common.Hash) common.Hash {
	log.Printf("[DualStateDB] GetState: addr=%s, hash=%s", addr.Hex(), hash.Hex())
	result := d.snapshotDB.GetState(addr, hash)
	log.Printf("[DualStateDB] GetState: output result=%s", result.Hex())
	return result
}

// SetState sets the state in both state implementations.
// Returns the previous state from the eth StateDB.
func (d *DualStateDB) SetState(addr common.Address, key common.Hash, value common.Hash) common.Hash {
	log.Printf("[DualStateDB] SetState: addr=%s, key=%s, value=%s", addr.Hex(), key.Hex(), value.Hex())
	prev := d.ethStateDB.SetState(addr, key, value)
	d.snapshotDB.SetState(addr, key, value)
	log.Printf("[DualStateDB] SetState: output prev=%s", prev.Hex())
	return prev
}

// GetStorageRoot returns the storage root from the SnapshotDB.
func (d *DualStateDB) GetStorageRoot(addr common.Address) common.Hash {
	log.Printf("[DualStateDB] GetStorageRoot: addr=%s", addr.Hex())
	result := d.snapshotDB.GetStorageRoot(addr)
	log.Printf("[DualStateDB] GetStorageRoot: output result=%s", result.Hex())
	return result
}

// GetTransientState returns the transient state from the SnapshotDB.
func (d *DualStateDB) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	log.Printf("[DualStateDB] GetTransientState: addr=%s, key=%s", addr.Hex(), key.Hex())
	result := d.snapshotDB.GetTransientState(addr, key)
	log.Printf("[DualStateDB] GetTransientState: output result=%s", result.Hex())
	return result
}

// SetTransientState sets the transient state in both state implementations.
func (d *DualStateDB) SetTransientState(addr common.Address, key, value common.Hash) {
	log.Printf("[DualStateDB] SetTransientState: addr=%s, key=%s, value=%s", addr.Hex(), key.Hex(), value.Hex())
	d.ethStateDB.SetTransientState(addr, key, value)
	d.snapshotDB.SetTransientState(addr, key, value)
	log.Printf("[DualStateDB] SetTransientState: completed")
}

// SelfDestruct performs self-destruct in both state implementations.
// Returns the balance from the eth StateDB.
func (d *DualStateDB) SelfDestruct(addr common.Address) uint256.Int {
	log.Printf("[DualStateDB] SelfDestruct: addr=%s", addr.Hex())
	balance := d.ethStateDB.SelfDestruct(addr)
	d.snapshotDB.SelfDestruct(addr)
	log.Printf("[DualStateDB] SelfDestruct: output balance=%s", balance.String())
	return balance
}

// HasSelfDestructed checks if an account has self-destructed in the SnapshotDB.
func (d *DualStateDB) HasSelfDestructed(addr common.Address) bool {
	log.Printf("[DualStateDB] HasSelfDestructed: addr=%s", addr.Hex())
	result := d.snapshotDB.HasSelfDestructed(addr)
	log.Printf("[DualStateDB] HasSelfDestructed: output result=%t", result)
	return result
}

// SelfDestruct6780 performs EIP-6780 self-destruct in both state implementations.
// Returns the balance and destruction status from the eth StateDB.
func (d *DualStateDB) SelfDestruct6780(addr common.Address) (uint256.Int, bool) {
	log.Printf("[DualStateDB] SelfDestruct6780: addr=%s", addr.Hex())
	balance, destructed := d.ethStateDB.SelfDestruct6780(addr)
	d.snapshotDB.SelfDestruct6780(addr)
	log.Printf("[DualStateDB] SelfDestruct6780: output balance=%s, destructed=%t", balance.String(), destructed)
	return balance, destructed
}

// Exist checks if an account exists in the SnapshotDB.
func (d *DualStateDB) Exist(addr common.Address) bool {
	log.Printf("[DualStateDB] Exist: addr=%s", addr.Hex())
	result := d.snapshotDB.Exist(addr)
	log.Printf("[DualStateDB] Exist: output result=%t", result)
	return result
}

// Empty checks if an account is empty in the SnapshotDB.
func (d *DualStateDB) Empty(addr common.Address) bool {
	log.Printf("[DualStateDB] Empty: addr=%s", addr.Hex())
	result := d.snapshotDB.Empty(addr)
	log.Printf("[DualStateDB] Empty: output result=%t", result)
	return result
}

// AddressInAccessList checks if an address is in the access list in the SnapshotDB.
func (d *DualStateDB) AddressInAccessList(addr common.Address) bool {
	log.Printf("[DualStateDB] AddressInAccessList: addr=%s", addr.Hex())
	result := d.snapshotDB.AddressInAccessList(addr)
	log.Printf("[DualStateDB] AddressInAccessList: output result=%t", result)
	return result
}

// SlotInAccessList checks if a slot is in the access list in the SnapshotDB.
func (d *DualStateDB) SlotInAccessList(addr common.Address, slot common.Hash) (addressOk bool, slotOk bool) {
	log.Printf("[DualStateDB] SlotInAccessList: addr=%s, slot=%s", addr.Hex(), slot.Hex())
	addressOk, slotOk = d.snapshotDB.SlotInAccessList(addr, slot)
	log.Printf("[DualStateDB] SlotInAccessList: output addressOk=%t, slotOk=%t", addressOk, slotOk)
	return addressOk, slotOk
}

// AddAddressToAccessList adds an address to the access list in both state implementations.
func (d *DualStateDB) AddAddressToAccessList(addr common.Address) {
	log.Printf("[DualStateDB] AddAddressToAccessList: addr=%s", addr.Hex())
	d.ethStateDB.AddAddressToAccessList(addr)
	d.snapshotDB.AddAddressToAccessList(addr)
	log.Printf("[DualStateDB] AddAddressToAccessList: completed")
}

// AddSlotToAccessList adds a slot to the access list in both state implementations.
func (d *DualStateDB) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	log.Printf("[DualStateDB] AddSlotToAccessList: addr=%s, slot=%s", addr.Hex(), slot.Hex())
	d.ethStateDB.AddSlotToAccessList(addr, slot)
	d.snapshotDB.AddSlotToAccessList(addr, slot)
	log.Printf("[DualStateDB] AddSlotToAccessList: completed")
}

// PointCache returns the point cache from the SnapshotDB.
func (d *DualStateDB) PointCache() *utils.PointCache {
	log.Printf("[DualStateDB] PointCache")
	result := d.snapshotDB.PointCache()
	log.Printf("[DualStateDB] PointCache: output result=%p", result)
	return result
}

// Prepare prepares both state implementations for transaction execution.
func (d *DualStateDB) Prepare(rules params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses types.AccessList) {
	destStr := "nil"
	if dest != nil {
		destStr = dest.Hex()
	}
	log.Printf("[DualStateDB] Prepare: sender=%s, coinbase=%s, dest=%s, precompiles len=%d, txAccesses len=%d",
		sender.Hex(), coinbase.Hex(), destStr, len(precompiles), len(txAccesses))
	d.ethStateDB.Prepare(rules, sender, coinbase, dest, precompiles, txAccesses)
	d.snapshotDB.Prepare(rules, sender, coinbase, dest, precompiles, txAccesses)
	log.Printf("[DualStateDB] Prepare: completed")
}

// RevertToSnapshot reverts to a snapshot in both state implementations.
func (d *DualStateDB) RevertToSnapshot(snapshot int) {
	log.Printf("[DualStateDB] RevertToSnapshot: snapshot=%d", snapshot)
	d.ethStateDB.RevertToSnapshot(snapshot)
	d.snapshotDB.RevertToSnapshot(snapshot)
	log.Printf("[DualStateDB] RevertToSnapshot: completed")
}

// Snapshot creates a snapshot in both state implementations.
// Returns the snapshot ID from the SnapshotDB.
func (d *DualStateDB) Snapshot() int {
	log.Printf("[DualStateDB] Snapshot: called")
	d.ethStateDB.Snapshot()
	snapSnapshot := d.snapshotDB.Snapshot()
	log.Printf("[DualStateDB] Snapshot: returning snapSnapshot=%d", snapSnapshot)
	return snapSnapshot
}

// AddLog adds a log to both state implementations.
func (d *DualStateDB) AddLog(logEntry *types.Log) {
	log.Printf("[DualStateDB] AddLog: log=%+v", logEntry)
	d.ethStateDB.AddLog(logEntry)
	d.snapshotDB.AddLog(logEntry)
	log.Printf("[DualStateDB] AddLog: completed")
}

// AddPreimage adds a preimage to both state implementations.
func (d *DualStateDB) AddPreimage(hash common.Hash, preimage []byte) {
	log.Printf("[DualStateDB] AddPreimage: hash=%s, preimage len=%d", hash.Hex(), len(preimage))
	d.ethStateDB.AddPreimage(hash, preimage)
	d.snapshotDB.AddPreimage(hash, preimage)
	log.Printf("[DualStateDB] AddPreimage: completed")
}

// Witness returns the witness from the SnapshotDB.
func (d *DualStateDB) Witness() *stateless.Witness {
	log.Printf("[DualStateDB] Witness: called")
	result := d.snapshotDB.Witness()
	log.Printf("[DualStateDB] Witness: returning result=%p", result)
	return result
}

// AccessEvents returns the access events from the SnapshotDB.
func (d *DualStateDB) AccessEvents() *ethstate.AccessEvents {
	log.Printf("[DualStateDB] AccessEvents: called")
	result := d.snapshotDB.AccessEvents()
	log.Printf("[DualStateDB] AccessEvents: returning result=%p", result)
	return result
}

// Finalise finalizes both state implementations.
func (d *DualStateDB) Finalise(deleteEmptyObjects bool) {
	log.Printf("[DualStateDB] Finalise: deleteEmptyObjects=%t", deleteEmptyObjects)
	d.ethStateDB.Finalise(deleteEmptyObjects)
	d.snapshotDB.Finalise(deleteEmptyObjects)
	log.Printf("[DualStateDB] Finalise: completed")
}

// Result returns the read-write set from the SnapshotDB.
// This is a SnapshotDB-specific method not part of vm.StateDB interface.
func (d *DualStateDB) Result() blocks.ReadWriteSet {
	log.Printf("[DualStateDB] Result: called")
	result := d.snapshotDB.Result()
	log.Printf("[DualStateDB] Result: returning result with %d reads, %d writes", len(result.Reads), len(result.Writes))
	return result
}

// Logs returns the logs from the SnapshotDB.
// This is a SnapshotDB-specific method not part of vm.StateDB interface.
func (d *DualStateDB) Logs() []state.Log {
	log.Printf("[DualStateDB] Logs: called")
	result := d.snapshotDB.Logs()
	log.Printf("[DualStateDB] Logs: returning result len=%d", len(result))
	return result
}

// Ops returns the recorded state operations from the SnapshotDB.
// This is a SnapshotDB-specific method not part of vm.StateDB interface.
func (d *DualStateDB) Ops() []StateOp {
	log.Printf("[DualStateDB] Ops: called")
	result := d.snapshotDB.Ops()
	log.Printf("[DualStateDB] Ops: returning result len=%d", len(result))
	return result
}

// Made with Bob
