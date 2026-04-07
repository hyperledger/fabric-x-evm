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
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// ExtendedStateDB extends vm.StateDB with additional methods specific to
// the endorser implementation (Result, Logs).
type ExtendedStateDB interface {
	vm.StateDB
	Result() blocks.ReadWriteSet
	Logs() []Log
}

// DualStateDB implements the ExtendedStateDB interface by delegating all calls
// to both a go-ethereum StateDB and an endorser StateDB.
// This allows both state implementations to be kept in sync during execution.
type DualStateDB struct {
	ethStateDB *ethstate.StateDB
	snapshotDB *StateDB
	logger     *flogging.FabricLogger
}

// NewDualStateDB creates a new DualStateDB that wraps both state implementations.
// The constructor takes concrete types (not interfaces) so that callers can
// access non-interface methods on both implementations.
func NewDualStateDB(ethStateDB *ethstate.StateDB, SnapshotDB *StateDB) *DualStateDB {
	logger := flogging.MustGetLogger("DualStateDB")
	logger.Debugf("NewDualStateDB: input ethStateDB=%p, SnapshotDB=%p", ethStateDB, SnapshotDB)
	result := &DualStateDB{
		ethStateDB: ethStateDB,
		snapshotDB: SnapshotDB,
		logger:     logger,
	}
	logger.Debugf("NewDualStateDB: output result=%p", result)
	return result
}

// EthStateDB returns the underlying go-ethereum StateDB for accessing
// non-interface methods.
func (d *DualStateDB) EthStateDB() *ethstate.StateDB {
	d.logger.Debugf("EthStateDB")
	result := d.ethStateDB
	d.logger.Debugf("EthStateDB: output result=%p", result)
	return result
}

// TrieDB returns the trie database from the underlying ethStateDB.
// This is useful for accessing the database for state root verification.
func (d *DualStateDB) TrieDB() *triedb.Database {
	d.logger.Debugf("TrieDB")
	result := d.ethStateDB.Database().TrieDB()
	d.logger.Debugf("TrieDB: output result=%p", result)
	return result
}

// CreateAccount creates an account in both state implementations.
func (d *DualStateDB) CreateAccount(addr common.Address) {
	d.logger.Debugf("CreateAccount: addr=%s", addr.Hex())
	d.ethStateDB.CreateAccount(addr)
	d.snapshotDB.CreateAccount(addr)
	d.logger.Debugf("CreateAccount: completed")
}

// CreateContract creates a contract account in both state implementations.
func (d *DualStateDB) CreateContract(addr common.Address) {
	d.logger.Debugf("CreateContract: addr=%s", addr.Hex())
	d.ethStateDB.CreateContract(addr)
	d.snapshotDB.CreateContract(addr)
	d.logger.Debugf("CreateContract: completed")
}

// SubBalance subtracts balance from an account in both state implementations.
// Returns the previous balance from the eth StateDB.
func (d *DualStateDB) SubBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	d.logger.Debugf("SubBalance: addr=%s, amount=%s, reason=%v", addr.Hex(), amount.String(), reason)
	prev := d.ethStateDB.SubBalance(addr, amount, reason)
	d.snapshotDB.SubBalance(addr, amount, reason)
	d.logger.Debugf("SubBalance: output prev=%s", prev.String())
	return prev
}

// AddBalance adds balance to an account in both state implementations.
// Returns the previous balance from the eth StateDB.
func (d *DualStateDB) AddBalance(addr common.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) uint256.Int {
	d.logger.Debugf("AddBalance: addr=%s, amount=%s, reason=%v", addr.Hex(), amount.String(), reason)
	prev := d.ethStateDB.AddBalance(addr, amount, reason)
	d.snapshotDB.AddBalance(addr, amount, reason)
	d.logger.Debugf("AddBalance: output prev=%s", prev.String())
	return prev
}

// GetBalance returns the balance from the SnapshotDB.
func (d *DualStateDB) GetBalance(addr common.Address) *uint256.Int {
	d.logger.Debugf("GetBalance: addr=%s", addr.Hex())
	result := d.snapshotDB.GetBalance(addr)
	d.logger.Debugf("GetBalance: output result=%s", result.String())
	return result
}

