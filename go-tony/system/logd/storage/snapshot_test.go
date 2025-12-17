package storage

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

func TestSwitchAndSnapshot(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Commit a few patches to have some state
	tx1, err := s.NewTx(1, nil)
	if err != nil {
		t.Fatalf("NewTx() error = %v", err)
	}

	patch1Data, err := parse.Parse([]byte(`{name: "alice"}`))
	if err != nil {
		t.Fatalf("parse patch1: %v", err)
	}

	patch1 := &api.Patch{
		Patch: api.Body{
			Path: "",
			Data: patch1Data,
		},
	}
	p1, err := tx1.NewPatcher(patch1)
	if err != nil {
		t.Fatalf("NewPatcher() error = %v", err)
	}
	result1 := p1.Commit()
	if !result1.Committed {
		t.Fatalf("first commit failed: %v", result1.Error)
	}

	tx2, err := s.NewTx(1, nil)
	if err != nil {
		t.Fatalf("NewTx() error = %v", err)
	}

	patch2Data, err := parse.Parse([]byte(`{age: 30}`))
	if err != nil {
		t.Fatalf("parse patch2: %v", err)
	}

	patch2 := &api.Patch{
		Patch: api.Body{
			Path: "",
			Data: patch2Data,
		},
	}
	p2, err := tx2.NewPatcher(patch2)
	if err != nil {
		t.Fatalf("NewPatcher() error = %v", err)
	}
	result2 := p2.Commit()
	if !result2.Committed {
		t.Fatalf("second commit failed: %v", result2.Error)
	}

	// Get current commit
	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit() error = %v", err)
	}
	if commit != 2 {
		t.Errorf("expected commit 2, got %d", commit)
	}

	// Switch and create snapshot
	if err := s.SwitchAndSnapshot(); err != nil {
		t.Fatalf("SwitchAndSnapshot() error = %v", err)
	}

	// Verify snapshot entry was added to index
	// Query for snapshot at commit 2
	segments := s.index.LookupWithin("", commit)
	var foundSnapshot *index.LogSegment
	for i := range segments {
		seg := &segments[i]
		if seg.StartCommit == seg.EndCommit && seg.StartCommit == commit {
			foundSnapshot = seg
			break
		}
	}

	if foundSnapshot == nil {
		t.Fatal("snapshot entry not found in index")
	}

	// Verify snapshot entry has correct fields
	if foundSnapshot.StartCommit != commit {
		t.Errorf("snapshot StartCommit = %d, want %d", foundSnapshot.StartCommit, commit)
	}
	if foundSnapshot.EndCommit != commit {
		t.Errorf("snapshot EndCommit = %d, want %d", foundSnapshot.EndCommit, commit)
	}
	if foundSnapshot.KindedPath != "" {
		t.Errorf("snapshot KindedPath = %q, want empty string", foundSnapshot.KindedPath)
	}

	// Verify we can read the snapshot entry
	entry, err := s.dLog.ReadEntryAt(s.dLog.GetInactiveLog(), foundSnapshot.LogPosition)
	if err != nil {
		t.Fatalf("ReadEntryAt() error = %v", err)
	}

	if entry.SnapPos == nil {
		t.Error("entry.SnapPos is nil, expected non-nil for snapshot entry")
	}
	if entry.Commit != commit {
		t.Errorf("entry.Commit = %d, want %d", entry.Commit, commit)
	}
	if entry.Patch != nil {
		t.Error("entry.Patch should be nil for snapshot entry")
	}

	t.Logf("Snapshot created successfully at commit %d, logFile %s, position %d, snapPos %d",
		entry.Commit, foundSnapshot.LogFile, foundSnapshot.LogPosition, *entry.SnapPos)
}
