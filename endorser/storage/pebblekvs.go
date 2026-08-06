/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/pebble"
	gethpebble "github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-evm/common"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// PebbleKVS is a persistent, MVCC key-value store that implements the KVS
// interface as a drop-in replacement for the in-memory LightKVS. Unlike
// LightKVS it survives process restarts: every block's writes are committed
// atomically alongside a persisted block-number checkpoint, so on reopen the
// store resumes from the last committed block.
//
// # Storage model (multi-version, insert-only)
//
// Each write is stored under its own versioned key rather than overwriting a
// single latest slot. The data key layout is:
//
//	'd' | be32(len(fullKey)) | fullKey | be64(^block) | be64(^tx)
//
// where fullKey is the "namespace:key" string used throughout the endorser and
// ^x == math.MaxUint64-x. Length-prefixing fullKey makes the per-key prefix
// unambiguous, and encoding (block, tx) in descending order means a forward
// scan starting at be64(^lastBlock) lands first on the highest block <=
// lastBlock (and, within it, the highest tx). geth's ethdb.Iterator is
// forward-only, so this descending encoding is what makes a "read as of block
// N" query a single Next().
//
// A reserved meta key 'm'|"block" holds be64(lastCommittedBlock) and is written
// in the same batch as the block's data keys, giving an atomic checkpoint for
// crash recovery.
//
// # Durability
//
// Each block commits as one atomic pebble batch, so a crash leaves either the
// whole block applied or none of it. Commits use geth's default (NoSync): the
// data survives a process restart, but an OS crash or power loss can lose the
// most recently committed blocks. Those are recoverable from the ledger — the
// persisted checkpoint tells the synchronizer where to resume — so this is safe
// for the endorser, whose state is always reconstructible from committed blocks.
//
// # Parity notes vs LightKVS
//
//   - Time-travel reads: a snapshot at block N returns, for each key, the record
//     with the highest version whose block <= N (mirroring the sqlite-backed
//     VersionedDB, see fabric-x-sdk state.VersionedDB.Get). LightKVS instead
//     keeps only a small ring buffer of recent snapshots and errors for evicted
//     blocks; PebbleKVS can serve any historical block. This is a strict
//     superset of LightKVS's read behavior.
//   - Deletes are stored as tombstone records (IsDelete=true), matching the
//     VersionedDB, rather than removing the key as LightKVS does. The execution
//     layer already treats IsDelete=true records as absent (nil value, nil
//     version in the read-set), so this is behaviorally equivalent for callers
//     but preserves the version chain for MVCC validation.
//   - Block height tracks the ledger exactly: a block that yields no writes still
//     advances the persisted checkpoint, where LightKVS leaves its height at the
//     last block that contained writes. See Handle.
//   - The write path is idempotent per block: re-applying a block at or below the
//     checkpoint is a no-op, where LightKVS would apply it again and bump every
//     touched key's version. See commitBlock.
//   - Version numbers mirror the VersionedDB, not LightKVS: each write's Version
//     is MAX(version)+1 for its key, so multiple writes to one key within a block
//     get consecutive versions and the counter is monotonic across a tombstone
//     (a rewrite after a delete does not reset to 0). LightKVS instead shares one
//     version across a block's writes to a key and resets after a delete. Because
//     the fabric-x read-set carries this Version to the committer for MVCC
//     validation, matching the VersionedDB — the committer's own store — is the
//     correct behavior; the two divergences from LightKVS are intentional.
type PebbleKVS struct {
	db *gethpebble.Database

	// writeMu serializes Update calls. The KVS contract already assumes a
	// single writer, but the per-key version lookup performs a read against
	// committed state that must not interleave with a concurrent commit.
	writeMu sync.Mutex

	// currentBlock is the last committed block number, cached in memory for
	// cheap BlockNumber() reads and seeded from the meta checkpoint on open.
	currentBlock atomic.Uint64

	// hasCheckpoint records whether currentBlock reflects an actual commit, as
	// opposed to the zero value of a fresh store. currentBlock alone cannot
	// distinguish the two, and the replay guard in commitBlock would otherwise
	// treat block 0 of a fresh store as already applied.
	hasCheckpoint atomic.Bool
}

var pebbleLogger = flogging.MustGetLogger("endorser.storage.pebblekvs")

