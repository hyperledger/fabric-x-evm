/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: LGPL-3.0-or-later
*/

package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/pebble"
	"github.com/ethereum/go-ethereum/ethdb"
	gethpebble "github.com/ethereum/go-ethereum/ethdb/pebble"
	"github.com/hyperledger/fabric-lib-go/common/flogging"
	"github.com/hyperledger/fabric-x-evm/endorser/execution"
	"github.com/hyperledger/fabric-x-sdk/blocks"
)

// PebbleKVS is a persistent, MVCC key-value store that implements the KVS
// interface as a drop-in replacement for the in-memory LightKVS. Unlike
// LightKVS it survives process restarts: every block's writes are committed
// atomically alongside a persisted block-number checkpoint, so on reopen the
// store resumes from the last committed block.
//
// # Storage model
//
// A key's current record lives under a version-free key, so a rewrite overwrites
// it and reading it is a single point lookup that pebble's block cache and bloom
// filters serve without allocating an iterator:
//
//	'd' | be32(len(fullKey)) | fullKey
//
// where fullKey is the "namespace:key" string used throughout the endorser and
// the length prefix makes the per-key prefix unambiguous.
//
// A write that supersedes an existing record copies it aside, so reads pinned to
// an earlier block can still find it:
//
//	'h' | be32(len(fullKey)) | fullKey | be64(^block) | be64(^tx)
//
// with ^x == math.MaxUint64-x. That descending suffix is what makes a read as of
// block N a single Next(): a forward scan from be64(^N) lands on the highest
// retained version at or below N. geth's iterator is forward-only.
//
// Only historySize blocks of that history are kept, mirroring LightKVS's
// snapshot ring — pruning it is what stops the store growing with every rewrite.
// So a commit can find what aged out without scanning every key, each history
// entry is also recorded under the block that created it:
//
//	'i' | be64(block) | <the history key it created>   ->   no value
//
// Two reserved meta keys hold be64 values and are written in the same batch as
// the block they describe: 'm'|"block" is the last committed block (the
// crash-recovery checkpoint), and 'm'|"oldest" the lowest block a historical
// read can still be answered for.
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
//     VersionedDB, see fabric-x-sdk state.VersionedDB.Get), for any N in the
//     retained window [head-historySize, head]. Outside it NewSnapshot errors,
//     as LightKVS does for a block evicted from its ring. The window is that
//     range of heights, not the set of heights a block was delivered at, so if
//     block numbers skip, the two disagree at the heights in the gap: any height
//     in the window is answerable here, where LightKVS's ring matches a delivered
//     block number exactly. This follows the VersionedDB, which answers any
//     height.
//   - Deletes are stored as tombstone records (IsDelete=true), matching the
//     VersionedDB. The execution layer already treats IsDelete=true records as
//     absent (nil value, nil version in the read-set), so this is behaviorally
//     equivalent for callers but preserves the version chain for MVCC validation.
//   - Block height tracks the ledger exactly: a block that yields no writes still
//     advances the persisted checkpoint, where LightKVS leaves its height at the
//     last block that contained writes. See Handle.
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

	// historySize is how many blocks of superseded records are retained for
	// time-travel reads, mirroring LightKVS's snapshot ring.
	historySize uint64

	// writeMu serializes commits. The KVS contract already assumes a single
	// writer, but the per-key version lookup performs a read against committed
	// state that must not interleave with a concurrent commit.
	writeMu sync.Mutex

	// currentBlock is the last committed block number, cached in memory for
	// cheap BlockNumber() reads and seeded from the meta checkpoint on open.
	currentBlock atomic.Uint64

	// hasCheckpoint records whether currentBlock reflects an actual commit, as
	// opposed to the zero value of a fresh store. currentBlock alone cannot
	// distinguish the two, and the replay guard in commitBlock would otherwise
	// treat block 0 of a fresh store as already applied.
	hasCheckpoint atomic.Bool

	// oldestRetained is the lowest block a historical read can still be answered
	// for; everything below it has been pruned. Seeded from metaOldestKey on open.
	oldestRetained atomic.Uint64

	// readerMu guards openReaders. A commit holds it across its prune so a reader
	// cannot be admitted at a height that prune is cutting away.
	readerMu sync.Mutex

	// openReaders counts live readers by the height each is pinned to. A reader at
	// H is served by history entries created after H, so holding the prune cutoff
	// at or below the lowest pin keeps every reader whole.
	openReaders map[uint64]int
}

