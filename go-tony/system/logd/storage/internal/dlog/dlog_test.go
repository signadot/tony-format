package dlog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

func TestNewDLog(t *testing.T) {
	tmpDir := t.TempDir()

	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Check that both log files exist
	logAPath := filepath.Join(tmpDir, "logA")
	logBPath := filepath.Join(tmpDir, "logB")

	if _, err := os.Stat(logAPath); os.IsNotExist(err) {
		t.Errorf("logA file does not exist")
	}
	if _, err := os.Stat(logBPath); os.IsNotExist(err) {
		t.Errorf("logB file does not exist")
	}

	// Check default active log is A
	if dl.GetActiveLog() != LogFileA {
		t.Errorf("expected active log to be A, got %q", dl.GetActiveLog())
	}
}

func TestNewDLogWithExistingState(t *testing.T) {
	tmpDir := t.TempDir()

	// Create state file indicating B is active
	statePath := filepath.Join(tmpDir, "dlog.state")
	if err := os.WriteFile(statePath, []byte("B"), 0644); err != nil {
		t.Fatalf("failed to create state file: %v", err)
	}

	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Check that active log is B
	if dl.GetActiveLog() != LogFileB {
		t.Errorf("expected active log to be B, got %q", dl.GetActiveLog())
	}
}

func TestDLogFile_AppendEntry_ReadEntryAt(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Create a test entry
	entry := &Entry{
		Commit:    1,
		Timestamp: time.Now().Format(time.RFC3339),
		Patch:     ir.FromMap(map[string]*ir.Node{"test": ir.FromString("value")}),
	}

	// Append entry
	position, logFile, err := dl.AppendEntry(entry)
	if err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	if logFile != LogFileA {
		t.Errorf("expected log file A, got %q", logFile)
	}

	if position != 0 {
		t.Errorf("expected position 0 for first entry, got %d", position)
	}

	// Read entry back
	readEntry, err := dl.ReadEntryAt(logFile, position)
	if err != nil {
		t.Fatalf("ReadEntryAt() error = %v", err)
	}

	// Compare entries
	if readEntry.Commit != entry.Commit {
		t.Errorf("Commit mismatch: got %d, want %d", readEntry.Commit, entry.Commit)
	}
	if readEntry.Timestamp != entry.Timestamp {
		t.Errorf("Timestamp mismatch: got %q, want %q", readEntry.Timestamp, entry.Timestamp)
	}

	// Compare patches (simplified - just check they're not nil)
	if readEntry.Patch == nil {
		t.Error("read entry Patch is nil")
	}
}

func TestDLogFile_AppendEntry_Multiple(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Append multiple entries
	entries := []*Entry{
		{
			Commit:    1,
			Timestamp: time.Now().Format(time.RFC3339),
			Patch:     ir.FromMap(map[string]*ir.Node{"a": ir.FromInt(1)}),
		},
		{
			Commit:    2,
			Timestamp: time.Now().Format(time.RFC3339),
			Patch:     ir.FromMap(map[string]*ir.Node{"b": ir.FromInt(2)}),
		},
		{
			Commit:    3,
			Timestamp: time.Now().Format(time.RFC3339),
			Patch:     ir.FromMap(map[string]*ir.Node{"c": ir.FromInt(3)}),
		},
	}

	positions := make([]int64, len(entries))
	for i, entry := range entries {
		pos, _, err := dl.AppendEntry(entry)
		if err != nil {
			t.Fatalf("AppendEntry(%d) error = %v", i, err)
		}
		positions[i] = pos
	}

	// Read entries back and verify
	for i, expectedEntry := range entries {
		readEntry, err := dl.ReadEntryAt(LogFileA, positions[i])
		if err != nil {
			t.Fatalf("ReadEntryAt(%d) error = %v", i, err)
		}

		if readEntry.Commit != expectedEntry.Commit {
			t.Errorf("Entry %d: Commit mismatch: got %d, want %d", i, readEntry.Commit, expectedEntry.Commit)
		}
	}
}