const (
	// prefixData tags versioned data keys.
	prefixData = 'd'
	// prefixMeta tags reserved metadata keys.
	prefixMeta = 'm'
)

// metaBlockKey holds the last committed block number.
var metaBlockKey = []byte{prefixMeta, 'b', 'l', 'o', 'c', 'k'}

// Compile-time assertion that PebbleKVS satisfies the KVS interface.
var _ KVS = (*PebbleKVS)(nil)

// NewPebbleKVS opens (or creates) a pebble-backed KVS rooted at dir. The
// historySize parameter is accepted for interface symmetry with NewLightKVS but
// is currently unused: PebbleKVS retains full history (that is the point of a
// persistent store). It is reserved for a future disk-bounding pruning pass.
func NewPebbleKVS(dir string, historySize int) (*PebbleKVS, error) {
	if dir == "" {
		return nil, errors.New("pebble kvs requires a non-empty data directory (connection-string)")
	}
	_ = historySize // reserved for optional pruning; see doc comment.

	// cache is in MB; handles is the max open-file budget. Modest defaults
	// that keep the working set in memory without a large FD footprint.
	db, err := gethpebble.New(dir, 128, 512, "endorser/pebblekvs", false)
	if err != nil {
		return nil, fmt.Errorf("failed to open pebble db at %q: %w", dir, err)
	}

	kvs := &PebbleKVS{db: db}

	// Recover the last committed block number from the atomic checkpoint.
	block, found, err := kvs.readMetaBlock()
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to read block checkpoint: %w", err)
	}
	kvs.currentBlock.Store(block)
	kvs.hasCheckpoint.Store(found)
	if found {
		pebbleLogger.Infof("opened pebble kvs at %q, resuming from block %d", dir, block)
	} else {
		pebbleLogger.Infof("opened fresh pebble kvs at %q", dir)
	}

	return kvs, nil
}

// readMetaBlock returns the persisted last-committed block and whether a
// checkpoint was found at all. The two are distinct: a fresh store and a store
// whose last commit was block 0 both report block 0.
func (p *PebbleKVS) readMetaBlock() (uint64, bool, error) {
	raw, err := p.db.Get(metaBlockKey)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if len(raw) != 8 {
		return 0, false, fmt.Errorf("corrupt block checkpoint: got %d bytes, want 8", len(raw))
	}
	return binary.BigEndian.Uint64(raw), true, nil
}

// Update atomically applies a batch of writes for a single block. All writes
// must carry the same block number (they come from the same block); the block's
// data keys and the meta checkpoint are committed in one pebble batch, so a
// crash either leaves the whole block applied or none of it.
//
// Blocks at or below the persisted checkpoint are skipped as already applied —
// see commitBlock.
func (p *PebbleKVS) Update(updates []KeyValueVersion) error {
	if len(updates) == 0 {
		return nil
	}

	// A batch spanning several blocks would be committed under a single
	// checkpoint, so a crash could leave the store claiming a height whose
	// blocks are only partly applied. Both write paths group by block before
	// calling Update, so this is a broken-invariant check, not a runtime
	// condition.
	blockNum := updates[0].BlockNum
	for i := range updates {
		if updates[i].BlockNum != blockNum {
			return fmt.Errorf(
				"pebble kvs: update batch spans multiple blocks (%d at index 0, %d at index %d); "+
					"writes must be grouped by block before committing",
				blockNum, updates[i].BlockNum, i)
		}
	}

	return p.commitBlock(blockNum, updates)
}

