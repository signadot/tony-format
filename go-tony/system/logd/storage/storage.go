package storage

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/patches"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/seq"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// CommitNotification contains information about a committed patch.
// This is sent to any registered CommitNotifier after a successful commit.
type CommitNotification struct {
	Commit    int64    // The commit number
	TxSeq     int64    // Transaction sequence number
	Timestamp string   // ISO8601 timestamp
	KPaths    []string // Top-level kpaths affected by this commit
	Patch     *ir.Node // The merged patch that was committed
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

	// head is the baseline document at headCommit, kept so a CAS precondition can be
	// evaluated without materializing the whole document per conditional write. All
	// three fields are owned by commitMu: read and written only while it is held.
	//
	// headSeeded, not head != nil, is what says the head is usable: empty state reads
	// back as a nil document, so an empty log would otherwise look permanently unseeded
	// and never start stepping. Nothing may mutate head — see stepHead.
	head       *ir.Node
	headCommit int64
	headSeeded bool

	sequence *seq.Seq

	dLog *dlog.DLog

	index          *index.Index
	indexPersister *IndexPersister

	// tick holds the published commit watermark and the ordered notification fan-out.
	// Created in Open once the watermark has been reconciled against the log.
	tick *tick

	txStore        tx.Store      // Transaction store (in-memory for now, can be swapped for disk-based)
	txTimeout      time.Duration // Timeout for transaction participants to join (0 = no timeout)
	logger         *slog.Logger
	schemaResolver api.SchemaResolver // Optional schema resolver for !key indexed arrays

	// Schema state - derived from log entries during replay.
	// Schema changes are stored in dlog entries and always occur at snapshot boundaries.
	schema *storageSchema

	// Compaction config - if set, Compact() is called after SwitchDLog
	compactionConfig *CompactionConfig

	// durability decides whether the commit path fsyncs. Read under commitMu (the
	// commit path) or by the accessors; set at configuration time, before serving.
	durability Durability

	// scopeOverlay turns on the overlay read path (SPIKE, see scope_overlay.go).
	scopeOverlay bool

	// replayFloor is the highest commit whose delta history compaction has removed.
	// See replay_floor.go. Read on the replay path, raised by Compact.
	replayFloor atomic.Int64
}