// GetNonce returns the nonce from the SnapshotDB.
func (d *DualStateDB) GetNonce(addr common.Address) uint64 {
	d.logger.Debugf("GetNonce: addr=%s", addr.Hex())
	result := d.snapshotDB.GetNonce(addr)
	d.logger.Debugf("GetNonce: output result=%d", result)
	return result
}

// SetNonce sets the nonce in both state implementations.
func (d *DualStateDB) SetNonce(addr common.Address, nonce uint64, reason tracing.NonceChangeReason) {
	d.logger.Debugf("SetNonce: addr=%s, nonce=%d, reason=%v", addr.Hex(), nonce, reason)
	d.ethStateDB.SetNonce(addr, nonce, reason)
	d.snapshotDB.SetNonce(addr, nonce, reason)
	d.logger.Debugf("SetNonce: completed")
}

// GetCodeHash returns the code hash from the SnapshotDB.
func (d *DualStateDB) GetCodeHash(addr common.Address) common.Hash {
	d.logger.Debugf("GetCodeHash: addr=%s", addr.Hex())
	result := d.snapshotDB.GetCodeHash(addr)
	d.logger.Debugf("GetCodeHash: output result=%s", result.Hex())
	return result
}

// GetCode returns the code from the SnapshotDB.
func (d *DualStateDB) GetCode(addr common.Address) []byte {
	d.logger.Debugf("GetCode: addr=%s", addr.Hex())
	result := d.snapshotDB.GetCode(addr)
	d.logger.Debugf("GetCode: output result len=%d", len(result))
	return result
}

// SetCode sets the code in both state implementations.
// Returns the previous code from the eth StateDB.
func (d *DualStateDB) SetCode(addr common.Address, code []byte, reason tracing.CodeChangeReason) []byte {
	d.logger.Debugf("SetCode: addr=%s, code len=%d, reason=%v", addr.Hex(), len(code), reason)
	prev := d.ethStateDB.SetCode(addr, code, reason)
	d.snapshotDB.SetCode(addr, code, reason)
	d.logger.Debugf("SetCode: output prev len=%d", len(prev))
	return prev
}

// GetCodeSize returns the code size from the SnapshotDB.
func (d *DualStateDB) GetCodeSize(addr common.Address) int {
	d.logger.Debugf("GetCodeSize: addr=%s", addr.Hex())
	result := d.snapshotDB.GetCodeSize(addr)
	d.logger.Debugf("GetCodeSize: output result=%d", result)
	return result
}

// AddRefund adds a gas refund in both state implementations.
func (d *DualStateDB) AddRefund(gas uint64) {
	d.logger.Debugf("AddRefund: gas=%d", gas)
	d.ethStateDB.AddRefund(gas)
	d.snapshotDB.AddRefund(gas)
	d.logger.Debugf("AddRefund: completed")
}

// SubRefund subtracts a gas refund in both state implementations.
func (d *DualStateDB) SubRefund(gas uint64) {
	d.logger.Debugf("SubRefund: gas=%d", gas)
	d.ethStateDB.SubRefund(gas)
	d.snapshotDB.SubRefund(gas)
	d.logger.Debugf("SubRefund: completed")
}

// GetRefund returns the refund counter from the SnapshotDB.
func (d *DualStateDB) GetRefund() uint64 {
	d.logger.Debugf("GetRefund")
	result := d.snapshotDB.GetRefund()
	d.logger.Debugf("GetRefund: output result=%d", result)
	return result
}

// GetStateAndCommittedState returns both current and committed state from the SnapshotDB.
func (d *DualStateDB) GetStateAndCommittedState(addr common.Address, hash common.Hash) (common.Hash, common.Hash) {
	d.logger.Debugf("GetStateAndCommittedState: addr=%s, hash=%s", addr.Hex(), hash.Hex())
	// Call both to verify they return the same data
	ethCurrent, ethCommitted := d.ethStateDB.GetStateAndCommittedState(addr, hash)
	snapCurrent, snapCommitted := d.snapshotDB.GetStateAndCommittedState(addr, hash)
	d.logger.Debugf("GetStateAndCommittedState: ethCurrent=%s, ethCommitted=%s", ethCurrent.Hex(), ethCommitted.Hex())
	if ethCurrent != snapCurrent || ethCommitted != snapCommitted {
		d.logger.Warn("GetStateAndCommittedState: MISMATCH DETECTED!")
		d.logger.Debugf("GetStateAndCommittedState: snapCurrent=%s, snapCommitted=%s", snapCurrent.Hex(), snapCommitted.Hex())
	}
	return snapCurrent, snapCommitted
}