// commitBlock applies a single block's writes (possibly none) and advances the
// persisted checkpoint to blockNum, in one atomic pebble batch.
//
// Blocks at or below the current checkpoint are skipped. This is what makes the
// write path idempotent under replay, which is required rather than merely nice:
// the synchronizer resumes at lastBlock+1 of whichever store it was given as its
// height reader, and in the gateway topology that store is the block DB, not
// this KVS (see integration/test_helpers.go, where handlers run in the order
// [endorser KVSs..., chain, gateway]). A crash between this KVS's commit and the
// block DB's therefore replays a block this KVS has already applied. Applying a
// block's writes twice would advance every touched key's version an extra step
// and permanently desynchronize this store from the committer's worldstate, so
// the read-sets built from it would fail MVCC validation. The block DB tolerates
// the same replay via ON CONFLICT DO NOTHING (gateway/storage/query.sql).
func (p *PebbleKVS) commitBlock(blockNum uint64, updates []KeyValueVersion) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	if p.hasCheckpoint.Load() && blockNum <= p.currentBlock.Load() {
		pebbleLogger.Debugf("skipping already-applied block %d (checkpoint at %d)", blockNum, p.currentBlock.Load())
		return nil
	}

	batch := p.db.NewBatch()
	defer batch.Close()

	// nextVersion holds the version to assign to the *next* write of each key
	// within this batch. VersionedDB computes COALESCE(MAX(version)+1, 0) live
	// per insert, so successive writes to the same key in one block receive
	// consecutive versions (fabric-x-sdk state.VersionedDB.UpdateWorldState).
	// We mirror that exactly: the first write to a key seeds from the committed
	// latest, and each subsequent write in the same batch increments — so the
	// highest-tx write wins the read with the highest version, matching the
	// committer's worldstate that the fabric-x MVCC read-set is validated
	// against. Seeding from a single point-read also avoids re-reading committed
	// state for a key written multiple times in one block.
	nextVersion := make(map[string]uint64, len(updates))

	for i := range updates {
		u := &updates[i]

		version, seen := nextVersion[u.Key]
		if !seen {
			latest, err := p.latestVersion(u.Key)
			if err != nil {
				return fmt.Errorf("failed to read latest version for %q: %w", u.Key, err)
			}
			version = uint64(latest + 1) // latest is -1 for a fresh key → version 0
		}
		nextVersion[u.Key] = version + 1

		value := encodeRecord(record{
			BlockNum: u.BlockNum,
			TxNum:    u.TxNum,
			Version:  version,
			TxID:     u.TxID,
			IsDelete: u.IsDelete,
			Value:    u.Value,
		})

		if err := batch.Put(dataKey(u.Key, u.BlockNum, u.TxNum), value); err != nil {
			return fmt.Errorf("failed to stage write for %q: %w", u.Key, err)
		}
	}

	// Checkpoint the block number in the same batch for an atomic commit.
	if err := batch.Put(metaBlockKey, u64be(blockNum)); err != nil {
		return fmt.Errorf("failed to stage block checkpoint: %w", err)
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to commit block %d: %w", blockNum, err)
	}

	p.currentBlock.Store(blockNum)
	p.hasCheckpoint.Store(true)
	return nil
}

// latestVersion returns the version of the most recent committed record for
// fullKey, or -1 if the key has never been written. The most recent record is
// the first hit of a forward scan over the key's prefix (descending version
// encoding puts the highest block/tx first).
func (p *PebbleKVS) latestVersion(fullKey string) (int64, error) {
	it := p.db.NewIterator(dataPrefix(fullKey), nil)
	defer it.Release()

	if it.Next() {
		rec, err := decodeRecord(it.Value())
		if err != nil {
			return 0, err
		}
		return int64(rec.Version), nil
	}
	return -1, it.Error()
}

// NewSnapshot returns a read view of the store as of blockNumber. A blockNumber
// of 0 resolves to the latest committed block at call time, pinning the view so
// later commits (higher block numbers) are naturally excluded. Any historical
// block is serviceable; unlike LightKVS this never errors for an "evicted"
// block.
func (p *PebbleKVS) NewSnapshot(blockNumber uint64) (execution.ReadStore, error) {
	if blockNumber == 0 {
		blockNumber = p.currentBlock.Load()
	}
	return &pebbleSnapshot{
		db:        p.db,
		lastBlock: blockNumber,
	}, nil
}

// Get returns the record for (namespace, key) as of lastBlock, or nil if no
// such record exists at or before that block. A lastBlock of 0 resolves to the
// latest committed block.
func (p *PebbleKVS) Get(namespace, key string, lastBlock uint64) (*blocks.WriteRecord, error) {
	r, err := p.NewSnapshot(lastBlock)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.Get(namespace, key)
}

// Handle implements blocks.BlockHandler: it extracts all valid transaction
// writes from a block and applies them atomically. Called by the synchronizer.
//
// A block that yields no writes — empty, config-only, or carrying nothing but
// invalid transactions — still advances the persisted checkpoint. LightKVS can
// leave its height behind in that case because its state does not outlive the
// process, but for a persistent store the checkpoint is the resume point: were
// it to lag, a restart would re-deliver every block since the last one that
// happened to contain writes, and the store's reported height would disagree
// with the block DB's, which does log empty blocks. Keeping height equal to
// ledger height is what lets this KVS serve as the synchronizer's height reader.
func (p *PebbleKVS) Handle(ctx context.Context, b blocks.Block) error {
	var updates []KeyValueVersion
	for _, tx := range b.Transactions {
		collectWrites(&updates, tx.NsRWS, b.Number, uint64(tx.Number), tx.ID, tx.Valid)
	}
	return p.commitBlock(b.Number, updates)
}