func TestDLog_SwitchActive(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Initially active should be A
	if dl.GetActiveLog() != LogFileA {
		t.Errorf("expected initial active log to be A, got %q", dl.GetActiveLog())
	}

	// Switch to B
	if err := dl.SwitchActive(); err != nil {
		t.Fatalf("SwitchActive() error = %v", err)
	}

	if dl.GetActiveLog() != LogFileB {
		t.Errorf("expected active log to be B after switch, got %q", dl.GetActiveLog())
	}

	if dl.GetInactiveLog() != LogFileA {
		t.Errorf("expected inactive log to be A, got %q", dl.GetInactiveLog())
	}

	// Switch back to A
	if err := dl.SwitchActive(); err != nil {
		t.Fatalf("SwitchActive() error = %v", err)
	}

	if dl.GetActiveLog() != LogFileA {
		t.Errorf("expected active log to be A after second switch, got %q", dl.GetActiveLog())
	}

	// Verify state was persisted
	dl2, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() (second instance) error = %v", err)
	}
	defer dl2.Close()

	// Should still be A (we switched back)
	if dl2.GetActiveLog() != LogFileA {
		t.Errorf("expected persisted active log to be A, got %q", dl2.GetActiveLog())
	}
}

func TestDLog_AppendEntry_SwitchesActiveLog(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Append to A
	entry1 := &Entry{
		Commit:    1,
		Timestamp: time.Now().Format(time.RFC3339),
		Patch:     ir.FromMap(map[string]*ir.Node{"log": ir.FromString("A")}),
	}
	pos1, logFile1, err := dl.AppendEntry(entry1)
	if err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}
	if logFile1 != LogFileA {
		t.Errorf("expected first entry in logA, got %q", logFile1)
	}

	// Switch to B
	if err := dl.SwitchActive(); err != nil {
		t.Fatalf("SwitchActive() error = %v", err)
	}

	// Append to B
	entry2 := &Entry{
		Commit:    2,
		Timestamp: time.Now().Format(time.RFC3339),
		Patch:     ir.FromMap(map[string]*ir.Node{"log": ir.FromString("B")}),
	}
	pos2, logFile2, err := dl.AppendEntry(entry2)
	if err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}
	if logFile2 != LogFileB {
		t.Errorf("expected second entry in logB, got %q", logFile2)
	}

	// Verify entries are in correct files
	read1, err := dl.ReadEntryAt(LogFileA, pos1)
	if err != nil {
		t.Fatalf("ReadEntryAt(logA) error = %v", err)
	}
	if read1.Commit != 1 {
		t.Errorf("logA entry commit mismatch: got %d, want 1", read1.Commit)
	}

	read2, err := dl.ReadEntryAt(LogFileB, pos2)
	if err != nil {
		t.Fatalf("ReadEntryAt(logB) error = %v", err)
	}
	if read2.Commit != 2 {
		t.Errorf("logB entry commit mismatch: got %d, want 2", read2.Commit)
	}
}

func TestDLog_ReadEntryAt_InvalidLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	_, err = dl.ReadEntryAt(LogFileID("invalid"), 0)
	if err == nil {
		t.Error("expected error for invalid log file ID")
	}
}

func TestDLogFile_ReadEntryAt_InvalidPosition(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Try to read at position beyond file size
	_, err = dl.ReadEntryAt(LogFileA, 1000000)
	if err == nil {
		t.Error("expected error for invalid position")
	}
}

func TestDLogFile_Size(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Initial size should be 0
	size, err := dl.logA.Size()
	if err != nil {
		t.Fatalf("Size() error = %v", err)
	}
	if size != 0 {
		t.Errorf("expected initial size 0, got %d", size)
	}

	// Append an entry
	entry := &Entry{
		Commit:    1,
		Timestamp: time.Now().Format(time.RFC3339),
		Patch:     ir.FromMap(map[string]*ir.Node{"test": ir.FromString("value")}),
	}
	_, _, err = dl.AppendEntry(entry)
	if err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Size should have increased
	newSize, err := dl.logA.Size()
	if err != nil {
		t.Fatalf("Size() error = %v", err)
	}
	if newSize <= size {
		t.Errorf("expected size to increase, got %d (was %d)", newSize, size)
	}
}

func TestDLogFile_Position(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Initial position should be 0
	pos := dl.logA.Position()
	if pos != 0 {
		t.Errorf("expected initial position 0, got %d", pos)
	}

	// Append an entry
	entry := &Entry{
		Commit:    1,
		Timestamp: time.Now().Format(time.RFC3339),
		Patch:     ir.FromMap(map[string]*ir.Node{"test": ir.FromString("value")}),
	}
	appendPos, _, err := dl.AppendEntry(entry)
	if err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Position should match append position
	newPos := dl.logA.Position()
	// Position should be appendPos + 4 (length prefix) + entry size
	entryBytes, err := entry.ToTony()
	if err != nil {
		t.Fatalf("ToTony() error = %v", err)
	}
	expectedPos := appendPos + 4 + int64(len(entryBytes))
	if newPos != expectedPos {
		t.Errorf("position mismatch: got %d, want %d", newPos, expectedPos)
	}
}

