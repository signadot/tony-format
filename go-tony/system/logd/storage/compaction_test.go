package storage

import (
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// TestCompact_RemovesAbortedSchemaMigration verifies that aborted schema migrations
// are filtered out during compaction.
func TestCompact_RemovesAbortedSchemaMigration(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Write initial data
	patch1, _ := parse.Parse([]byte(`{data: "initial"}`))
	tx1, _ := s.NewTx(1, nil)
	p1, _ := tx1.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch1}})
	result1 := p1.Commit()
	if !result1.Committed {
		t.Fatalf("initial commit failed: %v", result1.Error)
	}

	// Start and abort migration - creates aborted schema entry in inactive log (B)
	schema := testSchema(t, `{data: .[string]}`)
	_, err = s.StartMigration(schema)
	if err != nil {
		t.Fatalf("StartMigration() error = %v", err)
	}

	abortCommit, err := s.AbortMigration()
	if err != nil {
		t.Fatalf("AbortMigration() error = %v", err)
	}
	t.Logf("aborted schema at commit %d", abortCommit)

	// First switch: A→inactive, B→active
	// The aborted schema entry is in B, so it's now in the active log
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog() error = %v", err)
	}

	// Write more data to the new active log (B)
	patch2, _ := parse.Parse([]byte(`{data: "more"}`))
	tx2, _ := s.NewTx(1, nil)
	p2, _ := tx2.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch2}})
	p2.Commit()

	// Second switch: B→inactive, A→active
	// Now the aborted schema entry (in B) is in the inactive log
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog() error = %v", err)
	}

	// Verify aborted segment is in inactive log before compaction
	inactiveLogID := s.dLog.GetInactiveLog()
	allSegments := s.index.LookupRangeAll("", nil, nil)

	var abortedSegmentFound bool
	for _, seg := range allSegments {
		if dlog.LogFileID(seg.LogFile) == inactiveLogID {
			entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
			if err == nil && entry.SchemaEntry != nil && entry.SchemaEntry.Status == dlog.SchemaStatusAborted {
				abortedSegmentFound = true
				t.Logf("found aborted schema entry in inactive log at commit %d", entry.Commit)
				break
			}
		}
	}

	if !abortedSegmentFound {
		t.Fatal("aborted schema segment not found in inactive log before compaction")
	}

	// Run compaction - should filter out aborted schema entry
	config := &CompactionConfig{
		Cutoff:      time.Hour, // Long cutoff to keep patches, focus on schema filtering
		GracePeriod: 100 * time.Millisecond,
	}

	if err := s.Compact(config); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	// Verify aborted schema segment is removed from index
	segmentsAfter := s.index.LookupRangeAll("", nil, nil)
	for _, seg := range segmentsAfter {
		if dlog.LogFileID(seg.LogFile) != inactiveLogID {
			continue // Only check inactive log
		}
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			continue
		}
		if entry.SchemaEntry != nil && entry.SchemaEntry.Status == dlog.SchemaStatusAborted {
			t.Errorf("found aborted schema entry at commit %d after compaction", entry.Commit)
		}
	}
}

// TestCompact_RemovesSupersededPendingMigration verifies that old pending schema migrations
// that have been completed or aborted are filtered out during compaction.
func TestCompact_RemovesSupersededPendingMigration(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Write initial data
	patch1, _ := parse.Parse([]byte(`{data: "initial"}`))
	tx1, _ := s.NewTx(1, nil)
	p1, _ := tx1.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch1}})
	result1 := p1.Commit()
	if !result1.Committed {
		t.Fatalf("initial commit failed: %v", result1.Error)
	}

	// Start and complete first migration
	schema1 := testSchema(t, `{data: .[string]}`)
	pendingCommit1, err := s.StartMigration(schema1)
	if err != nil {
		t.Fatalf("StartMigration 1 error = %v", err)
	}
	t.Logf("first pending at commit %d", pendingCommit1)

	activeCommit1, err := s.CompleteMigration()
	if err != nil {
		t.Fatalf("CompleteMigration 1 error = %v", err)
	}
	t.Logf("first active at commit %d", activeCommit1)

	// Write more data
	patch2, _ := parse.Parse([]byte(`{data: "more"}`))
	tx2, _ := s.NewTx(1, nil)
	p2, _ := tx2.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch2}})
	p2.Commit()

	// Start and complete second migration - this supersedes the first
	schema2 := testSchema(t, `{data: .[string], version: .[int]}`)
	pendingCommit2, err := s.StartMigration(schema2)
	if err != nil {
		t.Fatalf("StartMigration 2 error = %v", err)
	}
	t.Logf("second pending at commit %d", pendingCommit2)

	activeCommit2, err := s.CompleteMigration()
	if err != nil {
		t.Fatalf("CompleteMigration 2 error = %v", err)
	}
	t.Logf("second active at commit %d", activeCommit2)

	// Switch dlog (also creates snapshot)
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog() error = %v", err)
	}

	// Run compaction
	config := &CompactionConfig{
		Cutoff:      time.Millisecond,
		GracePeriod: 100 * time.Millisecond,
	}

	if err := s.Compact(config); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	// Verify first pending schema entry is removed (superseded)
	segmentsAfter := s.index.LookupRangeAll("", nil, nil)
	for _, seg := range segmentsAfter {
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			continue
		}
		if entry.SchemaEntry != nil {
			if entry.SchemaEntry.Status == dlog.SchemaStatusPending && entry.Commit == pendingCommit1 {
				t.Errorf("found superseded pending schema entry at commit %d after compaction", entry.Commit)
			}
		}
	}
}

