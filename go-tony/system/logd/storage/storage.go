package storage

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/seq"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// CommitNotification contains information about a committed patch.
// This is sent to any registered CommitNotifier after a successful commit.
// [Storage.Deltas] answers the same type for a replayed commit, with TxSeq and KPaths
// unset.
type CommitNotification struct {
	Commit    int64    // The commit number
	TxSeq     int64    // Transaction sequence number
	Timestamp string   // ISO8601 timestamp
	KPaths    []string // Top-level kpaths affected by this commit
	Patch     *ir.Node // The delta the log stored for the commit, keyed arrays raised (raise.go); the notification's own copy
	ScopeID   *string  // Scope ID (nil = baseline)
}

// CommitNotifier is a callback invoked after each successful commit.
// Implementations must not block - if async processing is needed,
// the notifier should queue the notification and return immediately.
type CommitNotifier func(n *CommitNotification)

// Durability controls when a commit's log record is forced to stable storage.
//
// It trades write latency against the size of the window a machine crash (as opposed
// to a process crash, which the page cache survives) can erase. It does NOT affect
// what a restart can make sense of: an unsynced tail is recovered by the frame scan
// on open, and the commit watermark is reconciled against the log either way
// (reconcileWatermark), so a lost tail costs commits, never their identity.
type Durability int

const (
	// DurabilityOS acknowledges a commit once its record is written to the OS page
	// cache — no fsync on the commit path. This is the default: it is the historical
	// behavior, and the per-write fsync cost is not worth paying for every commit.
	// A machine crash loses whatever the OS had not yet flushed.
	DurabilityOS Durability = iota

	// DurabilitySync fsyncs the log record before the commit is indexed, so a commit
	// that has been acknowledged is on stable storage. Costs one fsync per commit.
	DurabilitySync
)

func (d Durability) String() string {
	switch d {
	case DurabilityOS:
		return "os"
	case DurabilitySync:
		return "sync"
	default:
		return fmt.Sprintf("Durability(%d)", int(d))
	}
}

// Storage provides filesystem-based storage for logd.
type Storage struct {
	// commitMu serializes a commit's read-modify-write — CAS precondition evaluation,
	// commit-number allocation, and log append — so it is atomic w.r.t. other commits.
	// Without it, two conditional patches with the same precondition both evaluate against
	// the same pre-commit state, both pass, and both write (CAS lost update, issue r1w4k6g2).
	// It is deliberately NOT held across the post-commit fan-out notify: the notifier contract
	// is non-blocking, but until that holds everywhere, keeping notify outside the lock ensures
	// a slow watcher can never serialize all commits.
	commitMu sync.Mutex

	// readStats counts what reads at a path did -- narrowed, or read wide and why.
	// A store cannot be asked afterwards, and from outside a fast read and a read
	// that never happened look the same (ap8ddvp2h12krd43gdn0).
	readStats  readStats
	writeStats writeStats

	// unreadable names a log record the rebuild could not deserialize, if there was
	// one. Set at open, read by the report; nil is the ordinary case.
	unreadable *index.Unreadable

	// writeBudget is the largest node the write path builds to verify or lower one
	// write, or to evaluate one precondition: the value at a path the write names, read
	// under this bound. A write whose verification needs more is refused, naming the
	// path and the size, which is the bound charging the client that asked for it
	// (read_write_interface.md; rebuild_plan.md decision 5).
	writeBudget int64

	sequence *seq.Seq

	dLog *dlog.DLog

	index          *index.Index
	indexPersister *IndexPersister

	// tick holds the published commit watermark and the ordered notification fan-out.
	// Created in Open once the watermark has been reconciled against the log.
	tick *tick

	txStore   tx.Store      // Transaction store (in-memory for now, can be swapped for disk-based)
	txTimeout time.Duration // Timeout for transaction participants to join (0 = no timeout)
	logger    *slog.Logger

	// The schemas the store has had, in commit order; the last is in force (schema.go).
	schema *schemaHistory

	// inCommit, when set, is called on the commit path under commitMu, before the commit
	// takes its number: a probe for tests of what a commit in flight looks like from
	// outside (tick_test.go).
	inCommit func()

	// Compaction config - if set, Compact() is called after SwitchDLog
	compactionConfig *CompactionConfig

	// snapMu serializes the writers of the inactive log: the switch, with the root
	// snapshot and the compaction that rewrites the log, and a snapshot of a path.
	snapMu sync.Mutex
	// pathSnap is when a read schedules a snapshot at its path (path_snapshot.go);
	// pathSnapBusy is the one in flight, pathSnapWG what Close waits for, and closing
	// what stops another being scheduled.
	pathSnap     pathSnapshotPolicy
	pathSnapBusy atomic.Bool
	pathSnapWG   sync.WaitGroup
	closing      atomic.Bool

	// durability decides whether the commit path fsyncs. Read under commitMu (the
	// commit path) or by the accessors; set at configuration time, before serving.
	durability Durability

	// lowerAll lowers every write rather than only the ones that need it. Lowering
	// itself is not optional: what the log KEEPS is held to the storage vocabulary,
	// always. See lower.go, and lowerEverything for what this amplifies.
	lowerAll bool

	// replayFloor is the highest commit whose delta history compaction has removed.
	// See replay_floor.go. Read on the replay path, raised by Compact.
	replayFloor atomic.Int64

	// scopeReadsHistoric makes every scoped read fold the scope's whole history from
	// the index instead of the footprint's live statements. Not a mode to run in: it is
	// the reference arm of the footprint differential, the read the footprint is held
	// equal to (scope_plan.md, phase 2).
	scopeReadsHistoric bool
}