func TestDLog_Close(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}

	// Close should succeed
	if err := dl.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Closing again should be safe (idempotent)
	if err := dl.Close(); err != nil {
		t.Fatalf("Close() (second call) error = %v", err)
	}
}

func TestDLog_Iterator(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Append multiple entries
	entries := []*Entry{
		{
			Commit:    1,
			Timestamp: time.Now().Format(time.RFC3339),
			Patch:     ir.FromMap(map[string]*ir.Node{"a": ir.FromInt(1)}),
		},
		{
			Commit:    2,
			Timestamp: time.Now().Format(time.RFC3339),
			Patch:     ir.FromMap(map[string]*ir.Node{"b": ir.FromInt(2)}),
		},
		{
			Commit:    3,
			Timestamp: time.Now().Format(time.RFC3339),
			Patch:     ir.FromMap(map[string]*ir.Node{"c": ir.FromInt(3)}),
		},
	}

	for _, entry := range entries {
		if _, _, err := dl.AppendEntry(entry); err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// Create iterator from start
	iter, err := dl.Iterator()
	if err != nil {
		t.Fatalf("Iterator() error = %v", err)
	}

	// Read all entries
	var readEntries []*Entry
	var positions []int64
	var logFiles []LogFileID
	for {
		entry, logFile, pos, err := iter.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Next() error = %v", err)
		}
		readEntries = append(readEntries, entry)
		positions = append(positions, pos)
		logFiles = append(logFiles, logFile)
	}

	// Verify we read all entries
	if len(readEntries) != len(entries) {
		t.Errorf("expected %d entries, got %d", len(entries), len(readEntries))
	}

	// Verify commit numbers match
	for i, readEntry := range readEntries {
		if readEntry.Commit != entries[i].Commit {
			t.Errorf("entry %d: commit mismatch: got %d, want %d", i, readEntry.Commit, entries[i].Commit)
		}
	}

	// Verify iterator is done
	if !iter.Done() {
		t.Error("expected iterator to be done")
	}
}

func TestDLog_Iterator_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	iter, err := dl.Iterator()
	if err != nil {
		t.Fatalf("Iterator() error = %v", err)
	}

	// Should immediately return EOF
	_, _, _, err = iter.Next()
	if err == nil {
		t.Error("expected EOF for empty file")
	}
	if err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestEntry_TransactionEntry(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Create a transaction entry
	txState := &tx.State{
		TxID:        123,
		Scope:       nil,
		PatcherData: []*tx.PatcherData{},
	}

	entry := &Entry{
		Commit:    1,
		Timestamp: time.Now().Format(time.RFC3339),
		Patch:     ir.FromMap(map[string]*ir.Node{"test": ir.FromString("value")}),
		TxSource:  txState,
	}

	pos, _, err := dl.AppendEntry(entry)
	if err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	readEntry, err := dl.ReadEntryAt(LogFileA, pos)
	if err != nil {
		t.Fatalf("ReadEntryAt() error = %v", err)
	}

	if readEntry.TxSource == nil {
		t.Error("TxSource is nil")
	}
	if readEntry.TxSource.TxID != 123 {
		t.Errorf("TxID mismatch: got %d, want 123", readEntry.TxSource.TxID)
	}
}

func TestEntry_SnapshotEntry(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Create a snapshot entry
	snapPos := int64(100)
	entry := &Entry{
		Commit:    1,
		Timestamp: time.Now().Format(time.RFC3339),
		Patch:     ir.FromMap(map[string]*ir.Node{"full": ir.FromString("state")}),
		SnapPos:   &snapPos,
	}

	pos, _, err := dl.AppendEntry(entry)
	if err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	readEntry, err := dl.ReadEntryAt(LogFileA, pos)
	if err != nil {
		t.Fatalf("ReadEntryAt() error = %v", err)
	}

	if readEntry.SnapPos == nil || *readEntry.SnapPos != 100 {
		t.Errorf("SnapPos mismatch: got %v, want 100", readEntry.SnapPos)
	}
}