// HandleTx implements the notification write path: it extracts writes from a
// batch of committed transaction notifications and applies them. Notifications
// may span multiple blocks, so writes are grouped by block and each block is
// committed atomically.
//
// Each call must carry all of a block's notifications. AllTxBatchDispatcher
// satisfies this — the SDK delivers one AllTxBatch per block and the dispatcher
// forwards each batch as one call — and the replay guard in commitBlock depends
// on it: a block arriving in two calls would have its second half skipped as
// already applied rather than merged.
func (p *PebbleKVS) HandleTx(ctx context.Context, notifs []common.TxNotification) error {
	if len(notifs) == 0 {
		return nil
	}

	// Group by block so each block commits as one atomic batch, then apply
	// blocks in ascending order.
	byBlock := make(map[uint64][]KeyValueVersion)
	var order []uint64
	for _, notif := range notifs {
		valid := notif.Status == committerpb.Status_COMMITTED
		before := len(byBlock[notif.BlockNum])
		updates := byBlock[notif.BlockNum]
		collectWrites(&updates, notif.NsRWS, notif.BlockNum, notif.TxNum, notif.FabricTxID, valid)
		if before == 0 && len(updates) > 0 {
			order = append(order, notif.BlockNum)
		}
		byBlock[notif.BlockNum] = updates
	}

	// Apply in ascending block order so the persisted checkpoint advances
	// monotonically.
	sortUint64(order)
	for _, blockNum := range order {
		if err := p.Update(byBlock[blockNum]); err != nil {
			return err
		}
	}
	return nil
}

// BlockNumber returns the last committed block number.
func (p *PebbleKVS) BlockNumber(ctx context.Context) (uint64, error) {
	return p.currentBlock.Load(), nil
}

// Close closes the underlying pebble database.
func (p *PebbleKVS) Close() error {
	return p.db.Close()
}

// pebbleSnapshot is a point-in-time read view pinned to a block number. It
// implements execution.ReadStore. Isolation is inherent to the insert-only MVCC
// model: existing records never mutate, and later commits have higher block
// numbers that the descending-version seek skips.
type pebbleSnapshot struct {
	db        *gethpebble.Database
	lastBlock uint64
	closed    bool
}

// Get returns the record for (namespace, key) as of the snapshot's block, or
// nil if none exists at or before it. Tombstone records are returned as-is
// (IsDelete=true); the execution layer treats them as absent.
func (s *pebbleSnapshot) Get(namespace, key string) (*blocks.WriteRecord, error) {
	if s.closed {
		return nil, errors.New("reader is closed")
	}

	fullKey := namespace + ":" + key

	// Seek to the first record whose block <= lastBlock. start = be64(^lastBlock)
	// is <= any key at block == lastBlock and > any key at block > lastBlock.
	it := s.db.NewIterator(dataPrefix(fullKey), u64be(^s.lastBlock))
	defer it.Release()

	if it.Next() {
		rec, err := decodeRecord(it.Value())
		if err != nil {
			return nil, err
		}
		rec.Namespace = namespace
		rec.Key = key
		return rec, nil
	}
	return nil, it.Error()
}

// Close releases the snapshot. After Close the snapshot cannot be used.
func (s *pebbleSnapshot) Close() error {
	s.closed = true
	return nil
}

// --- key/value encoding ---------------------------------------------------

// record is the decoded form of a stored value. Namespace and Key are not
// stored (they are recoverable from the query) and are filled in by the reader.
type record struct {
	BlockNum uint64
	TxNum    uint64
	Version  uint64
	TxID     string
	IsDelete bool
	Value    []byte
}