var pebbleLogger = flogging.MustGetLogger("endorser.storage.pebblekvs")

const (
	// prefixData tags a key's current record.
	prefixData = 'd'
	// prefixHistory tags retained superseded records, for in-window time travel.
	prefixHistory = 'h'
	// prefixIndex tags history entries by the block that created them, for pruning.
	prefixIndex = 'i'
	// prefixMeta tags reserved metadata keys.
	prefixMeta = 'm'

	// defaultHistorySize is the retained window when the caller does not ask for
	// one. Callers normally pass endorser config's history_size.
	defaultHistorySize = 128
)

var (
	// metaBlockKey holds the last committed block number.
	metaBlockKey = []byte{prefixMeta, 'b', 'l', 'o', 'c', 'k'}

	// metaOldestKey holds the lowest block a historical read can still be answered
	// for. Persisted rather than derived from historySize, because what has been
	// pruned depends on the historySize that was in force when it happened.
	metaOldestKey = []byte{prefixMeta, 'o', 'l', 'd', 'e', 's', 't'}
)

// Compile-time assertion that PebbleKVS satisfies the KVS interface.
var _ KVS = (*PebbleKVS)(nil)

// NewPebbleKVS opens (or creates) a pebble-backed KVS rooted at dir.
//
// historySize is how many recent blocks of superseded records to retain for
// time-travel reads, matching LightKVS's ring-buffer semantics. A non-positive
// value falls back to defaultHistorySize.
func NewPebbleKVS(dir string, historySize int) (*PebbleKVS, error) {
	if dir == "" {
		return nil, errors.New("pebble kvs requires a non-empty data directory (connection-string)")
	}
	if historySize <= 0 {
		historySize = defaultHistorySize
	}

	// cache is in MB; handles is the max open-file budget. Modest defaults
	// that keep the working set in memory without a large FD footprint.
	db, err := gethpebble.New(dir, 128, 512, "endorser/pebblekvs", false)
	if err != nil {
		return nil, fmt.Errorf("failed to open pebble db at %q: %w", dir, err)
	}

	kvs := &PebbleKVS{
		db:          db,
		historySize: uint64(historySize),
		openReaders: map[uint64]int{},
	}

	// Recover the last committed block number from the atomic checkpoint.
	block, found, err := kvs.readMetaBlock()
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to read block checkpoint: %w", err)
	}
	kvs.currentBlock.Store(block)
	kvs.hasCheckpoint.Store(found)

	oldest, _, err := kvs.readMeta(metaOldestKey)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to read oldest-retained marker: %w", err)
	}
	kvs.oldestRetained.Store(oldest)

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
	return p.readMeta(metaBlockKey)
}

// readMeta reads an 8-byte big-endian meta value, reporting whether it was
// present. An absent value reads as 0.
func (p *PebbleKVS) readMeta(key []byte) (uint64, bool, error) {
	raw, err := p.db.Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if len(raw) != 8 {
		return 0, false, fmt.Errorf("corrupt meta value for %q: got %d bytes, want 8", key, len(raw))
	}
	return binary.BigEndian.Uint64(raw), true, nil
}