// Open opens or creates a Storage instance with the given root directory.
// The root directory will be created if it doesn't exist.
// If logger is nil, slog.Default() will be used.
func Open(root string, logger *slog.Logger) (*Storage, error) {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Storage{
		sequence:    seq.NewSeq(root),
		writeBudget: DefaultWriteBudget,
		pathSnap:    pathSnapshotPolicy{tail: DefaultPathSnapshotTail, bytes: DefaultPathSnapshotBytes},

		txStore: tx.NewInMemoryTxStore(),
		index:   index.NewIndex(""),
		logger:  logger,
		schema:  newSchemaHistory(nil), // init loads the history the index rebuilt
		// Never on outside a test. See lowerEverything.
		lowerAll: os.Getenv("LOGD_LOWERING") == "all",
	}

	dlog, err := dlog.NewDLog(root, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize DLog: %w", err)
	}
	s.dLog = dlog

	if err := s.init(); err != nil {
		return nil, err
	}

	// Create persister after init() since init() may replace s.index
	s.indexPersister = NewIndexPersister(s.index, s.logGenerations, DefaultIndexPersistInterval, logger)
	s.indexPersister.SetLastPersisted(s.getIndexMaxCommit())

	// The tick starts at the reconciled watermark: everything the log holds is already
	// indexed by now, so everything up to it is readable.
	published, err := s.sequence.CurrentSeqState()
	if err != nil {
		return nil, fmt.Errorf("failed to read sequence state: %w", err)
	}
	s.tick = newTick(published.Commit)

	return s, nil
}

// ActiveLogSize returns the size of the currently active log file in bytes.
func (s *Storage) ActiveLogSize() (int64, error) {
	return s.dLog.ActiveLogSize()
}

// DeltaBytesSinceSnapshot returns how many bytes of the active log a read must
// replay on top of the newest snapshot. It is what a size-based snapshot policy
// thresholds; ActiveLogSize is not (see DLog.DeltaBytesSinceSnapshot).
func (s *Storage) DeltaBytesSinceSnapshot() (int64, error) {
	return s.dLog.DeltaBytesSinceSnapshot()
}

