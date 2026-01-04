package dlog

import (
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
)

func TestCompactInactive(t *testing.T) {
	tmpDir := t.TempDir()

	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Write some entries to active log (A)
	var positions []int64
	for i := 1; i <= 5; i++ {
		entry := &Entry{
			Commit:    int64(i),
			Timestamp: time.Now().Format(time.RFC3339),
			Patch:     ir.FromMap(map[string]*ir.Node{"key": ir.FromInt(int64(i))}),
		}
		pos, _, err := dl.AppendEntry(entry)
		if err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
		positions = append(positions, pos)
	}

	// Switch to make A inactive
	if err := dl.SwitchActive(); err != nil {
		t.Fatalf("SwitchActive() error = %v", err)
	}

	// Now A is inactive, B is active
	if dl.GetActiveLog() != LogFileB {
		t.Fatalf("expected B to be active after switch")
	}
	if dl.GetInactiveLog() != LogFileA {
		t.Fatalf("expected A to be inactive after switch")
	}

	// Compact keeping only positions 1, 3, 5 (entries with commit 2, 4)
	keepPositions := []int64{positions[1], positions[3]}

	results, err := dl.CompactInactive(keepPositions, &CompactConfig{GracePeriod: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("CompactInactive() error = %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 compaction results, got %d", len(results))
	}

	// Verify position mapping
	posMap := make(map[int64]int64)
	for _, r := range results {
		posMap[r.OldPosition] = r.NewPosition
	}

	// Old positions should be in the map
	if _, ok := posMap[positions[1]]; !ok {
		t.Errorf("expected position %d to be in results", positions[1])
	}
	if _, ok := posMap[positions[3]]; !ok {
		t.Errorf("expected position %d to be in results", positions[3])
	}

	// Verify entries can be read at new positions
	for _, r := range results {
		entry, err := dl.ReadEntryAt(r.LogFile, r.NewPosition)
		if err != nil {
			t.Errorf("ReadEntryAt(%v, %d) error = %v", r.LogFile, r.NewPosition, err)
		}
		if entry == nil {
			t.Errorf("ReadEntryAt(%v, %d) returned nil", r.LogFile, r.NewPosition)
		}
	}
}

func TestCompactInactiveEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Write entries to A
	for i := 1; i <= 3; i++ {
		entry := &Entry{
			Commit:    int64(i),
			Timestamp: time.Now().Format(time.RFC3339),
			Patch:     ir.FromString("test"),
		}
		if _, _, err := dl.AppendEntry(entry); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// Switch to B
	if err := dl.SwitchActive(); err != nil {
		t.Fatalf("SwitchActive() error = %v", err)
	}

	// Compact with empty positions (should truncate)
	results, err := dl.CompactInactive(nil, &CompactConfig{GracePeriod: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("CompactInactive(nil) error = %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results for empty compact, got %d", len(results))
	}
}

func TestReaderRefcount(t *testing.T) {
	tmpDir := t.TempDir()

	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Initially no readers
	if c := dl.ActiveReaders(LogFileA); c != 0 {
		t.Errorf("expected 0 readers for A, got %d", c)
	}

	// Acquire and release readers
	dl.AcquireReader(LogFileA)
	if c := dl.ActiveReaders(LogFileA); c != 1 {
		t.Errorf("expected 1 reader for A, got %d", c)
	}

	dl.AcquireReader(LogFileA)
	if c := dl.ActiveReaders(LogFileA); c != 2 {
		t.Errorf("expected 2 readers for A, got %d", c)
	}

	dl.ReleaseReader(LogFileA)
	if c := dl.ActiveReaders(LogFileA); c != 1 {
		t.Errorf("expected 1 reader for A after release, got %d", c)
	}

	dl.ReleaseReader(LogFileA)
	if c := dl.ActiveReaders(LogFileA); c != 0 {
		t.Errorf("expected 0 readers for A after all releases, got %d", c)
	}

	// Check B is independent
	dl.AcquireReader(LogFileB)
	if c := dl.ActiveReaders(LogFileA); c != 0 {
		t.Errorf("expected 0 readers for A, got %d", c)
	}
	if c := dl.ActiveReaders(LogFileB); c != 1 {
		t.Errorf("expected 1 reader for B, got %d", c)
	}
	dl.ReleaseReader(LogFileB)
}