func TestEntry_CompactionEntry(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Create a compaction entry
	lastCommit := int64(5)
	entry := &Entry{
		Commit:     10,
		Timestamp:  time.Now().Format(time.RFC3339),
		Patch:      ir.FromMap(map[string]*ir.Node{"compacted": ir.FromString("data")}),
		LastCommit: &lastCommit,
	}

	pos, _, err := dl.AppendEntry(entry)
	if err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	readEntry, err := dl.ReadEntryAt(LogFileA, pos)
	if err != nil {
		t.Fatalf("ReadEntryAt() error = %v", err)
	}

	if readEntry.LastCommit == nil || *readEntry.LastCommit != 5 {
		t.Errorf("LastCommit mismatch: got %v, want 5", readEntry.LastCommit)
	}
}

func TestDLogFile_AppendEntry_LargeEntry(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Create a large entry (but still within uint32 limit)
	largeMap := make(map[string]*ir.Node)
	for i := 0; i < 1000; i++ {
		largeMap[fmt.Sprintf("key%d", i)] = ir.FromString("value")
	}

	entry := &Entry{
		Commit:    1,
		Timestamp: time.Now().Format(time.RFC3339),
		Patch:     ir.FromMap(largeMap),
	}

	pos, _, err := dl.AppendEntry(entry)
	if err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Read it back
	readEntry, err := dl.ReadEntryAt(LogFileA, pos)
	if err != nil {
		t.Fatalf("ReadEntryAt() error = %v", err)
	}

	if readEntry.Patch == nil {
		t.Error("read entry Patch is nil")
	}
}

func TestDLog_ConcurrentAppends(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Append entries concurrently
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(commit int64) {
			entry := &Entry{
				Commit:    commit,
				Timestamp: time.Now().Format(time.RFC3339),
				Patch:     ir.FromMap(map[string]*ir.Node{"commit": ir.FromInt(commit)}),
			}
			_, _, err := dl.AppendEntry(entry)
			done <- err
		}(int64(i + 1))
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent append %d error = %v", i, err)
		}
	}

	// Verify all entries can be read
	iter, err := dl.Iterator()
	if err != nil {
		t.Fatalf("Iterator() error = %v", err)
	}

	count := 0
	for {
		_, _, _, err := iter.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Next() error = %v", err)
		}
		count++
	}

	if count != 10 {
		t.Errorf("expected 10 entries, got %d", count)
	}
}

func TestDLog_Iterator_SwitchesBetweenFiles(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Write entries to logA with commits 1, 3, 5
	// We'll manually write to logA by switching active log or writing directly
	// For now, let's write entries that will interleave

	// First, write some entries that will go to logA (default active)
	entriesA := []*Entry{
		{Commit: 1, Timestamp: time.Now().Format(time.RFC3339), Patch: ir.FromMap(map[string]*ir.Node{"a": ir.FromInt(1)})},
		{Commit: 3, Timestamp: time.Now().Format(time.RFC3339), Patch: ir.FromMap(map[string]*ir.Node{"c": ir.FromInt(3)})},
		{Commit: 5, Timestamp: time.Now().Format(time.RFC3339), Patch: ir.FromMap(map[string]*ir.Node{"e": ir.FromInt(5)})},
	}

	// Write entries to logB with commits 2, 4, 6
	// We need to write directly to logB since AppendEntry always uses active log
	entriesB := []*Entry{
		{Commit: 2, Timestamp: time.Now().Format(time.RFC3339), Patch: ir.FromMap(map[string]*ir.Node{"b": ir.FromInt(2)})},
		{Commit: 4, Timestamp: time.Now().Format(time.RFC3339), Patch: ir.FromMap(map[string]*ir.Node{"d": ir.FromInt(4)})},
		{Commit: 6, Timestamp: time.Now().Format(time.RFC3339), Patch: ir.FromMap(map[string]*ir.Node{"f": ir.FromInt(6)})},
	}

	// Write to logA
	for _, entry := range entriesA {
		_, _, err := dl.AppendEntry(entry)
		if err != nil {
			t.Fatalf("AppendEntry() to logA error = %v", err)
		}
	}

	// Switch active log to B
	dl.mu.Lock()
	dl.activeLog = LogFileB
	dl.mu.Unlock()

	// Write to logB
	for _, entry := range entriesB {
		_, _, err := dl.AppendEntry(entry)
		if err != nil {
			t.Fatalf("AppendEntry() to logB error = %v", err)
		}
	}

	// Now iterate and verify entries come back in commit order
	iter, err := dl.Iterator()
	if err != nil {
		t.Fatalf("Iterator() error = %v", err)
	}

	var readCommits []int64
	var readLogFiles []LogFileID
	for {
		entry, logFile, _, err := iter.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("Next() error = %v", err)
		}
		readCommits = append(readCommits, entry.Commit)
		readLogFiles = append(readLogFiles, logFile)
	}

	// Verify we got all 6 entries
	if len(readCommits) != 6 {
		t.Fatalf("expected 6 entries, got %d", len(readCommits))
	}

	// Verify commit order: 1, 2, 3, 4, 5, 6
	expectedCommits := []int64{1, 2, 3, 4, 5, 6}
	for i, commit := range readCommits {
		if commit != expectedCommits[i] {
			t.Errorf("entry %d: expected commit %d, got %d", i, expectedCommits[i], commit)
		}
	}

	// Verify switching: commits 1,3,5 from logA, commits 2,4,6 from logB
	expectedLogFiles := []LogFileID{LogFileA, LogFileB, LogFileA, LogFileB, LogFileA, LogFileB}
	for i, logFile := range readLogFiles {
		if logFile != expectedLogFiles[i] {
			t.Errorf("entry %d (commit %d): expected logFile %s, got %s", i, readCommits[i], expectedLogFiles[i], logFile)
		}
	}
}