// GetState returns the state from the SnapshotDB.
func (d *DualStateDB) GetState(addr common.Address, hash common.Hash) common.Hash {
	d.logger.Debugf("GetState: addr=%s, hash=%s", addr.Hex(), hash.Hex())
	ethResult := d.ethStateDB.GetState(addr, hash)
	snapResult := d.snapshotDB.GetState(addr, hash)
	d.logger.Debugf("GetState: ethResult=%s, snapResult=%s", ethResult.Hex(), snapResult.Hex())
	if ethResult != snapResult {
		d.logger.Warnf("GetState MISMATCH: eth=%s snap=%s addr=%s slot=%s", ethResult.Hex(), snapResult.Hex(), addr.Hex(), hash.Hex())
	}
	return snapResult
}

// SetState sets the state in both state implementations.
// Returns the previous state from the eth StateDB.
func (d *DualStateDB) SetState(addr common.Address, key common.Hash, value common.Hash) common.Hash {
	d.logger.Debugf("SetState: addr=%s, key=%s, value=%s", addr.Hex(), key.Hex(), value.Hex())
	prev := d.ethStateDB.SetState(addr, key, value)
	d.snapshotDB.SetState(addr, key, value)
	d.logger.Debugf("SetState: output prev=%s", prev.Hex())
	return prev
}

// GetStorageRoot returns the storage root from the SnapshotDB.
func (d *DualStateDB) GetStorageRoot(addr common.Address) common.Hash {
	d.logger.Debugf("GetStorageRoot: addr=%s", addr.Hex())
	// FIXME: use snapshotDB as soon as it supports this function
	result := d.ethStateDB.GetStorageRoot(addr)
	d.logger.Debugf("GetStorageRoot: output result=%s", result.Hex())
	return result
}

// GetTransientState returns the transient state from the SnapshotDB.
func (d *DualStateDB) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	d.logger.Debugf("GetTransientState: addr=%s, key=%s", addr.Hex(), key.Hex())
	result := d.snapshotDB.GetTransientState(addr, key)
	d.logger.Debugf("GetTransientState: output result=%s", result.Hex())
	return result
}

// SetTransientState sets the transient state in both state implementations.
func (d *DualStateDB) SetTransientState(addr common.Address, key, value common.Hash) {
	d.logger.Debugf("SetTransientState: addr=%s, key=%s, value=%s", addr.Hex(), key.Hex(), value.Hex())
	d.ethStateDB.SetTransientState(addr, key, value)
	d.snapshotDB.SetTransientState(addr, key, value)
	d.logger.Debugf("SetTransientState: completed")
}

// SelfDestruct performs self-destruct in both state implementations.
// Returns the balance from the eth StateDB.
func (d *DualStateDB) SelfDestruct(addr common.Address) uint256.Int {
	d.logger.Debugf("SelfDestruct: addr=%s", addr.Hex())
	balance := d.ethStateDB.SelfDestruct(addr)
	d.snapshotDB.SelfDestruct(addr)
	d.logger.Debugf("SelfDestruct: output balance=%s", balance.String())
	return balance
}

// HasSelfDestructed checks if an account has self-destructed in the SnapshotDB.
func (d *DualStateDB) HasSelfDestructed(addr common.Address) bool {
	d.logger.Debugf("HasSelfDestructed: addr=%s", addr.Hex())
	result := d.snapshotDB.HasSelfDestructed(addr)
	d.logger.Debugf("HasSelfDestructed: output result=%t", result)
	return result
}

