package storage

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
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

func TestStorage_CommitNotifier(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Track notifications
	var mu sync.Mutex
	var notifications []*CommitNotification

	s.SetCommitNotifier(func(n *CommitNotification) {
		mu.Lock()
		defer mu.Unlock()
		notifications = append(notifications, n)
	})

	// First commit: {users: {alice: {name: "Alice"}}}
	tx1, err := s.NewTx(1, nil)
	if err != nil {
		t.Fatalf("NewTx() error = %v", err)
	}

	patch1Data, err := parse.Parse([]byte(`{users: {alice: {name: "Alice"}}}`))
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

	// Verify notification was received
	mu.Lock()
	if len(notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifications))
	}
	n1 := notifications[0]
	mu.Unlock()

	if n1.Commit != 1 {
		t.Errorf("expected commit 1, got %d", n1.Commit)
	}
	if n1.TxSeq != 1 {
		t.Errorf("expected txSeq 1, got %d", n1.TxSeq)
	}
	if n1.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
	if len(n1.KPaths) != 1 || n1.KPaths[0] != "users" {
		t.Errorf("expected kpaths [users], got %v", n1.KPaths)
	}
	if n1.Patch == nil {
		t.Error("expected non-nil patch")
	}

	// Second commit: {posts: {p1: {title: "Hello"}}}
	tx2, err := s.NewTx(1, nil)
	if err != nil {
		t.Fatalf("NewTx() error = %v", err)
	}

	patch2Data, err := parse.Parse([]byte(`{posts: {p1: {title: "Hello"}}}`))
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

	// Verify second notification
	mu.Lock()
	if len(notifications) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(notifications))
	}
	n2 := notifications[1]
	mu.Unlock()

	if n2.Commit != 2 {
		t.Errorf("expected commit 2, got %d", n2.Commit)
	}
	if len(n2.KPaths) != 1 || n2.KPaths[0] != "posts" {
		t.Errorf("expected kpaths [posts], got %v", n2.KPaths)
	}

	// Verify GetCommitNotifier returns the notifier
	if s.GetCommitNotifier() == nil {
		t.Error("expected GetCommitNotifier() to return non-nil")
	}

	// Clear notifier
	s.SetCommitNotifier(nil)
	if s.GetCommitNotifier() != nil {
		t.Error("expected GetCommitNotifier() to return nil after clearing")
	}

	// Third commit should not notify
	tx3, err := s.NewTx(1, nil)
	if err != nil {
		t.Fatalf("NewTx() error = %v", err)
	}

	patch3Data, err := parse.Parse([]byte(`{comments: []}`))
	if err != nil {
		t.Fatalf("parse patch3: %v", err)
	}

	patch3 := &api.Patch{
		Patch: api.Body{
			Path: "",
			Data: patch3Data,
		},
	}
	p3, err := tx3.NewPatcher(patch3)
	if err != nil {
		t.Fatalf("NewPatcher() error = %v", err)
	}
	result3 := p3.Commit()
	if !result3.Committed {
		t.Fatalf("third commit failed: %v", result3.Error)
	}

	// Verify no new notifications
	mu.Lock()
	if len(notifications) != 2 {
		t.Errorf("expected 2 notifications after clearing notifier, got %d", len(notifications))
	}
	mu.Unlock()
}