func TestDLog_OpenReaderAt(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	// Append some entries to the log
	entry1 := &Entry{
		Commit:    1,
		Timestamp: time.Now().Format(time.RFC3339),
		Patch:     ir.FromMap(map[string]*ir.Node{"test": ir.FromString("value1")}),
	}
	_, _, err = dl.AppendEntry(entry1)
	if err != nil {
		t.Fatalf("AppendEntry() error = %v", err)
	}

	// Get current position (this is where we'll write inline snapshot data)
	snapshotPos := dl.logA.Position()

	// Write some inline data directly to the file (simulating snapshot data)
	testData := []byte("This is inline snapshot data for testing")
	dl.logA.mu.Lock()
	n, err := dl.logA.file.Write(testData)
	if err != nil {
		dl.logA.mu.Unlock()
		t.Fatalf("failed to write test data: %v", err)
	}
	dl.logA.position += int64(n)
	dl.logA.mu.Unlock()

	// Open a reader at the snapshot position
	reader, err := dl.OpenReaderAt(LogFileA, snapshotPos)
	if err != nil {
		t.Fatalf("OpenReaderAt() error = %v", err)
	}
	defer reader.Close()

	// Verify Seek(0, SeekStart) seeks to the snapshot position (not file start)
	pos, err := reader.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek(0, SeekStart) error = %v", err)
	}
	if pos != 0 {
		t.Errorf("Seek(0, SeekStart) returned position %d, want 0", pos)
	}

	// Read the data
	buf := make([]byte, len(testData))
	n, err = reader.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != len(testData) {
		t.Errorf("Read() read %d bytes, want %d", n, len(testData))
	}

	// Verify the data matches
	if string(buf) != string(testData) {
		t.Errorf("Read data mismatch:\ngot:  %q\nwant: %q", string(buf), string(testData))
	}

	// Verify Seek(0, SeekStart) works again (seek back to start of snapshot)
	pos, err = reader.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("Second Seek(0, SeekStart) error = %v", err)
	}
	if pos != 0 {
		t.Errorf("Second Seek(0, SeekStart) returned position %d, want 0", pos)
	}

	// Read the first 10 bytes
	buf2 := make([]byte, 10)
	n, err = reader.Read(buf2)
	if err != nil {
		t.Fatalf("Second Read() error = %v", err)
	}
	if string(buf2[:n]) != string(testData[:10]) {
		t.Errorf("Second read mismatch:\ngot:  %q\nwant: %q", string(buf2[:n]), string(testData[:10]))
	}

	// Verify Seek forward works
	pos, err = reader.Seek(5, io.SeekStart)
	if err != nil {
		t.Fatalf("Seek(5, SeekStart) error = %v", err)
	}
	if pos != 5 {
		t.Errorf("Seek(5, SeekStart) returned position %d, want 5", pos)
	}

	// Read from offset 5
	buf3 := make([]byte, 10)
	n, err = reader.Read(buf3)
	if err != nil {
		t.Fatalf("Third Read() error = %v", err)
	}
	if string(buf3[:n]) != string(testData[5:15]) {
		t.Errorf("Third read mismatch:\ngot:  %q\nwant: %q", string(buf3[:n]), string(testData[5:15]))
	}
}

func TestDLog_OpenReaderAt_InvalidLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	dl, err := NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	_, err = dl.OpenReaderAt(LogFileID("invalid"), 0)
	if err == nil {
		t.Error("expected error for invalid log file ID")
	}
}
