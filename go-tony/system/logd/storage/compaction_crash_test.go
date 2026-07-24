package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

// Regression tests for compaction crash/restart consistency (issue 656g8yt5).
//
// Compaction rewrites the inactive log's byte layout and bumps an in-memory generation, but
// (a) the generation is not persisted (it resets to 0 on restart), and (b) the index is
// persisted asynchronously. On restart the persisted index can disagree with the on-disk
// log, so a read of compacted data returns the wrong bytes (silent corruption) or fails
// forever with ErrCompactionInterrupted.

func casWrite(t *testing.T, s *Storage, data string) {
	t.Helper()
	node, err := parse.Parse([]byte(data))
	if err != nil {
		t.Fatalf("parse %q: %v", data, err)
	}
	tx, err := s.NewTx(1, nil)
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: node}})
	if err != nil {
		t.Fatalf("NewPatcher: %v", err)
	}
	if r := p.Commit(); !r.Committed {
		t.Fatalf("commit %q failed: %v", data, r.Error)
	}
}

// readAll returns the full state document at the current commit.
func readAll(t *testing.T, s *Storage) *ir.Node {
	t.Helper()
	c, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	doc, err := s.ReadStateAt("", c, nil)
	if err != nil {
		t.Fatalf("ReadStateAt(commit=%d): %v", c, err)
	}
	return doc
}

// setupCompactableState writes several superseding values, switches the active log (so the
// old values land in the inactive log), and returns the storage ready to compact with a
// short cutoff. Returns the expected full state after compaction.
func setupCompactableState(t *testing.T, s *Storage) *ir.Node {
	t.Helper()
	casWrite(t, s, `{k: "v1", other: "x"}`)
	casWrite(t, s, `{k: "v2"}`)
	casWrite(t, s, `{k: "v3"}`) // current: {k:v3, other:x}
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	time.Sleep(5 * time.Millisecond) // let patches age past the cutoff
	return readAll(t, s)
}

func compactConfig() *CompactionConfig {
	return &CompactionConfig{
		Cutoff:       time.Millisecond, // aggressively drop superseded patches -> real rewrite
		BaseInterval: time.Hour,
		SlotsPerTier: 8,
		Multiplier:   2,
		GracePeriod:  50 * time.Millisecond,
	}
}

// Mode 2: a clean Close + reopen after compaction. The post-compaction index (new positions,
// generation 1) is persisted by Close, but the reopened DLog resets generation to 0, so every
// compacted segment reads back with a generation mismatch.
func TestCompact_CleanReopen_ReadsCompactedState(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	want := setupCompactableState(t, s)
	if err := s.Compact(compactConfig()); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if got := readAll(t, s); !got.DeepEqual(want) {
		t.Fatalf("in-process read after compaction changed state:\n want %v\n got  %v", want, got)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Restart.
	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	got := readAll(t, s2)
	if !got.DeepEqual(want) {
		t.Fatalf("state lost/corrupted after compaction + reopen:\n want %v\n got  %v", want, got)
	}
}

// Mode 3: a crash between swapLogFile's two renames. logX is gone (moved to logX.old), the
// survivors live in logX.old / logX.compact.tmp, and the second rename never ran. On restart
// the inactive log's data must be recovered, not silently reborn as an empty file.
func TestCompact_CrashBetweenRenames_RecoversData(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := setupCompactableState(t, s)
	inactive := string(s.dLog.GetInactiveLog())
	if err := s.Compact(compactConfig()); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Simulate a crash between the two renames of a *fresh* compaction of the now-compacted
	// log: rename log -> log.old, and drop `log` (as if the second rename never ran). We only
	// have log.old (the pre-crash contents); the recovery must restore it.
	logPath := filepath.Join(dir, "log"+inactive)
	if err := os.Rename(logPath, logPath+".old"); err != nil {
		t.Fatalf("simulate crash rename: %v", err)
	}
	// (logX now missing; logX.old holds the data)

	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	got := readAll(t, s2)
	if !got.DeepEqual(want) {
		t.Fatalf("data lost after crash-between-renames:\n want %v\n got  %v", want, got)
	}
}

// Mode 1: crash after the swap but before the post-compaction index is persisted (and after
// swapLogFile already deleted the `.old` undo copy). The on-disk index is pre-compaction (old
// positions, generation 0) while the log file is the rewritten one; the persisted generation
// (bumped durably during the swap) is what lets the restart detect the stale index and rebuild.
func TestCompact_CrashAfterSwapBeforeIndexPersist_RebuildsIndex(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	want := setupCompactableState(t, s)

	// Force the on-disk index to reflect the PRE-compaction state, then compact without any
	// further persist (no Close) — simulating a crash between the swap and the next index persist.
	c, _ := s.GetCurrentCommit()
	if err := index.StoreIndexWithMetadata(filepath.Join(dir, "index.gob"), s.index, c); err != nil {
		t.Fatalf("pre-compaction index store: %v", err)
	}
	if err := s.Compact(compactConfig()); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	// Do NOT Close s (its post-compaction index is never made durable). Reopen from disk.

	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if got := readAll(t, s2); !got.DeepEqual(want) {
		t.Fatalf("state corrupted after crash-before-index-persist:\n want %v\n got  %v", want, got)
	}
}
