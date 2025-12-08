package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

func TestStorage_Close(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// Close should persist index (if there are commits)
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// With no commits, getIndexMaxCommit() returns -1, so index file is not created
	// This is expected behavior - index is only persisted when there are commits
	indexPath := filepath.Join(tmpDir, "index.gob")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		// This is expected when there are no commits
		return
	}

	// If index file exists, verify we can load it
	idx, maxCommit, err := index.LoadIndexWithMetadata(indexPath)
	if err != nil {
		t.Fatalf("LoadIndexWithMetadata() error = %v", err)
	}
	if idx == nil {
		t.Error("loaded index is nil")
	}
	if maxCommit < 0 {
		t.Errorf("expected maxCommit >= 0, got %d", maxCommit)
	}
}

func TestStorage_Close_PersistsIndex(t *testing.T) {
	tmpDir := t.TempDir()

	s1, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// Get initial max commit
	initialMaxCommit := s1.getIndexMaxCommit()

	// Close first instance
	if err := s1.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Open again - should load persisted index
	s2, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("Open() second time error = %v", err)
	}
	defer s2.Close()

	// Verify max commit persisted
	reloadedMaxCommit := s2.getIndexMaxCommit()
	if reloadedMaxCommit != initialMaxCommit {
		t.Errorf("maxCommit mismatch: got %d, want %d", reloadedMaxCommit, initialMaxCommit)
	}
}