// Open opens or creates a Storage instance with the given root directory.
// The root directory will be created if it doesn't exist.
// If logger is nil, slog.Default() will be used.
func Open(root string, logger *slog.Logger) (*Storage, error) {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Storage{
		sequence: seq.NewSeq(root),

		txStore: tx.NewInMemoryTxStore(),
		index:   index.NewIndex(""),
		logger:  logger,
		schema:  newStorageSchema(),
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
	s.indexPersister = NewIndexPersister(s.sequence.Root, s.index, DefaultIndexPersistInterval, logger)
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

// ReadStateAt reads the state at a specific commit count.
// scopeID controls the view: nil = baseline only; non-nil = the scope's copy-on-write
// overlay. A scope is a LIVE OVERLAY, not a frozen branch: the scoped view at commit C
// is the baseline state at C with the scope's OWN writes replayed on top. See
// readScopedStateAt and issue eagjggjdh12ksg00bsn0.
//
// kp does NOT narrow the result: log entries are whole-document patches, so what comes
// back is the document, root-rooted — a SUPERSET of kp's subtree. Callers trim it (see
// session.go handleMatch, scopedDocAt). kp is kept in the signature because it is the
// read's declared subject and the natural hook for a future path-scoped read, but it is
// deliberately not used to pick the snapshot base or the patch range: doing so silently
// returned no snapshot for every non-root read (issue bvm163tyh12krwcqcsn0), and applied
// each entry once per level of kp.
func (s *Storage) ReadStateAt(kp string, commit int64, scopeID *string) (*ir.Node, error) {
	if scopeID != nil {
		return s.readScopedStateAt(commit, scopeID)
	}
	return s.readBaselineStateAt(commit)
}

// readBaselineStateAt reads baseline state at commit: the most recent baseline
// snapshot plus baseline patches applied from that point forward.
//
// The patch range is taken at the document ROOT, not at kp. Every entry is indexed at
// the root as well as at each path inside it (index.indexPatchRec starts at ""), so the
// root range is already the complete set of entries in the range — and it is the set
// createSnapshot itself applies. LookupRange(kp) returns that same set PLUS a repeat of
// each entry for every level of kp the entry also touches, and the applier paid to apply
// every repeat: reading "demo.x.hot" from a log written entirely at that path applied
// each entry four times (root, demo, demo.x, demo.x.hot), costing ~5x a read of a path
// written once over the same log. The result was the same only because merging a whole
// document twice is a no-op; it is not a property to rely on.
func (s *Storage) readBaselineStateAt(commit int64) (*ir.Node, error) {
	baseReader, startCommit, err := s.findSnapshotBaseReader(commit)
	if err != nil {
		return nil, err
	}
	defer baseReader.Close()

	segments := s.index.LookupRange("", &startCommit, &commit, nil)
	patchNodes, err := s.patchNodesFromSegments(segments, nil)
	if err != nil {
		return nil, err
	}
	return applyPatchesToBase(baseReader, patchNodes)
}

// readScopedStateAt implements copy-on-write scope reads. The scoped view at commit C
// is the baseline state at C (same commit bound) with the scope's OWN patches applied
// on top, verbatim. It is computed in a SINGLE apply pass: the baseline snapshot as
// base, then all baseline patches (commit order), then this scope's patches (commit
// order). Applying the scope's writes last makes them sticky over baseline — a later
// baseline write to a leaf the scope has written is shadowed, while baseline writes
// elsewhere still show through — and replaying real patches (not a materialized
// overlay) keeps op semantics durable (notably !key identity merges).
//
// The single pass matters: materializing baseline first and re-applying the scope
// patches over it round-trips through node<->events, and that round-trip mis-handles
// numeric-string field keys (e.g. path "users.1"), so the scope patch fails to align
// with the base and is dropped. Keeping every patch in one apply pass gives every
// patch the same path computation.
//
// The scope layer is deliberately NOT read from a scope snapshot: materialized scope
// snapshots resolve !key away and are unsound here (see issue eagjggjdh12ksg00bsn0;
// bounded op-preserving compaction is tracked in 5hmq80f3h12krh1mbsn0).
func (s *Storage) readScopedStateAt(commit int64, scopeID *string) (*ir.Node, error) {
	// SPIKE (docs/scope_overlay_plan.md): with an overlay in the log, the scope layer is
	// that overlay plus only what the scope has written since -- instead of every scope
	// patch ever. Off by default; see EnableScopeOverlay.
	if s.scopeOverlay {
		return s.readScopedStateAtOverlay(commit, scopeID)
	}
	return s.readScopedStateAtReplay(commit, scopeID)
}

// readScopedStateAtReplay is the definition: every scope patch, replayed. It stays the
// oracle the overlay path is checked against, and the source the overlay is built from.
func (s *Storage) readScopedStateAtReplay(commit int64, scopeID *string) (*ir.Node, error) {
	baseReader, startCommit, err := s.findSnapshotBaseReader(commit)
	if err != nil {
		return nil, err
	}
	defer baseReader.Close()

	// Baseline patches from the snapshot forward. Taken at the root for the same reason
	// as readBaselineStateAt: the root range is the complete, non-repeating entry set.
	baseSegments := s.index.LookupRange("", &startCommit, &commit, nil)
	patchNodes, err := s.patchNodesFromSegments(baseSegments, nil)
	if err != nil {
		return nil, err
	}

	// This scope's own patches over the full [0, commit] range, applied last.
	scopeSegments := s.index.LookupRange("", nil, &commit, scopeID)
	scopePatches, err := s.patchNodesFromSegments(scopeSegments, scopeID)
	if err != nil {
		return nil, err
	}
	patchNodes = append(patchNodes, scopePatches...)

	return applyPatchesToBase(baseReader, patchNodes)
}

// readScopedStateAtOverlay reads the scope layer as overlay(T) plus the scope's patches
// above T. With no overlay yet it is exactly the replay path, so enabling the flag on a
// store that has never had one written changes nothing.
func (s *Storage) readScopedStateAtOverlay(commit int64, scopeID *string) (*ir.Node, error) {
	// A keyed path anywhere in the scope means the overlay cannot answer for it; see
	// scopeHasKeyedPaths. Replay instead -- slower, and right.
	if s.scopeHasKeyedPaths(*scopeID) {
		return s.readScopedStateAtReplay(commit, scopeID)
	}
	ov := s.latestOverlay(*scopeID, commit)
	if ov == nil {
		return s.readScopedStateAtReplay(commit, scopeID)
	}

	baseReader, startCommit, err := s.findSnapshotBaseReader(commit)
	if err != nil {
		return nil, err
	}
	defer baseReader.Close()

	baseSegments := s.index.LookupRange("", &startCommit, &commit, nil)
	patchNodes, err := s.patchNodesFromSegments(baseSegments, nil)
	if err != nil {
		return nil, err
	}

	// The overlay first -- it is the scope's ownership as of its own commit -- then only
	// what the scope has written since. Both still apply after every baseline patch, which
	// is what makes a scope write shadow a later baseline one.
	overlayEntry, err := s.dLog.ReadEntryAt(dlog.LogFileID(ov.LogFile), ov.LogPosition, ov.LogFileGeneration)
	if err != nil {
		return nil, fmt.Errorf("failed to read scope overlay: %w", err)
	}
	if overlayEntry.Patch != nil {
		patchNodes = append(patchNodes, overlayEntry.Patch)
	}

	after := ov.EndCommit
	for _, seg := range s.index.LookupRange("", &after, &commit, scopeID) {
		if seg.ScopeID == nil || *seg.ScopeID != *scopeID || isOverlaySegment(seg) {
			continue
		}
		if seg.StartCommit == seg.EndCommit || seg.EndCommit <= ov.EndCommit {
			continue
		}
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			return nil, fmt.Errorf("failed to read scope patch: %w", err)
		}
		if entry.Patch != nil {
			patchNodes = append(patchNodes, entry.Patch)
		}
	}

	return applyPatchesToBase(baseReader, patchNodes)
}

// patchNodesFromSegments reads patch nodes from segments in commit order, skipping
// snapshots. If scopeID is non-nil, only that scope's segments are kept (baseline and
// other scopes dropped); if nil, segments are taken as filtered by the caller.
func (s *Storage) patchNodesFromSegments(segments []index.LogSegment, scopeID *string) ([]*ir.Node, error) {
	var patchNodes []*ir.Node
	for _, seg := range segments {
		// Skip snapshots (StartCommit == EndCommit).
		if seg.StartCommit == seg.EndCommit {
			continue
		}
		// Scope layer: keep only this scope's patches, op-preserving.
		if scopeID != nil && (seg.ScopeID == nil || *seg.ScopeID != *scopeID) {
			continue
		}
		// An overlay is a scope-tagged patch entry, so it looks exactly like one of the
		// scope's own writes here. It is not: it SUBSUMES them, and the replay path is
		// the definition the overlay is checked against. Left in, a store that ever had
		// an overlay written would replay it as an extra patch, and -- worse -- the
		// differential would be comparing two paths that both consume it. Only
		// readScopedStateAtOverlay applies one, and it does so explicitly.
		if isOverlaySegment(seg) {
			continue
		}
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			return nil, fmt.Errorf("failed to read patch entry: %w", err)
		}
		if entry.Patch == nil {
			continue
		}
		patchNodes = append(patchNodes, entry.Patch)
	}
	return patchNodes, nil
}