// SelfDestruct6780 performs EIP-6780 self-destruct in both state implementations.
// Returns the balance and destruction status from the eth StateDB.
func (d *DualStateDB) SelfDestruct6780(addr common.Address) (uint256.Int, bool) {
	d.logger.Debugf("SelfDestruct6780: addr=%s", addr.Hex())
	balance, destructed := d.ethStateDB.SelfDestruct6780(addr)
	d.snapshotDB.SelfDestruct6780(addr)
	d.logger.Debugf("SelfDestruct6780: output balance=%s, destructed=%t", balance.String(), destructed)
	return balance, destructed
}

// Exist checks if an account exists in either the ethStateDB
func (d *DualStateDB) Exist(addr common.Address) bool {
	d.logger.Debugf("Exist: addr=%s", addr.Hex())
	ethExists := d.ethStateDB.Exist(addr)
	snapExists := d.snapshotDB.Exist(addr)
	if ethExists != snapExists {
		d.logger.Warnf("Exist MISMATCH: eth=%t snap=%t", ethExists, snapExists)
	}
	result := snapExists
	d.logger.Debugf("Exist: output result=%t", result)
	return result
}

// Empty checks if an account is empty in the SnapshotDB.
func (d *DualStateDB) Empty(addr common.Address) bool {
	d.logger.Debugf("Empty: addr=%s", addr.Hex())
	result := d.snapshotDB.Empty(addr)
	d.logger.Debugf("Empty: output result=%t", result)
	return result
}

// AddressInAccessList checks if an address is in the access list in the SnapshotDB.
func (d *DualStateDB) AddressInAccessList(addr common.Address) bool {
	d.logger.Debugf("AddressInAccessList: addr=%s", addr.Hex())
	result := d.snapshotDB.AddressInAccessList(addr)
	d.logger.Debugf("AddressInAccessList: output result=%t", result)
	return result
}

// SlotInAccessList checks if a slot is in the access list in the SnapshotDB.
func (d *DualStateDB) SlotInAccessList(addr common.Address, slot common.Hash) (addressOk bool, slotOk bool) {
	d.logger.Debugf("SlotInAccessList: addr=%s, slot=%s", addr.Hex(), slot.Hex())
	addressOk, slotOk = d.snapshotDB.SlotInAccessList(addr, slot)
	d.logger.Debugf("SlotInAccessList: output addressOk=%t, slotOk=%t", addressOk, slotOk)
	return addressOk, slotOk
}

// AddAddressToAccessList adds an address to the access list in both state implementations.
func (d *DualStateDB) AddAddressToAccessList(addr common.Address) {
	d.logger.Debugf("AddAddressToAccessList: addr=%s", addr.Hex())
	d.ethStateDB.AddAddressToAccessList(addr)
	d.snapshotDB.AddAddressToAccessList(addr)
	d.logger.Debugf("AddAddressToAccessList: completed")
}

// AddSlotToAccessList adds a slot to the access list in both state implementations.
func (d *DualStateDB) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	d.logger.Debugf("AddSlotToAccessList: addr=%s, slot=%s", addr.Hex(), slot.Hex())
	d.ethStateDB.AddSlotToAccessList(addr, slot)
	d.snapshotDB.AddSlotToAccessList(addr, slot)
	d.logger.Debugf("AddSlotToAccessList: completed")
}

// PointCache returns the point cache from the SnapshotDB.
func (d *DualStateDB) PointCache() *utils.PointCache {
	d.logger.Debugf("PointCache")
	result := d.snapshotDB.PointCache()
	d.logger.Debugf("PointCache: output result=%p", result)
	return result
}

// Prepare prepares both state implementations for transaction execution.
func (d *DualStateDB) Prepare(rules params.Rules, sender, coinbase common.Address, dest *common.Address, precompiles []common.Address, txAccesses types.AccessList) {
	destStr := "nil"
	if dest != nil {
		destStr = dest.Hex()
	}
	d.logger.Debugf("Prepare: sender=%s, coinbase=%s, dest=%s, precompiles len=%d, txAccesses len=%d",
		sender.Hex(), coinbase.Hex(), destStr, len(precompiles), len(txAccesses))
	d.ethStateDB.Prepare(rules, sender, coinbase, dest, precompiles, txAccesses)
	d.snapshotDB.Prepare(rules, sender, coinbase, dest, precompiles, txAccesses)
	d.logger.Debugf("Prepare: completed")
}