// commitBlock applies a single block's writes (possibly none) and advances the
// persisted checkpoint to blockNum, in one atomic pebble batch.
//
// Replays are handled as LightKVS handles them. A redelivery of the tip is
// verified rather than skipped: versions are derived from current state, so
// re-applying the block would double-bump them and the committer would reject
// later transactions on those keys. A shared synchronizer always redelivers
// identical content on resume, so an identical replay is a no-op and a differing
// one a loud error. Anything older than the tip is skipped unverified — its
// history may already be pruned, leaving nothing to check it against.
func (p *PebbleKVS) commitBlock(blockNum uint64, updates []KeyValueVersion) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	if head := p.currentBlock.Load(); p.hasCheckpoint.Load() && blockNum <= head {
		if blockNum < head {
			pebbleLogger.Debugf("skipping already-applied block %d (checkpoint at %d)", blockNum, head)
			return nil
		}
		return p.verifyTipReplay(updates)
	}

	batch := p.db.NewBatch()
	defer batch.Close()

	// nextVersion holds the version to assign to the *next* write of each key
	// within this batch. VersionedDB computes COALESCE(MAX(version)+1, 0) live
	// per insert, so successive writes to the same key in one block receive
	// consecutive versions (fabric-x-sdk state.VersionedDB.UpdateWorldState).
	// We mirror that exactly, so the highest-tx write wins the read with the
	// highest version, matching the committer's worldstate that the fabric-x
	// MVCC read-set is validated against.
	nextVersion := make(map[string]uint64, len(updates))

	for i := range updates {
		u := &updates[i]

		version, seen := nextVersion[u.Key]
		if !seen {
			// First write to this key in this block: seed the version from committed
			// state and retain the record being superseded. Later writes in the same
			// block supersede a record this block created, which no in-window read can
			// ask for (time travel is block-granular), so they only bump the version.
			prev, raw, err := currentRecord(p.db, u.Key)
			if err != nil {
				return fmt.Errorf("failed to read current record for %q: %w", u.Key, err)
			}
			if prev != nil {
				version = prev.Version + 1
				if err := stageHistory(batch, blockNum, u.Key, prev, raw); err != nil {
					return err
				}
			}
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

		if err := batch.Put(dataKey(u.Key), value); err != nil {
			return fmt.Errorf("failed to stage write for %q: %w", u.Key, err)
		}
	}

	// Prune in the same batch, so a crash cannot leave the index describing
	// entries that are already gone. readerMu is held from here through the
	// marker being published below, so a reader cannot be admitted against a
	// window this commit is about to narrow.
	p.readerMu.Lock()
	defer p.readerMu.Unlock()

	oldest, err := p.pruneLocked(batch, blockNum)
	if err != nil {
		return err
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
	if oldest > 0 {
		p.oldestRetained.Store(oldest)
	}
	return nil
}

// verifyTipReplay checks a redelivery of the tip against what is stored, erroring
// if the net effect of updates differs from it.
//
// Only the last write per key is compared, as LightKVS's verifyReplay does: a
// block's earlier writes to the same key are superseded by it and are not stored
// separately, so that is the granularity at which state can be checked. Like
// LightKVS, this walks the redelivery's keys, so it does not notice a redelivery
// that *omits* a key the original block wrote.
func (p *PebbleKVS) verifyTipReplay(updates []KeyValueVersion) error {
	final := make(map[string]KeyValueVersion, len(updates))
	for _, u := range updates {
		final[u.Key] = u
	}

	for key, u := range final {
		stored, _, err := currentRecord(p.db, key)
		if err != nil {
			return fmt.Errorf("failed to read stored record for %q: %w", key, err)
		}
		if stored == nil || stored.BlockNum != u.BlockNum {
			return fmt.Errorf(
				"missing write for %q at block=%d: the block is already checkpointed but this write was never committed for it",
				key, u.BlockNum)
		}
		if stored.IsDelete != u.IsDelete || !bytes.Equal(stored.Value, u.Value) || stored.TxID != u.TxID {
			return fmt.Errorf(
				"conflicting write for %q at block=%d: existing (is_delete=%v tx_id=%s) differs from replayed (is_delete=%v tx_id=%s)",
				key, u.BlockNum, stored.IsDelete, stored.TxID, u.IsDelete, u.TxID)
		}
	}
	return nil
}

// stageHistory retains the record a write supersedes and indexes it under the
// block doing the superseding, so pruneLocked can later delete both.
func stageHistory(batch ethdb.Batch, blockNum uint64, fullKey string, prev *blocks.WriteRecord, raw []byte) error {
	histKey := historyKey(fullKey, prev.BlockNum, prev.TxNum)
	if err := batch.Put(histKey, raw); err != nil {
		return fmt.Errorf("failed to stage history for %q: %w", fullKey, err)
	}
	if err := batch.Put(indexKey(blockNum, histKey), nil); err != nil {
		return fmt.Errorf("failed to stage history index for %q: %w", fullKey, err)
	}
	return nil
}

// pruneLocked deletes the history entries that leave the window when blockNum is
// committed, along with the index entries naming them, staging both into the
// caller's batch. It returns the new oldest answerable block, or 0 to leave the
// bound unchanged.
//
// An entry created at block c answers reads below c, so with a readable window of
// [blockNum-historySize, blockNum] everything created at or below
// blockNum-historySize-1 is unreachable and goes.
//
// The caller must hold readerMu.
func (p *PebbleKVS) pruneLocked(batch ethdb.Batch, blockNum uint64) (uint64, error) {
	if blockNum < p.historySize+1 {
		return 0, nil // window not full yet; nothing has aged out
	}
	cutoff := blockNum - p.historySize - 1

	// Never cut into what an open reader still needs.
	if pin := p.lowestPinnedLocked(); pin < cutoff {
		cutoff = pin
	}

	// Walk the index in block order — its keys are prefixIndex|be64(block)|..., so
	// key order is block order — rather than probing the cutoff alone: delivered
	// block numbers are not guaranteed contiguous, and a gap would leak entries.
	it := p.db.NewIterator([]byte{prefixIndex}, nil)
	defer it.Release()

	for it.Next() {
		block, histKey, err := parseIndexKey(it.Key())
		if err != nil {
			return 0, err
		}
		if block > cutoff {
			break
		}
		// Both deletes copy the key into the batch, so advancing the iterator after
		// is safe.
		if err := batch.Delete(histKey); err != nil {
			return 0, fmt.Errorf("failed to stage history delete for block %d: %w", block, err)
		}
		if err := batch.Delete(it.Key()); err != nil {
			return 0, fmt.Errorf("failed to stage index delete for block %d: %w", block, err)
		}
	}
	if err := it.Error(); err != nil {
		return 0, fmt.Errorf("failed to scan history index: %w", err)
	}

	// Record how far history reaches, in the same batch, so the bound survives a
	// restart. It only moves forward: a reader pin or a larger historySize lowers
	// the cutoff, and following it back down would re-accept reads whose entries
	// are already gone.
	oldest := cutoff + 1
	if oldest <= p.oldestRetained.Load() {
		return 0, nil
	}
	if err := batch.Put(metaOldestKey, u64be(oldest)); err != nil {
		return 0, fmt.Errorf("failed to stage oldest-retained marker: %w", err)
	}
	return oldest, nil
}

// lowestPinnedLocked returns the lowest height an open reader is pinned to, or
// math.MaxUint64 when there are none. The caller must hold readerMu.
func (p *PebbleKVS) lowestPinnedLocked() uint64 {
	lowest := uint64(math.MaxUint64)
	for bn := range p.openReaders {
		if bn < lowest {
			lowest = bn
		}
	}
	return lowest
}

// releaseReader drops a reader's pin, letting prune advance past its height.
func (p *PebbleKVS) releaseReader(bn uint64) {
	p.readerMu.Lock()
	defer p.readerMu.Unlock()
	if n := p.openReaders[bn]; n > 1 {
		p.openReaders[bn] = n - 1
	} else {
		delete(p.openReaders, bn)
	}
}

// NewSnapshot returns a read view of the store as of blockNumber. nil means the
// latest committed block at call time, pinning the view so later commits are
// excluded. A non-nil value is that exact height (including 0 for genesis), and
// must be inside the retained window: an older block is refused rather than
// answered with a newer value, as LightKVS refuses a block evicted from its ring.
//
// The view holds history back at its height until Close, so readers must close.
func (p *PebbleKVS) NewSnapshot(blockNumber *uint64) (execution.ReadStore, error) {
	// Resolve the height and register the pin under the lock a commit holds from
	// its prune through publishing its checkpoint. Sampling head outside it would
	// let a commit prune the history this view needs before the pin is in place.
	p.readerMu.Lock()
	defer p.readerMu.Unlock()

	// A height at or past the tip is served by the tip, matching LightKVS. Pinning
	// the requested height instead would leave the view open to later commits,
	// since isolation is re-derived per read from the pinned height.
	head := p.currentBlock.Load()
	bn := head
	if blockNumber != nil && *blockNumber < head {
		bn = *blockNumber
	}

	if oldest := p.oldestRetained.Load(); bn < oldest {
		return nil, fmt.Errorf("snapshot not found for block number %d (retained history starts at %d, head is %d)",
			bn, oldest, head)
	}
	p.openReaders[bn]++

	return &pebbleSnapshot{
		db:        p.db,
		owner:     p,
		lastBlock: bn,
	}, nil
}

// Get implements blocks.RecordGetter, returning the record for (namespace, key) at the
// latest committed block, or nil if no such record exists. For a read at a specific
// block, take a NewSnapshot instead.
func (p *PebbleKVS) Get(namespace, key string) (*blocks.WriteRecord, error) {
	r, err := p.NewSnapshot(nil)
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
		collectWrites(&updates, tx.NsRWS, b.Number, uint64(tx.Number), tx.ID, tx.Valid())
	}
	return p.commitBlock(b.Number, updates)
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
// implements execution.ReadStore.
//
// Records are overwritten in place, so isolation is re-derived per read rather
// than being inherent to the layout (see Get). The history those reads fall back
// to is held at this view's height by the pin NewSnapshot took and Close drops,
// so a reader that is never closed holds history back.
type pebbleSnapshot struct {
	db *gethpebble.Database
	// owner is notified on Close so pruning can resume below this view's height.
	owner *PebbleKVS
	// lastBlock is the height this view is pinned to.
	lastBlock uint64
	// closed is atomic because Close may be called concurrently, and releasing the
	// pin twice would drop one another reader at the same height still holds.
	closed atomic.Bool
}

// Get returns the record for (namespace, key) as of the snapshot's block, or
// nil if none exists at or before it. Tombstone records are returned as-is
// (IsDelete=true); the execution layer treats them as absent.
func (s *pebbleSnapshot) Get(namespace, key string) (*blocks.WriteRecord, error) {
	if s.closed.Load() {
		return nil, errors.New("reader is closed")
	}

	fullKey := namespace + ":" + key

	// The common case is one point lookup. The block check is not an optimisation:
	// the current record is overwritten in place, so it can belong to a block
	// committed after this view was pinned, and the read-set built here is
	// MVCC-validated by the committer.
	rec, _, err := currentRecord(s.db, fullKey)
	if err != nil {
		return nil, err
	}
	if rec != nil && rec.BlockNum <= s.lastBlock {
		rec.Namespace, rec.Key = namespace, key
		return rec, nil
	}

	// The current record is newer than this view, or absent. Whichever block
	// superseded the one we want retained it in the same batch.
	it := s.db.NewIterator(historyPrefix(fullKey), u64be(^s.lastBlock))
	defer it.Release()

	if it.Next() {
		hrec, err := decodeRecord(it.Value())
		if err != nil {
			return nil, err
		}
		hrec.Namespace, hrec.Key = namespace, key
		return hrec, nil
	}
	// Nothing at or below lastBlock: the key did not exist yet at that height.
	return nil, it.Error()
}

// Close releases the snapshot. After Close the snapshot cannot be used.
func (s *pebbleSnapshot) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil // already closed; the pin is released exactly once
	}
	s.owner.releaseReader(s.lastBlock)
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

