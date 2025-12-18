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

// Storage provides filesystem-based storage for logd.
type Storage struct {
	sequence *seq.Seq

	dLog *dlog.DLog

	index *index.Index

	txStore  tx.Store     // Transaction store (in-memory for now, can be swapped for disk-based)
	logger   *slog.Logger // Logger for error logging
	switchMu sync.Mutex   // Protects log switching to prevent concurrent switch+snapshot
}

// Open opens or creates a Storage instance with the given root directory.
// The root directory will be created if it doesn't exist.
// umask is applied to directory permissions (e.g., 022 for 0755 -> 0755).
// If logger is nil, slog.Default() will be used.
// compactorConfig is optional - if nil, a default config with divisor 2 and NeverRemove is used.
func Open(root string, umask int, logger *slog.Logger) (*Storage, error) {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Storage{
		sequence: seq.NewSeq(root),

		txStore: tx.NewInMemoryTxStore(),
		index:   index.NewIndex(""),
		logger:  logger,
	}

	dlog, err := dlog.NewDLog(root, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize DLog: %w", err)
	}
	s.dLog = dlog

	if err := s.init(); err != nil {
		return nil, err
	}

	return s, nil
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
// Searches for the most recent snapshot and applies patches from that point forward.
func (s *Storage) ReadStateAt(kp string, commit int64) (*ir.Node, error) {
	// Find most recent snapshot and get base event reader
	baseReader, startCommit, err := s.findSnapshotBaseReader(kp, commit)
	if err != nil {
		return nil, err
	}
	defer baseReader.Close()

	// Get patches from startCommit to commit
	segments := s.index.LookupRange(kp, &startCommit, &commit)

	// Extract patch nodes, filtering out snapshots
	var patchNodes []*ir.Node
	for _, seg := range segments {
		// Skip snapshots (StartCommit == EndCommit)
		if seg.StartCommit == seg.EndCommit {
			continue
		}

		// Read patch from dlog
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition)
		if err != nil {
			return nil, fmt.Errorf("failed to read patch entry: %w", err)
		}
		if entry.Patch == nil {
			continue
		}

		patchNodes = append(patchNodes, entry.Patch)
	}

	// Apply patches using PatchApplier interface
	eventBuffer := &bytes.Buffer{}
	sink := stream.NewBufferEventSink(eventBuffer)
	applier := patches.NewInMemoryApplier()

	if err := applier.ApplyPatches(baseReader, patchNodes, sink); err != nil {
		return nil, fmt.Errorf("failed to apply patches: %w", err)
	}

	// Read events from buffer and convert to ir.Node
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

	// Convert events to ir.Node
	if len(events) == 0 {
		return nil, nil
	}
	return stream.EventsToNode(events)
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
//	tx, err := storage.NewTx(participantCount, meta)
//	if err != nil {
//	    // handle error
//	}
//
//	// Each participant gets their own patcher handle
//	patcher := tx.NewPatcher(kp, m, p)
//	result := patcher.WaitForCompletion()
func (s *Storage) NewTx(participantCount int, meta *api.PatchMeta) (tx.Tx, error) {
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
		Meta:        meta,
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
	segments := s.index.LookupRange("", nil, nil)
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
