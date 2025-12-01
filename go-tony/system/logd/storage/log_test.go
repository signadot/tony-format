package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadTransactionLog_BinarySearch(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create storage instance
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Create test entries with different commit counts
	entries := []*TransactionLogEntry{
		{Commit: 1, TransactionID: 1, Timestamp: "2024-01-01T00:00:00Z", PendingFiles: []PendingFileRef{{VirtualPath: "/a", TxSeq: 1}}},
		{Commit: 3, TransactionID: 2, Timestamp: "2024-01-01T00:01:00Z", PendingFiles: []PendingFileRef{{VirtualPath: "/b", TxSeq: 2}}},
		{Commit: 5, TransactionID: 3, Timestamp: "2024-01-01T00:02:00Z", PendingFiles: []PendingFileRef{{VirtualPath: "/c", TxSeq: 3}}},
		{Commit: 7, TransactionID: 4, Timestamp: "2024-01-01T00:03:00Z", PendingFiles: []PendingFileRef{{VirtualPath: "/d", TxSeq: 4}}},
		{Commit: 10, TransactionID: 5, Timestamp: "2024-01-01T00:04:00Z", PendingFiles: []PendingFileRef{{VirtualPath: "/e", TxSeq: 5}}},
		{Commit: 15, TransactionID: 6, Timestamp: "2024-01-01T00:05:00Z", PendingFiles: []PendingFileRef{{VirtualPath: "/f", TxSeq: 6}}},
		{Commit: 20, TransactionID: 7, Timestamp: "2024-01-01T00:06:00Z", PendingFiles: []PendingFileRef{{VirtualPath: "/g", TxSeq: 7}}},
	}

	// Write all entries
	for _, entry := range entries {
		if err := storage.AppendTransactionLog(entry); err != nil {
			t.Fatalf("failed to append log entry: %v", err)
		}
	}

	// Test 1: Read all entries (nil parameter)
	allEntries, err := storage.ReadTransactionLog(nil)
	if err != nil {
		t.Fatalf("failed to read all entries: %v", err)
	}
	if len(allEntries) != len(entries) {
		t.Errorf("expected %d entries, got %d", len(entries), len(allEntries))
	}
	for i, expected := range entries {
		if i >= len(allEntries) {
			break
		}
		if allEntries[i].Commit != expected.Commit {
			t.Errorf("entry %d: expected commitCount %d, got %d", i, expected.Commit, allEntries[i].Commit)
		}
		if allEntries[i].TransactionID != expected.TransactionID {
			t.Errorf("entry %d: expected transactionID %d, got %d", i, expected.TransactionID, allEntries[i].TransactionID)
		}
	}

	// Test 2: Binary search for commitCount >= 5
	minCommit := int64(5)
	filteredEntries, err := storage.ReadTransactionLog(&minCommit)
	if err != nil {
		t.Fatalf("failed to read entries with minCommitCount=5: %v", err)
	}
	expectedCount := 5 // entries with commitCount 5, 7, 10, 15, 20
	if len(filteredEntries) != expectedCount {
		t.Errorf("expected %d entries with commitCount >= 5, got %d", expectedCount, len(filteredEntries))
	}
	for i, entry := range filteredEntries {
		if entry.Commit < minCommit {
			t.Errorf("entry %d has commitCount %d < %d", i, entry.Commit, minCommit)
		}
	}
	// Verify first entry is correct
	if len(filteredEntries) > 0 && filteredEntries[0].Commit != 5 {
		t.Errorf("first entry should have commitCount 5, got %d", filteredEntries[0].Commit)
	}

	// Test 3: Binary search for commitCount >= 10
	minCommit = 10
	filteredEntries, err = storage.ReadTransactionLog(&minCommit)
	if err != nil {
		t.Fatalf("failed to read entries with minCommitCount=10: %v", err)
	}
	expectedCount = 3 // entries with commitCount 10, 15, 20
	if len(filteredEntries) != expectedCount {
		t.Errorf("expected %d entries with commitCount >= 10, got %d", expectedCount, len(filteredEntries))
	}
	for i, entry := range filteredEntries {
		if entry.Commit < minCommit {
			t.Errorf("entry %d has commitCount %d < %d", i, entry.Commit, minCommit)
		}
	}
	if len(filteredEntries) > 0 && filteredEntries[0].Commit != 10 {
		t.Errorf("first entry should have commitCount 10, got %d", filteredEntries[0].Commit)
	}

	// Test 4: Binary search for commitCount >= 1 (should get all)
	minCommit = 1
	filteredEntries, err = storage.ReadTransactionLog(&minCommit)
	if err != nil {
		t.Fatalf("failed to read entries with minCommitCount=1: %v", err)
	}
	if len(filteredEntries) != len(entries) {
		t.Errorf("expected %d entries with commitCount >= 1, got %d", len(entries), len(filteredEntries))
	}

	// Test 5: Binary search for commitCount >= 25 (should get none)
	minCommit = 25
	filteredEntries, err = storage.ReadTransactionLog(&minCommit)
	if err != nil {
		t.Fatalf("failed to read entries with minCommitCount=25: %v", err)
	}
	if len(filteredEntries) != 0 {
		t.Errorf("expected 0 entries with commitCount >= 25, got %d", len(filteredEntries))
	}

	// Test 6: Binary search for commitCount >= 0 (should get all)
	minCommit = 0
	filteredEntries, err = storage.ReadTransactionLog(&minCommit)
	if err != nil {
		t.Fatalf("failed to read entries with minCommitCount=0: %v", err)
	}
	if len(filteredEntries) != len(entries) {
		t.Errorf("expected %d entries with commitCount >= 0, got %d", len(entries), len(filteredEntries))
	}
}

func TestReadTransactionLog_EmptyLog(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create storage instance
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Test reading from empty log
	entries, err := storage.ReadTransactionLog(nil)
	if err != nil {
		t.Fatalf("failed to read empty log: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries from empty log, got %d", len(entries))
	}

	// Test binary search on empty log
	minCommit := int64(5)
	entries, err = storage.ReadTransactionLog(&minCommit)
	if err != nil {
		t.Fatalf("failed to read empty log with minCommitCount: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries from empty log with minCommitCount, got %d", len(entries))
	}
}

func TestReadTransactionLog_NonExistentFile(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create storage instance
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Remove the log file if it exists
	logFile := filepath.Join(tmpDir, "meta", "transactions.log")
	os.Remove(logFile)

	// Test reading from non-existent log file
	entries, err := storage.ReadTransactionLog(nil)
	if err != nil {
		t.Fatalf("failed to read non-existent log: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries from non-existent log, got %d", len(entries))
	}
}