// currentRecord returns the record stored for fullKey together with the raw bytes
// it was decoded from (which history retains verbatim), or nils if the key has
// never been written. One point lookup: a key holds exactly one record.
func currentRecord(db *gethpebble.Database, fullKey string) (*blocks.WriteRecord, []byte, error) {
	raw, err := db.Get(dataKey(fullKey))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	rec, err := decodeRecord(raw)
	if err != nil {
		return nil, nil, err
	}
	return rec, raw, nil
}

// dataKey returns the key holding a state key's current record.
func dataKey(fullKey string) []byte {
	return keyForState(prefixData, fullKey)
}

// historyPrefix returns the per-key prefix under which superseded records live.
func historyPrefix(fullKey string) []byte {
	return keyForState(prefixHistory, fullKey)
}

// keyForState builds prefix | be32(len(fullKey)) | fullKey. The returned slice has
// len == cap so geth's NewIterator, which does append(prefix, start...),
// reallocates instead of clobbering our buffer.
func keyForState(prefix byte, fullKey string) []byte {
	b := make([]byte, 0, 1+4+len(fullKey))
	b = append(b, prefix)
	b = binary.BigEndian.AppendUint32(b, uint32(len(fullKey)))
	return append(b, fullKey...)
}

// historyKey returns the key for one superseded record, with (block, tx) encoded
// descending so a forward scan from be64(^N) lands on the highest version at or
// below N in one Next().
func historyKey(fullKey string, block, tx uint64) []byte {
	b := historyPrefix(fullKey)
	b = binary.BigEndian.AppendUint64(b, ^block)
	return binary.BigEndian.AppendUint64(b, ^tx)
}

// indexKey names a history entry under the block that created it. The history key
// is carried in the index key itself, so the index needs no value and pruning
// needs no decoding.
func indexKey(block uint64, histKey []byte) []byte {
	b := make([]byte, 0, 1+8+len(histKey))
	b = append(b, prefixIndex)
	b = binary.BigEndian.AppendUint64(b, block)
	return append(b, histKey...)
}

// parseIndexKey splits an index key back into its block and history key.
func parseIndexKey(k []byte) (uint64, []byte, error) {
	if len(k) <= 1+8 {
		return 0, nil, fmt.Errorf("corrupt history index key of length %d", len(k))
	}
	return binary.BigEndian.Uint64(k[1:9]), k[9:], nil
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