// TestCompact_KeepsCurrentPendingMigration verifies that the current pending migration
// is NOT filtered out during compaction.
func TestCompact_KeepsCurrentPendingMigration(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Write initial data
	patch1, _ := parse.Parse([]byte(`{data: "initial"}`))
	tx1, _ := s.NewTx(1, nil)
	p1, _ := tx1.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch1}})
	result1 := p1.Commit()
	if !result1.Committed {
		t.Fatalf("initial commit failed: %v", result1.Error)
	}

	// Start migration but don't complete
	schema := testSchema(t, `{data: .[string]}`)
	pendingCommit, err := s.StartMigration(schema)
	if err != nil {
		t.Fatalf("StartMigration() error = %v", err)
	}
	t.Logf("pending migration at commit %d", pendingCommit)

	// Switch dlog (also creates snapshot)
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog() error = %v", err)
	}

	// Run compaction - should keep current pending
	config := &CompactionConfig{
		Cutoff:      time.Hour, // Large cutoff so snapshots survive
		GracePeriod: 100 * time.Millisecond,
	}

	if err := s.Compact(config); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	// Verify pending migration state is still intact
	if !s.HasPendingMigration() {
		t.Error("expected pending migration to still exist after compaction")
	}

	pendingSchema, pc := s.GetPendingSchema()
	if pendingSchema == nil {
		t.Error("expected pending schema to still exist after compaction")
	}
	if pc != pendingCommit {
		t.Errorf("expected pending commit %d, got %d", pendingCommit, pc)
	}
}

// TestCompact_KeepsActiveSchemaViaPinnedCommit verifies that the current active schema
// snapshot is protected by the pinned commit mechanism.
func TestCompact_KeepsActiveSchemaViaPinnedCommit(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Write initial data
	patch1, _ := parse.Parse([]byte(`{data: "initial"}`))
	tx1, _ := s.NewTx(1, nil)
	p1, _ := tx1.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch1}})
	result1 := p1.Commit()
	if !result1.Committed {
		t.Fatalf("initial commit failed: %v", result1.Error)
	}

	// Complete a migration to have an active schema
	schema := testSchema(t, `{data: .[string]}`)
	_, err = s.StartMigration(schema)
	if err != nil {
		t.Fatalf("StartMigration() error = %v", err)
	}

	activeCommit, err := s.CompleteMigration()
	if err != nil {
		t.Fatalf("CompleteMigration() error = %v", err)
	}
	t.Logf("active schema at commit %d", activeCommit)

	// Switch dlog (also creates snapshot)
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog() error = %v", err)
	}

	// Run compaction with aggressive settings
	config := &CompactionConfig{
		Cutoff:      time.Millisecond, // Very short cutoff
		GracePeriod: 100 * time.Millisecond,
	}

	if err := s.Compact(config); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	// Verify active schema is still intact
	activeSchema, ac := s.GetActiveSchema()
	if activeSchema == nil {
		t.Error("expected active schema to still exist after compaction")
	}
	if ac != activeCommit {
		t.Errorf("expected active commit %d, got %d", activeCommit, ac)
	}
}

// TestShouldSkipSchemaEntry tests the schema entry filtering logic directly.
func TestShouldSkipSchemaEntry(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	tests := []struct {
		name          string
		status        string
		commit        int64
		hasPending    bool
		pendingCommit int64
		wantSkip      bool
	}{
		{
			name:     "aborted always skipped",
			status:   dlog.SchemaStatusAborted,
			commit:   10,
			wantSkip: true,
		},
		{
			name:          "pending skipped when no pending migration",
			status:        dlog.SchemaStatusPending,
			commit:        10,
			hasPending:    false,
			pendingCommit: 0,
			wantSkip:      true,
		},
		{
			name:          "pending skipped when different commit",
			status:        dlog.SchemaStatusPending,
			commit:        10,
			hasPending:    true,
			pendingCommit: 20, // Different commit
			wantSkip:      true,
		},
		{
			name:          "current pending kept",
			status:        dlog.SchemaStatusPending,
			commit:        20,
			hasPending:    true,
			pendingCommit: 20, // Same commit
			wantSkip:      false,
		},
		{
			name:     "active not skipped by schema filter",
			status:   dlog.SchemaStatusActive,
			commit:   10,
			wantSkip: false, // Active handled by tier policy
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &dlog.SchemaEntry{Status: tt.status}
			got := s.shouldSkipSchemaEntry(entry, tt.commit, tt.hasPending, tt.pendingCommit)
			if got != tt.wantSkip {
				t.Errorf("shouldSkipSchemaEntry() = %v, want %v", got, tt.wantSkip)
			}
		})
	}
}