// GetCurrentCommit returns the published commit watermark: the highest commit that is
// in the log AND in the index, and so can be read or replayed right now.
//
// It is deliberately NOT the sequence counter. The counter is bumped when a commit is
// allocated, before its entry is written or indexed, so reporting it handed callers a
// commit that did not yet exist for any reader — and a watch that took it as a replay
// target dropped that commit entirely (see tick). The counter remains the allocator;
// this is the reader's view, and it is served from memory rather than off disk.
//
// The watermark can sit ahead of the last entry the log actually holds, when a commit
// was allocated and then failed to write, or when reconcileWatermark restored a counter
// that had run ahead. That is the benign direction: it names a commit that no patch
// occupies, and a read there simply sees the state as of the last commit before it.
func (s *Storage) GetCurrentCommit() (int64, error) {
	return s.tick.current(), nil
}

// isOverlaySegment reports whether seg is a scope overlay -- a cache of a scope's layer
// that logd used to write beside a snapshot. Nothing writes one now; this is what lets a
// log that has them still be read. See projectScope.
func isOverlaySegment(seg index.LogSegment) bool {
	return seg.ScopeID != nil && seg.ScopeOverlay
}

// init initializes the storage directory structure.
func (s *Storage) init() error {
	dirs := []string{
		filepath.Join(s.sequence.Root, "transactions"),
		filepath.Join(s.sequence.Root, "meta"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	// The durable index, or a fresh one when what is there cannot be trusted --
	// index.OpenIndex says why -- and either way the log fills in from where the index
	// stops: the log is the record (index_residency.md).
	idx, manifest, why, err := index.OpenIndex(s.sequence.Root, func(logFile string) int64 {
		return s.dLog.GetGeneration(dlog.LogFileID(logFile))
	})
	if err != nil {
		return fmt.Errorf("failed to open index: %w", err)
	}
	maxCommit := int64(-1)
	if manifest != nil {
		maxCommit = manifest.MaxCommit
	} else if why != "" && why != "no manifest" {
		s.logger.Warn("rebuilding the index from the logs", "reason", why)
	}
	s.index = idx

	// Rebuild index from logs starting at maxCommit+1. A region the walk cannot read is
	// stepped over to the next record that decodes, and an error reading a log stops the
	// walk; neither stops the store, since refusing to open recovers nothing while
	// keeping the system down (t96b5ejqh12krprjghn0). What the walk could not read is
	// recorded (s.unreadable), and reported for as long as the process runs.
	unreadable, err := index.BuildWithLogger(s.index, s.dLog, maxCommit, s.logger)
	if err != nil {
		return fmt.Errorf("failed to rebuild index: %w", err)
	}
	s.unreadable = unreadable
	if unreadable != nil {
		// The loaded index may hold segments pointing into the bad bytes -- it was
		// written by a run which could still read them. They name data no read can
		// produce now, and keeping them fails every read and every write which needs
		// one. What lies behind a region the walk stepped over is kept: the walk read it.
		dropped := 0
		if len(unreadable.Regions) > 0 {
			for _, r := range unreadable.Regions {
				dropped += s.index.DropWithin(r.LogFile, r.From, r.To)
			}
		} else {
			dropped = s.index.DropFrom(unreadable.LogFile, unreadable.Position+1)
		}
		s.logger.Error("dropped index entries pointing into unreadable log bytes; the store is serving what it can read",
			"logFile", unreadable.LogFile, "position", unreadable.Position, "regions", len(unreadable.Regions), "segmentsDropped", dropped)
		unreadable.Dropped = dropped
		// A dropped segment may have been a dominator; what it dominated does not come
		// back on its own, so the footprint is remade from what is left.
		if dropped > 0 {
			s.index.RebuildFootprint()
		}
	}

	// The schemas, as the index's walk of the log noted them (index/schema_history.go).
	s.schema = newSchemaHistory(s.index.SchemaHistory())

	// What the catch-up added is written now, so it is evictable from the start.
	if s.getIndexMaxCommit() >= 0 {
		if err := s.index.Persist(s.logGenerations()); err != nil {
			return fmt.Errorf("failed to save index: %w", err)
		}
	}

	// Bring the sequence counters up to what the log actually contains, and create
	// the file if this is a fresh store.
	if err := s.reconcileWatermark(); err != nil {
		return err
	}

	// How far back delta replay is still exact, as left by past compactions.
	floor, err := loadReplayFloor(s.sequence.Root)
	if err != nil {
		return fmt.Errorf("failed to load replay floor: %w", err)
	}
	s.replayFloor.Store(floor)

	return nil
}

// reconcileWatermark raises the persisted sequence counters to the maxima the log
// holds, so a reopened store never reissues a commit or transaction number.
//
// The counters and the log are two separate files and neither is fsynced on the
// commit path (see Durability), so a crash can leave them disagreeing in either
// direction. Ahead of the log is benign: the unused numbers are a hole, and a hole
// costs nothing because readers address state by commit, not by counting. BEHIND the
// log is corruption: the next commit reuses a number the log already has, and the
// commit number stops naming one state forever — which is the single assumption every
// watch cursor rests on (a client resumes by saying "I have through commit N"). Losing
// meta/seq entirely, with the log intact, restarted the whole sequence from 1.
//
// max() is therefore the only safe direction, and it is safe even when the index
// overstates the log: an index persisted ahead of a lost log tail yields a watermark
// past the last real entry, which is the benign hole again.
//
// The maxima come from the index rather than a log scan because the index has just
// been rebuilt from the log (or loaded and caught up from maxCommit+1), so it already
// reflects every entry — every one of which is indexed at the root.
func (s *Storage) reconcileWatermark() error {
	logCommit, logTxSeq := s.indexWatermarks()

	s.sequence.Lock()
	defer s.sequence.Unlock()

	state, err := s.sequence.ReadStateLocked()
	if err != nil {
		return fmt.Errorf("failed to read sequence state: %w", err)
	}

	raised := false
	if logCommit > state.Commit {
		s.logger.Warn("sequence commit counter is behind the log; raising it to avoid reissuing commit numbers",
			"counter", state.Commit, "log", logCommit)
		state.Commit = logCommit
		raised = true
	}
	if logTxSeq > state.TxSeq {
		state.TxSeq = logTxSeq
		raised = true
	}

	// Write when raised, and on a fresh store so meta/seq exists from the start.
	if _, statErr := os.Stat(s.sequence.StateFilePath()); raised || os.IsNotExist(statErr) {
		if err := s.sequence.WriteStateLocked(state); err != nil {
			return fmt.Errorf("failed to write sequence state: %w", err)
		}
	}
	return nil
}

// indexWatermarks returns the highest commit and transaction sequence the index
// holds, or 0 for each if it is empty. Snapshot segments carry EndTx 0 and are
// simply never the maximum.
//
// Kept apart from getIndexMaxCommit, which reports -1 for an empty index because its
// caller distinguishes "nothing to persist" from "commit 0"; here 0 is the right
// floor, since it is what an unwritten counter already reads as.
func (s *Storage) indexWatermarks() (commit, txSeq int64) {
	commit, txSeq, _ = s.index.MaxCommit()
	return commit, txSeq
}

// NewTx creates a new transaction with the specified number of participants, in the
// view scope names (nil is baseline). Returns a transaction that participants can get
// via GetTx and join through its NewPatcher.
//
// Example usage (typical pattern for parallel HTTP handlers):
//
//	// Create transaction
//	t, err := s.NewTx(participantCount, scope)
//	if err != nil {
//	    // handle error
//	}
//
//	// Each participant gets their own patcher handle, and every one calls Commit
//	patcher, err := t.NewPatcher(&api.Patch{PathData: api.PathData{Path: kp, Data: data}})
//	if err != nil {
//	    // handle error
//	}
//	result := patcher.Commit()
func (s *Storage) NewTx(participantCount int, scope *string) (tx.Tx, error) {
	if participantCount < 1 {
		return nil, fmt.Errorf("participantCount must be at least 1, got %d", participantCount)
	}

	txSeq, err := s.sequence.NextTxSeq()
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction sequence: %w", err)
	}

	state := &tx.State{
		TxID:        txSeq,
		CreatedAt:   time.Now(),
		Timeout:     s.txTimeout,
		Scope:       scope,
		PatcherData: make([]*tx.PatcherData, 0, participantCount),
	}
	ops := &commitOps{s: s}
	res := tx.New(s.txStore, ops, state)

	if err := s.txStore.Put(res); err != nil {
		return nil, fmt.Errorf("failed to store transaction state: %w", err)
	}

	return res, nil
}

// Close shuts the store down: it waits for a snapshot of a path in flight, stops the
// transaction store, delivers the notifications already queued, writes the index whole,
// and closes the logs, syncing them first. Commits must have stopped before it is called.
func (s *Storage) Close() error {
	// No more snapshots get scheduled, and the one in flight lands before the log it
	// writes to goes away.
	s.closing.Store(true)
	s.pathSnapWG.Wait()

	// Stop transaction cleanup goroutine
	s.txStore.Close()

	// Deliver whatever the dispatcher still holds, then stop it, before the log it
	// describes goes away.
	s.tick.close()

	// Wait for any pending index persist
	if s.indexPersister != nil {
		s.indexPersister.Close()
	}

	// The index goes out whole and compact: every region the file lacks written, the
	// records earlier persists superseded dropped, the manifest last.
	if s.getIndexMaxCommit() >= 0 {
		if err := s.index.Rewrite(s.logGenerations()); err != nil {
			return fmt.Errorf("failed to save index: %w", err)
		}
	}
	if err := s.index.Close(); err != nil {
		return fmt.Errorf("failed to close index: %w", err)
	}

	if err := s.dLog.Close(); err != nil {
		return fmt.Errorf("failed to close dlog: %w", err)
	}

	return nil
}

func (s *Storage) getIndexMaxCommit() int64 {
	commit, _, ok := s.index.MaxCommit()
	if !ok {
		return -1
	}
	return commit
}

// logGenerations is what the index records about the logs it was written against: a
// compaction moves entries and bumps the generation, and an index persisted before it
// names positions that no longer hold what it says.
func (s *Storage) logGenerations() map[string]int64 {
	return map[string]int64{
		string(dlog.LogFileA): s.dLog.GetGeneration(dlog.LogFileA),
		string(dlog.LogFileB): s.dLog.GetGeneration(dlog.LogFileB),
	}
}

// IndexCeiling answers the ceiling the index holds resident under, in bytes; 0 is
// unbounded. See SetIndexCeiling.
func (s *Storage) IndexCeiling() int64 {
	if r := s.index.Residency(); r != nil {
		return r.Ceiling()
	}
	return 0
}

// SetIndexCeiling bounds what the index holds resident, in bytes: past it, the least
// recently used regions are evicted to the durable index and paged back on a miss
// (index_residency.md). Zero is unbounded. A ceiling under index.MinIndexCeiling is
// refused: a read at any depth would thrash under it rather than progress.
func (s *Storage) SetIndexCeiling(bytes int64) error {
	if bytes < 0 || (bytes > 0 && bytes < index.MinIndexCeiling) {
		return fmt.Errorf("index ceiling %d is below the floor of %d bytes", bytes, index.MinIndexCeiling)
	}
	// What is evicted has to be gettable back, so the file holds everything first.
	if err := s.index.Persist(s.logGenerations()); err != nil {
		return fmt.Errorf("index ceiling: %w", err)
	}
	s.index.Residency().SetCeiling(bytes)
	return nil
}

// GetTx gets an existing transaction by transaction ID.
// This is the primary way participants coordinate - they all receive the same
// transaction ID and get the same transaction.
//
// Example:
//
//	// Multiple parallel HTTP handlers all receive the same txID
//	t, err := s.GetTx(txID)
//	if err != nil {
//	    // handle error
//	}
//
//	// Each participant gets their own patcher handle
//	patcher, err := t.NewPatcher(&api.Patch{PathData: api.PathData{Path: kp, Data: data}})
//	if err != nil {
//	    // handle error
//	}
//	result := patcher.Commit()
func (s *Storage) GetTx(txID int64) (tx.Tx, error) {
	t, err := s.txStore.Get(txID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction state: %w", err)
	}
	if t == nil {
		return nil, fmt.Errorf("transaction %d not found", txID)
	}
	return t, nil
}

// SetCommitNotifier sets the callback to be invoked after each successful commit.
// Only one notifier can be active at a time - setting a new one replaces the previous.
// Pass nil to disable notifications.
//
// The notifier is called on the tick's dispatcher goroutine, once per commit, in commit
// order — never on the committing goroutine, and never under the commit lock.
func (s *Storage) SetCommitNotifier(notifier CommitNotifier) {
	s.tick.setNotifier(notifier)
}

// GetCommitNotifier returns the currently registered commit notifier, or nil if none.
func (s *Storage) GetCommitNotifier() CommitNotifier {
	return s.tick.getNotifier()
}

// SetTxTimeout sets the timeout for transaction participants to join.
// If not all participants join within this duration, the transaction is aborted
// and waiting participants receive a timeout error.
// Pass 0 to disable timeout (not recommended for production).
func (s *Storage) SetTxTimeout(timeout time.Duration) {
	s.txTimeout = timeout
}

// SetDurability sets when the commit path forces records to stable storage.
// Set it before serving; it is not meant to change under live commits.
func (s *Storage) SetDurability(d Durability) {
	s.durability = d
}

// GetDurability returns the current durability mode.
func (s *Storage) GetDurability() Durability {
	return s.durability
}

// Sync forces all written log records to stable storage. Under the default
// DurabilityOS this is how a caller takes a flush point of its own choosing —
// after a batch of writes, say — without paying an fsync per commit.
func (s *Storage) Sync() error {
	return s.dLog.SyncAll()
}

// SetCompactionConfig sets the compaction configuration.
// If set, Compact() is called automatically after SwitchDLog().
// Pass nil to disable automatic compaction.
func (s *Storage) SetCompactionConfig(config *CompactionConfig) {
	s.compactionConfig = config
}

// CompactionConfig answers the retention policy in force, or nil when the store does not
// compact. It is what the store took, defaults resolved, and not what a caller offered:
// a caller reporting its own half-filled config says a number that is not the one running
// (server.New logs this one).
func (s *Storage) CompactionConfig() *CompactionConfig {
	return s.compactionConfig
}

// schemaForScope returns the schema that decides what keys an array: the store's, which
// is in the log and moves only by a schema commit (SetSchema). A configured schema
// reaches the store the same way, once, when the store has none (BootstrapSchema).
//
// scopeID is accepted and ignored. The schema is per-store, so every scope keys the way
// baseline does — which is the rule the plan wanted for the per-scope dimension, arrived
// at by construction rather than by a policy nobody could point at a user for.
func (s *Storage) schemaForScope(scopeID *string) *api.Schema {
	return s.schema.ActiveParsed()
}

// DefaultWriteBudget is the write budget a store has when its configuration names none.
const DefaultWriteBudget = 128 << 20

// SetWriteBudget sets the largest node the write path builds for one write or
// precondition. Zero or less keeps the default.
func (s *Storage) SetWriteBudget(b int64) {
	if b <= 0 {
		b = DefaultWriteBudget
	}
	s.writeBudget = b
}

// WriteBudget answers the store's write budget.
func (s *Storage) WriteBudget() int64 { return s.writeBudget }

// DeleteScope removes all index entries for a scope, and its footprint.
// The actual log entries remain (append-only), but become inaccessible, until
// compaction drops them beyond its cutoff.
func (s *Storage) DeleteScope(scopeID string) error {
	count := s.index.DeleteScope(scopeID)
	if count == 0 {
		return fmt.Errorf("scope %q not found or has no data", scopeID)
	}
	// Nothing should hold a document for a scope which no longer has any.
	return nil
}