// RevertToSnapshot reverts to a snapshot in both state implementations.
func (d *DualStateDB) RevertToSnapshot(snapshot int) {
	d.logger.Debugf("RevertToSnapshot: snapshot=%d", snapshot)
	d.ethStateDB.RevertToSnapshot(snapshot)
	d.snapshotDB.RevertToSnapshot(snapshot)
	d.logger.Debugf("RevertToSnapshot: completed")
}

// Snapshot creates a snapshot in both state implementations.
// Both implementations must return the same snapshot ID for proper synchronization.
// We use the ethStateDB's snapshot ID as the canonical one.
func (d *DualStateDB) Snapshot() int {
	d.logger.Debugf("Snapshot: called")
	ethSnapshot := d.ethStateDB.Snapshot()
	snapSnapshot := d.snapshotDB.Snapshot()
	d.logger.Debugf("Snapshot: output result=%d", ethSnapshot)

	// Ensure both state DBs are synchronized - they should return the same snapshot ID
	if ethSnapshot != snapSnapshot {
		d.logger.Errorf("Snapshot ID mismatch: ethSnapshot=%d, snapSnapshot=%d", ethSnapshot, snapSnapshot)
		// This is a critical error that indicates the state DBs are out of sync
		panic("DualStateDB snapshot synchronization error")
	}

	// Return the ethStateDB's snapshot ID as it's the authoritative one for state root tracking
	return ethSnapshot
}

// AddLog adds a log to both state implementations.
func (d *DualStateDB) AddLog(logEntry *types.Log) {
	d.logger.Debugf("AddLog: log=%+v", logEntry)
	d.ethStateDB.AddLog(logEntry)
	d.snapshotDB.AddLog(logEntry)
	d.logger.Debugf("AddLog: completed")
}

// AddPreimage adds a preimage to both state implementations.
func (d *DualStateDB) AddPreimage(hash common.Hash, preimage []byte) {
	d.logger.Debugf("AddPreimage: hash=%s, preimage len=%d", hash.Hex(), len(preimage))
	d.ethStateDB.AddPreimage(hash, preimage)
	d.snapshotDB.AddPreimage(hash, preimage)
	d.logger.Debugf("AddPreimage: completed")
}

// Witness returns the witness from the SnapshotDB.
func (d *DualStateDB) Witness() *stateless.Witness {
	d.logger.Debugf("Witness: called")
	result := d.snapshotDB.Witness()
	d.logger.Debugf("Witness: returning result=%p", result)
	return result
}

// AccessEvents returns the access events from the SnapshotDB.
func (d *DualStateDB) AccessEvents() *ethstate.AccessEvents {
	d.logger.Debugf("AccessEvents: called")
	result := d.snapshotDB.AccessEvents()
	d.logger.Debugf("AccessEvents: returning result=%p", result)
	return result
}

// Finalise finalizes both state implementations.
func (d *DualStateDB) Finalise(deleteEmptyObjects bool) {
	d.logger.Debugf("Finalise: deleteEmptyObjects=%t", deleteEmptyObjects)
	d.ethStateDB.Finalise(deleteEmptyObjects)
	d.snapshotDB.Finalise(deleteEmptyObjects)
	d.logger.Debugf("Finalise: completed")
}

// Result returns the read-write set from the SnapshotDB.
// This is a SnapshotDB-specific method not part of vm.StateDB interface.
func (d *DualStateDB) Result() blocks.ReadWriteSet {
	d.logger.Debugf("Result: called")
	result := d.snapshotDB.Result()
	d.logger.Debugf("Result: returning result with %d reads, %d writes: %v", len(result.Reads), len(result.Writes), result.Writes)
	return result
}

// Logs returns the logs from the SnapshotDB.
// This is a SnapshotDB-specific method not part of vm.StateDB interface.
func (d *DualStateDB) Logs() []Log {
	d.logger.Debugf("Logs: called")
	result := d.snapshotDB.Logs()
	d.logger.Debugf("Logs: returning result len=%d", len(result))
	return result
}
