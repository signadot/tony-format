package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

func TestAutoSnapshotByCommits(t *testing.T) {
	// Create temp directory
	dir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Open storage
	store, err := storage.Open(dir, 0755, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Create server with maxCommits=3
	cfg := &Config{
		Snapshot: &SnapshotConfig{
			MaxCommits: 3,
		},
	}
	srv := New(&Spec{
		Config:  cfg,
		Storage: store,
	})

	// Helper to make PATCH request
	doPatch := func(path, data string) {
		body := []byte(`{patch: {path: "` + path + `", data: ` + data + `}}`)
		req := httptest.NewRequest("PATCH", "/api/data", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/x-tony")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("PATCH failed: %d %s", w.Code, w.Body.String())
		}
	}

	// Make commits
	doPatch("users", `{id: "1", name: "Alice"}`)
	if srv.commitsSinceSnapshot.Load() != 1 {
		t.Errorf("expected 1 commit, got %d", srv.commitsSinceSnapshot.Load())
	}

	doPatch("users", `{id: "2", name: "Bob"}`)
	if srv.commitsSinceSnapshot.Load() != 2 {
		t.Errorf("expected 2 commits, got %d", srv.commitsSinceSnapshot.Load())
	}

	doPatch("users", `{id: "3", name: "Charlie"}`)
	// After 3rd commit, snapshot should trigger and reset counter
	if srv.commitsSinceSnapshot.Load() != 0 {
		t.Errorf("expected 0 commits after snapshot, got %d", srv.commitsSinceSnapshot.Load())
	}

	t.Logf("Auto-snapshot triggered after 3 commits")
}

func TestAutoSnapshotBySize(t *testing.T) {
	// Create temp directory
	dir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Open storage
	store, err := storage.Open(dir, 0755, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Create server with small maxBytes
	cfg := &Config{
		Snapshot: &SnapshotConfig{
			MaxBytes: 500, // Small threshold
		},
	}
	srv := New(&Spec{
		Config:  cfg,
		Storage: store,
	})

	// Helper to make PATCH request
	doPatch := func(path, data string) {
		body := []byte(`{patch: {path: "` + path + `", data: ` + data + `}}`)
		req := httptest.NewRequest("PATCH", "/api/data", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/x-tony")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("PATCH failed: %d %s", w.Code, w.Body.String())
		}
	}

	// Check initial size
	size1, _ := store.ActiveLogSize()
	t.Logf("Initial log size: %d", size1)

	// Make a commit with some data
	doPatch("users", `{id: "1", name: "Alice", email: "alice@example.com", bio: "A long bio text to increase the size of the log entry"}`)
	size2, _ := store.ActiveLogSize()
	t.Logf("After 1st commit, log size: %d, commits: %d", size2, srv.commitsSinceSnapshot.Load())

	doPatch("posts", `{id: "1", title: "Hello World", content: "This is a test post with some content to increase size"}`)
	size3, _ := store.ActiveLogSize()
	t.Logf("After 2nd commit, log size: %d, commits: %d", size3, srv.commitsSinceSnapshot.Load())

	// Keep adding until snapshot triggers
	for i := 0; i < 10 && srv.commitsSinceSnapshot.Load() > 0; i++ {
		doPatch("data", `{i: `+string(rune('0'+i))+`, padding: "more data to fill up the log"}`)
		size, _ := store.ActiveLogSize()
		t.Logf("After commit %d, log size: %d, commits: %d", i+3, size, srv.commitsSinceSnapshot.Load())
	}

	if srv.commitsSinceSnapshot.Load() != 0 {
		t.Errorf("expected snapshot to trigger by size, but commits=%d", srv.commitsSinceSnapshot.Load())
	}
}
