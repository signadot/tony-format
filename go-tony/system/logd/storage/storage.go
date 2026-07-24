package storage

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
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

	sequence *seq.Seq

	dLog *dlog.DLog

	index          *index.Index
	indexPersister *IndexPersister

	txStore        tx.Store      // Transaction store (in-memory for now, can be swapped for disk-based)
	txTimeout      time.Duration // Timeout for transaction participants to join (0 = no timeout)
	logger         *slog.Logger
	notifier       CommitNotifier     // Optional callback for commit notifications
	schemaResolver api.SchemaResolver // Optional schema resolver for !key indexed arrays

	// Schema state - derived from log entries during replay.
	// Schema changes are stored in dlog entries and always occur at snapshot boundaries.
	schema *storageSchema

	// Compaction config - if set, Compact() is called after SwitchDLog
	compactionConfig *CompactionConfig
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

	return s, nil
}

// ActiveLogSize returns the size of the currently active log file in bytes.
func (s *Storage) ActiveLogSize() (int64, error) {
	return s.dLog.ActiveLogSize()
}

// GetCurrentCommit returns the current commit number.
func (s *Storage) GetCurrentCommit() (int64, error) {
	s.sequence.Lock()
	defer s.sequence.Unlock()
	state, err := s.sequence.ReadStateLocked()
	if err != nil {
		return 0, err
	}
	commit := state.Commit
	return commit, nil
}

// ReadStateAt reads the state for a given kpath at a specific commit count.
// scopeID controls the view: nil = baseline only; non-nil = the scope's copy-on-write
// overlay. A scope is a LIVE OVERLAY, not a frozen branch: the scoped view at commit C
// is the baseline state at C with the scope's OWN writes replayed on top. See
// readScopedStateAt and issue eagjggjdh12ksg00bsn0.
func (s *Storage) ReadStateAt(kp string, commit int64, scopeID *string) (*ir.Node, error) {
	if scopeID != nil {
		return s.readScopedStateAt(kp, commit, scopeID)
	}
	return s.readBaselineStateAt(kp, commit)
}

// readBaselineStateAt reads baseline state at commit: the most recent baseline
// snapshot plus baseline patches applied from that point forward.
func (s *Storage) readBaselineStateAt(kp string, commit int64) (*ir.Node, error) {
	baseReader, startCommit, err := s.findSnapshotBaseReader(kp, commit)
	if err != nil {
		return nil, err
	}
	defer baseReader.Close()

	segments := s.index.LookupRange(kp, &startCommit, &commit, nil)
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
func (s *Storage) readScopedStateAt(kp string, commit int64, scopeID *string) (*ir.Node, error) {
	baseReader, startCommit, err := s.findSnapshotBaseReader(kp, commit)
	if err != nil {
		return nil, err
	}
	defer baseReader.Close()

	// Baseline patches from the snapshot forward.
	baseSegments := s.index.LookupRange(kp, &startCommit, &commit, nil)
	patchNodes, err := s.patchNodesFromSegments(baseSegments, nil)
	if err != nil {
		return nil, err
	}

	// This scope's own patches over the full [0, commit] range, applied last.
	scopeSegments := s.index.LookupRange(kp, nil, &commit, scopeID)
	scopePatches, err := s.patchNodesFromSegments(scopeSegments, scopeID)
	if err != nil {
		return nil, err
	}
	patchNodes = append(patchNodes, scopePatches...)

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

	// Initialize sequence number file if it doesn't exist
	seqFile := filepath.Join(s.sequence.Root, "meta", "seq")
	if _, err := os.Stat(seqFile); os.IsNotExist(err) {
		s.sequence.Lock()
		state := &seq.State{Commit: 0, TxSeq: 0}
		err := s.sequence.WriteStateLocked(state)
		s.sequence.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
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
func (s *Storage) SetCommitNotifier(notifier CommitNotifier) {
	s.notifier = notifier
}

// GetCommitNotifier returns the currently registered commit notifier, or nil if none.
func (s *Storage) GetCommitNotifier() CommitNotifier {
	return s.notifier
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

// schemaForScope returns the schema for a given scope.
// Returns nil if no schema resolver is set.
func (s *Storage) schemaForScope(scopeID *string) *api.Schema {
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
