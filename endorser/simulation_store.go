/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package endorser

import (
	"context"
	"errors"

	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// Log is a type of event.
type Log struct {
	Address []byte
	Topics  [][]byte
	Data    []byte
}

// SimulationStore implements a very basic set of state interactions on a snapshot of the world state.
// It records reads and writes. By default, like in Fabric, you cannot 'read your own writes' but this can be configured.
// Function signatures correspond to the 'chaincode stub' where possible.
//
// SNAPSHOT/REVERT SUPPORT:
// The SimulationStore now supports EVM-style snapshots and reverts. This is critical for proper
// EVM execution where operations can be conditionally reverted (e.g., when a subcall fails).
//
// Key principles:
// 1. ALL operations (reads, writes, logs) are journaled as they occur
// 2. Snapshots record the current journal position
// 3. Reverts replay the journal in reverse to undo operations
// 4. Only non-reverted operations appear in the final read-write set
//
// This ensures that:
// - Reverted reads don't appear in the RWS (avoiding unnecessary MVCC conflicts)
// - Reverted writes don't appear in the RWS (maintaining correctness)
// - Reverted logs are discarded (matching EVM semantics)
type SimulationStore struct {
	namespace         string
	store             ReadStore
	blockNum          uint64
	reads             map[string]blocks.KVRead
	writes            map[string]blocks.KVWrite
	logs              []Log
	monotonicVersions bool // if true, KVRead.Version is built from WriteRecord.Version (fabric-x MVCC semantics)

	// Snapshot/revert support: journal tracks all state-modifying operations
	// so they can be undone on revert. Each journal entry knows how to undo itself.
	journal        []journalEntry
	validRevisions []revision
	nextRevisionId int
}

// ReadStore is the read interface required to back a SimulationStore.
type ReadStore interface {
	Get(namespace, key string, lastBlock uint64) (*blocks.WriteRecord, error)
	BlockNumber(ctx context.Context) (uint64, error)
}

// journalEntry is an interface for operations that can be reverted.
// Each entry knows how to undo its effect on the SimulationStore.
type journalEntry interface {
	revert(*SimulationStore)
}

// revision represents a snapshot point in the journal.
// It records the journal index and assigns a unique ID for identification.
type revision struct {
	id           int
	journalIndex int
}

// Journal entry types for different operations:

// readEntry records a read operation so it can be removed on revert.
// This ensures reverted reads don't appear in the final read-write set.
type readEntry struct {
	key string
}

func (e readEntry) revert(s *SimulationStore) {
	delete(s.reads, e.key)
}

// writeEntry records a write operation with its previous state.
// On revert, we restore the previous value (or remove the entry if it didn't exist).
type writeEntry struct {
	key     string
	prevVal blocks.KVWrite
	hadPrev bool
}

func (e writeEntry) revert(s *SimulationStore) {
	if e.hadPrev {
		s.writes[e.key] = e.prevVal
	} else {
		delete(s.writes, e.key)
	}
}

// logEntry records a log addition so it can be removed on revert.
// We store the index where the log was added.
type logEntry struct {
	logIndex int
}

func (e logEntry) revert(s *SimulationStore) {
	// Remove the log at the recorded index.
	// Since we revert in reverse order, this log should be at the end.
	if e.logIndex < len(s.logs) {
		s.logs = s.logs[:e.logIndex]
	}
}

// NewSimulationStore returns a read-only snapshot that records reads and writes.
// If blockNum is 0, the current block number is queried from store and used as the snapshot height.
// monotonicVersions controls MVCC semantics: when true, KVRead versions use the per-key
// monotonic version counter (Fabric-X); when false, they use (blockNum, txNum) (standard Fabric).
func NewSimulationStore(ctx context.Context, store ReadStore, namespace string, blockNum uint64, monotonicVersions bool) (*SimulationStore, error) {
	if blockNum == 0 {
		var err error
		blockNum, err = store.BlockNumber(ctx)
		if err != nil {
			return nil, err
		}
	}
	return &SimulationStore{
		namespace:         namespace,
		store:             store,
		blockNum:          blockNum,
		monotonicVersions: monotonicVersions,
		reads:             make(map[string]blocks.KVRead),
		writes:            make(map[string]blocks.KVWrite),
		journal:           make([]journalEntry, 0),
		validRevisions:    make([]revision, 0),
		nextRevisionId:    0,
	}, nil
}

// GetState behaves similar to in Fabric, with the exception that we _can_ read
// our own writes if explicitly configured.
// read own write (if enabled) -> return last written value, nil
// read own delete (if enabled) -> return nil, nil
// no result -> store read with nil version, return nil, nil
// deleted result -> no read, return nil, nil
// result -> store read version, return value, nil
//
// SNAPSHOT/REVERT: Read operations are journaled so they can be removed if reverted.
// This is important because in Fabric, reads create MVCC dependencies. If a read is
// reverted (e.g., in a failed subcall), it should not appear in the final RWS.
func (s *SimulationStore) GetState(key string) ([]byte, error) {
	// return early, we don't record reading your own writes
	if record, ok := s.writes[key]; ok {
		if record.IsDelete {
			return nil, nil
		}
		return record.Value, nil
	}

	// get from store snapshot
	record, err := s.store.Get(s.namespace, key, s.blockNum)
	if err != nil {
		return nil, err
	}

	var val []byte
	var read = blocks.KVRead{Key: key}
	if record != nil {
		// Fabric doesn't add a read marker if the value is deleted.
		if record.IsDelete {
			return nil, nil
		}
		if s.monotonicVersions {
			read.Version = &blocks.Version{BlockNum: record.Version}
		} else {
			read.Version = &blocks.Version{
				BlockNum: record.BlockNum,
				TxNum:    record.TxNum,
			}
		}

		val = record.Value
	}

	// Journal the read operation before recording it.
	// This allows us to remove it if the operation is reverted.
	s.journal = append(s.journal, readEntry{key: key})
	s.reads[key] = read

	return val, nil
}

// PutState puts the specified `key` and `value` into the transaction's
// writeset as a data-write proposal. PutState doesn't effect the ledger
// until the transaction is validated and successfully committed.
// Simple keys must not be an empty string and must not start with a
// null character (0x00) in order to avoid range query collisions with
// composite keys, which internally get prefixed with 0x00 as composite
// key namespace. In addition, if using CouchDB, keys can only contain
// valid UTF-8 strings and cannot begin with an underscore ("_").
//
// SNAPSHOT/REVERT: Write operations are journaled with their previous value
// so they can be undone if reverted.
func (s *SimulationStore) PutState(key string, value []byte) error {
	if len(key) == 0 {
		return errors.New("key is empty")
	}
	if len(value) == 0 {
		return s.DelState(key)
	}

	// Journal the write with previous value
	prevVal, hadPrev := s.writes[key]
	s.journal = append(s.journal, writeEntry{
		key:     key,
		prevVal: prevVal,
		hadPrev: hadPrev,
	})

	s.writes[key] = blocks.KVWrite{Key: key, Value: value}
	return nil
}

// DelState records the specified `key` to be deleted in the writeset of
// the transaction proposal. The `key` and its value will be deleted from
// the ledger when the transaction is validated and successfully committed.
//
// SNAPSHOT/REVERT: Delete operations are journaled as writes with IsDelete=true.
func (s *SimulationStore) DelState(key string) error {
	// Journal the write with previous value
	prevVal, hadPrev := s.writes[key]
	s.journal = append(s.journal, writeEntry{
		key:     key,
		prevVal: prevVal,
		hadPrev: hadPrev,
	})

	s.writes[key] = blocks.KVWrite{Key: key, IsDelete: true}
	return nil
}

// AddLog records a log (ethereum event) which will be part of the endorsed transaction.
//
// SNAPSHOT/REVERT: Log additions are journaled so they can be removed if reverted.
func (s *SimulationStore) AddLog(address []byte, topics [][]byte, data []byte) {
	// Journal the log addition with its index
	s.journal = append(s.journal, logEntry{logIndex: len(s.logs)})

	s.logs = append(s.logs, Log{
		Topics:  topics,
		Data:    data,
		Address: address,
	})
}

// Snapshot creates a snapshot of the current state.
// Returns a snapshot ID that can be used with RevertToSnapshot.
// Snapshots can be nested - each call to Snapshot() returns a new ID.
//
// The snapshot records the current position in the journal. If RevertToSnapshot
// is called, all journal entries after this position are undone in reverse order.
func (s *SimulationStore) Snapshot() int {
	id := s.nextRevisionId
	s.nextRevisionId++
	s.validRevisions = append(s.validRevisions, revision{
		id:           id,
		journalIndex: len(s.journal),
	})
	return id
}

// RevertToSnapshot reverts all state changes made after the snapshot with the given ID.
// This includes reads, writes, and logs. After revert, those operations will not appear
// in the final read-write set.
//
// The snapshot ID and all snapshots created after it are invalidated.
// If the snapshot ID is not found (already reverted or never existed), this is a no-op.
//
// Implementation: We replay the journal in reverse order from the current position back
// to the snapshot position, calling revert() on each entry. This undoes all operations
// that occurred after the snapshot was taken.
func (s *SimulationStore) RevertToSnapshot(revid int) {
	// Find the snapshot in the stack of valid snapshots
	idx := -1
	for i, rev := range s.validRevisions {
		if rev.id == revid {
			idx = i
			break
		}
	}
	if idx == -1 {
		return // Snapshot not found
	}

	snapshot := s.validRevisions[idx].journalIndex

	// Replay the journal in reverse to undo changes
	for i := len(s.journal) - 1; i >= snapshot; i-- {
		s.journal[i].revert(s)
	}

	// Truncate journal and revisions
	s.journal = s.journal[:snapshot]
	s.validRevisions = s.validRevisions[:idx]
}

// Result returns the read-write set containing all non-reverted operations.
// Only operations that were not reverted via RevertToSnapshot will be included.
// This is the authoritative source of what operations occurred during simulation.
func (s *SimulationStore) Result() blocks.ReadWriteSet {
	rws := blocks.ReadWriteSet{
		Reads:  make([]blocks.KVRead, 0, len(s.reads)),
		Writes: make([]blocks.KVWrite, 0, len(s.writes)),
	}
	for _, r := range s.reads {
		rws.Reads = append(rws.Reads, r)
	}
	for _, w := range s.writes {
		rws.Writes = append(rws.Writes, w)
	}
	return rws
}

// Logs returns all non-reverted logs.
// Only logs that were not reverted via RevertToSnapshot will be included.
func (s *SimulationStore) Logs() []Log {
	return s.logs
}

// Version is the blockheight of this snapshot.
func (s *SimulationStore) Version() uint64 {
	return s.blockNum
}