// dataPrefix returns the unambiguous per-key prefix: 'd' | be32(len) | fullKey.
// The returned slice has len == cap so geth's NewIterator, which does
// append(prefix, start...), reallocates instead of clobbering our buffer.
func dataPrefix(fullKey string) []byte {
	b := make([]byte, 0, 1+4+len(fullKey))
	b = append(b, prefixData)
	b = binary.BigEndian.AppendUint32(b, uint32(len(fullKey)))
	b = append(b, fullKey...)
	return b
}

// dataKey returns the full versioned key for a write: the per-key prefix
// followed by the descending-encoded (block, tx) version suffix.
func dataKey(fullKey string, block, tx uint64) []byte {
	b := dataPrefix(fullKey)
	b = binary.BigEndian.AppendUint64(b, ^block)
	b = binary.BigEndian.AppendUint64(b, ^tx)
	return b
}

// u64be returns the 8-byte big-endian encoding of v.
func u64be(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// record value layout (compact, varint-based; encoding cost is on the hot path
// so JSON is deliberately avoided):
//
//	flags byte      : bit0 = IsDelete, bit1 = value is non-nil
//	uvarint BlockNum
//	uvarint TxNum
//	uvarint Version
//	uvarint len(TxID) | TxID bytes
//	[ uvarint len(Value) | Value bytes ]   -- present iff value non-nil
const (
	flagIsDelete    = 1 << 0
	flagValueNonNil = 1 << 1
)

// encodeRecord serializes a record into a compact binary value.
func encodeRecord(r record) []byte {
	var flags byte
	if r.IsDelete {
		flags |= flagIsDelete
	}
	if r.Value != nil {
		flags |= flagValueNonNil
	}

	// Rough capacity estimate: flags + 3 uvarints + txid + value.
	buf := make([]byte, 0, 1+3*binary.MaxVarintLen64+len(r.TxID)+len(r.Value)+2*binary.MaxVarintLen64)
	buf = append(buf, flags)
	buf = binary.AppendUvarint(buf, r.BlockNum)
	buf = binary.AppendUvarint(buf, r.TxNum)
	buf = binary.AppendUvarint(buf, r.Version)
	buf = binary.AppendUvarint(buf, uint64(len(r.TxID)))
	buf = append(buf, r.TxID...)
	if r.Value != nil {
		buf = binary.AppendUvarint(buf, uint64(len(r.Value)))
		buf = append(buf, r.Value...)
	}
	return buf
}

// decodeRecord parses a stored value. The returned WriteRecord's Namespace and
// Key are left empty for the caller to populate. Value bytes are copied out so
// the result is safe to retain after the iterator advances.
func decodeRecord(raw []byte) (*blocks.WriteRecord, error) {
	if len(raw) < 1 {
		return nil, errors.New("corrupt record: empty value")
	}
	flags := raw[0]
	pos := 1

	blockNum, n := binary.Uvarint(raw[pos:])
	if n <= 0 {
		return nil, errors.New("corrupt record: block num")
	}
	pos += n

	txNum, n := binary.Uvarint(raw[pos:])
	if n <= 0 {
		return nil, errors.New("corrupt record: tx num")
	}
	pos += n

	version, n := binary.Uvarint(raw[pos:])
	if n <= 0 {
		return nil, errors.New("corrupt record: version")
	}
	pos += n

	txIDLen, n := binary.Uvarint(raw[pos:])
	if n <= 0 {
		return nil, errors.New("corrupt record: txid length")
	}
	pos += n
	if pos+int(txIDLen) > len(raw) {
		return nil, errors.New("corrupt record: txid overruns value")
	}
	txID := string(raw[pos : pos+int(txIDLen)])
	pos += int(txIDLen)

	var value []byte
	if flags&flagValueNonNil != 0 {
		valLen, n := binary.Uvarint(raw[pos:])
		if n <= 0 {
			return nil, errors.New("corrupt record: value length")
		}
		pos += n
		if pos+int(valLen) > len(raw) {
			return nil, errors.New("corrupt record: value overruns value")
		}
		// Copy so the result survives the next iterator step.
		value = make([]byte, valLen)
		copy(value, raw[pos:pos+int(valLen)])
	}

	return &blocks.WriteRecord{
		BlockNum: blockNum,
		TxNum:    txNum,
		Version:  version,
		Value:    value,
		IsDelete: flags&flagIsDelete != 0,
		TxID:     txID,
	}, nil
}

// sortUint64 sorts a small slice of block numbers ascending (insertion sort;
// notification batches span very few distinct blocks).
func sortUint64(s []uint64) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