// applyPatchesToBase applies patchNodes onto baseReader via the streaming processor
// and materializes the result as an ir.Node (nil for empty state).
func applyPatchesToBase(baseReader stream.EventReader, patchNodes []*ir.Node) (*ir.Node, error) {
	eventBuffer := &bytes.Buffer{}
	sink := stream.NewBufferEventSink(eventBuffer)
	applier := patches.NewStreamingProcessor()

	if err := applier.ApplyPatches(baseReader, patchNodes, sink); err != nil {
		return nil, fmt.Errorf("failed to apply patches: %w", err)
	}

	var events []stream.Event
	eventReader := stream.NewBinaryEventReader(eventBuffer)
	for {
		evt, err := eventReader.ReadEvent()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read event: %w", err)
		}
		events = append(events, *evt)
	}

	if len(events) == 0 {
		return nil, nil
	}
	node, err := stream.EventsToNode(events)
	if err != nil {
		return nil, err
	}
	tx.StripPatchRootTagRecursive(node)
	return node, nil
}

// persistedIndexStale reports whether the loaded index disagrees with the restored dlog
// generation for any segment — the signature of a compaction whose file swap became durable
// but whose index (and its new positions) did not. In a consistent state every segment for a
// log file carries that file's current generation.
func (s *Storage) persistedIndexStale() bool {
	for _, seg := range s.index.LookupRangeAll("", nil, nil) {
		if seg.LogFileGeneration != s.dLog.GetGeneration(dlog.LogFileID(seg.LogFile)) {
			return true
		}
	}
	return false
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

	// Load or rebuild index
	indexPath := filepath.Join(s.sequence.Root, "index.gob")
	idx, maxCommit, err := index.LoadIndexWithMetadata(indexPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to load index: %w", err)
		}
		idx = index.NewIndex("")
		maxCommit = -1
	}
	s.index = idx

	// Guard against a persisted index left inconsistent with the logs by a compaction that
	// did not complete durably: if any segment's recorded log-file generation disagrees with
	// the generation restored from dlog.state, the persisted positions are stale for the
	// actual on-disk layout. Discard the persisted index and rebuild from the logs, which
	// reflect the true current layout (issue 656g8yt5).
	if s.persistedIndexStale() {
		s.logger.Warn("persisted index generation mismatch after restart; rebuilding index from logs")
		s.index = index.NewIndex("")
		maxCommit = -1
	}

	// Rebuild index from logs starting at maxCommit+1
	if err := index.Build(s.index, s.dLog, maxCommit); err != nil {
		return fmt.Errorf("failed to rebuild index: %w", err)
	}

	// Replay schema state from log entries
	if err := s.replaySchemaState(); err != nil {
		return fmt.Errorf("failed to replay schema state: %w", err)
	}

	// Save index with updated maxCommit
	currentMaxCommit := s.getIndexMaxCommit()
	if currentMaxCommit >= 0 {
		if err := index.StoreIndexWithMetadata(indexPath, s.index, currentMaxCommit); err != nil {
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
	for _, seg := range s.index.LookupRangeAll("", nil, nil) {
		if seg.EndCommit > commit {
			commit = seg.EndCommit
		}
		if seg.EndTx > txSeq {
			txSeq = seg.EndTx
		}
	}
	return commit, txSeq
}

// NewTx creates a new transaction with the specified number of participants.
// Returns a transaction that participants can get via GetTx or get a patcher via NewPatcher().
//
// Example usage (typical pattern for parallel HTTP handlers):
//
//	// Create transaction
//	tx, err := storage.NewTx(participantCount, scope)
//	if err != nil {
//	    // handle error
//	}
//
//	// Each participant gets their own patcher handle
//	patcher := tx.NewPatcher(kp, m, p)
//	result := patcher.WaitForCompletion()
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

func (s *Storage) Close() error {
	// Stop transaction cleanup goroutine
	s.txStore.Close()

	// Deliver whatever the dispatcher still holds, then stop it, before the log it
	// describes goes away.
	s.tick.close()

	// Wait for any pending index persist
	if s.indexPersister != nil {
		s.indexPersister.Close()
	}

	indexPath := filepath.Join(s.sequence.Root, "index.gob")
	currentMaxCommit := s.getIndexMaxCommit()
	if currentMaxCommit >= 0 {
		if err := index.StoreIndexWithMetadata(indexPath, s.index, currentMaxCommit); err != nil {
			return fmt.Errorf("failed to save index: %w", err)
		}
	}

	if err := s.dLog.Close(); err != nil {
		return fmt.Errorf("failed to close dlog: %w", err)
	}

	return nil
}

func (s *Storage) getIndexMaxCommit() int64 {
	// Use LookupRangeAll to get all segments regardless of scope
	segments := s.index.LookupRangeAll("", nil, nil)
	var maxCommit int64 = -1
	for _, seg := range segments {
		if seg.EndCommit > maxCommit {
			maxCommit = seg.EndCommit
		}
	}
	return maxCommit
}

// GetTx gets an existing transaction by transaction ID.
// This is the primary way participants coordinate - they all receive the same
// transaction ID and get the same transaction.
//
// Example:
//
//	// Multiple parallel HTTP handlers all receive the same txID
//	tx, err := storage.GetTx(txID)
//	if err != nil {
//	    // handle error
//	}
//
//	// Each participant gets their own patcher handle
//	patcher := tx.NewPatcher(kp, m, p)
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

// GetTxTimeout returns the current transaction timeout.
func (s *Storage) GetTxTimeout() time.Duration {
	return s.txTimeout
}

// SetSchemaResolver sets the schema resolver for !key indexed arrays.
// The resolver provides schema for each scope (nil scope = baseline).
func (s *Storage) SetSchemaResolver(resolver api.SchemaResolver) {
	s.schemaResolver = resolver
}

// GetSchemaResolver returns the current schema resolver, or nil if none.
func (s *Storage) GetSchemaResolver() api.SchemaResolver {
	return s.schemaResolver
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

// GetCompactionConfig returns the current compaction configuration.
func (s *Storage) GetCompactionConfig() *CompactionConfig {
	return s.compactionConfig
}

// schemaForScope returns the schema that decides what keys an array.
//
// The PERSISTED active schema is the authority. It is in the log, it moves only through
// the migration path (pending -> active at a snapshot boundary), and it is what the
// pending dual-write index is already keyed from — so live and pending keying now come
// from one source instead of two.
//
// The resolver is the bootstrap only: a store with no schema of its own yet still keys
// from the one its configuration names, which is how every existing store behaves. The
// end state is that a configured schema is COMMITTED rather than consulted, at which point
// this fallback goes away; until then a store that has never been given a schema through
// StartMigration/CompleteMigration keeps working exactly as before.
//
// scopeID is accepted and ignored. The persisted schema is per-store, so every scope keys
// the way baseline does — which is the rule the plan wanted for the per-scope dimension,
// arrived at by construction rather than by a policy nobody could point at a user for.
func (s *Storage) schemaForScope(scopeID *string) *api.Schema {
	if persisted := s.schema.GetActiveParsed(); persisted != nil {
		return persisted
	}
	if s.schemaResolver == nil {
		return nil
	}
	return s.schemaResolver.GetSchema(scopeID)
}

// DeleteScope removes all index entries for a scope.
// The actual log entries remain (append-only), but become inaccessible.
func (s *Storage) DeleteScope(scopeID string) error {
	count := s.index.DeleteScope(scopeID)
	if count == 0 {
		return fmt.Errorf("scope %q not found or has no data", scopeID)
	}
	return nil
}

// GetActiveSchema returns the current active schema and the commit where it was set.
// Returns nil schema and 0 commit if schemaless.
func (s *Storage) GetActiveSchema() (*ir.Node, int64) {
	return s.schema.GetActive()
}

// GetPendingSchema returns the pending schema and commit if a migration is in progress.
// Returns nil, 0 if no migration is in progress.
func (s *Storage) GetPendingSchema() (*ir.Node, int64) {
	return s.schema.GetPending()
}

// HasPendingMigration returns true if a schema migration is in progress.
func (s *Storage) HasPendingMigration() bool {
	return s.schema.HasPending()
}
