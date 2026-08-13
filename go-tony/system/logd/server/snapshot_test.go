package server

import (
	"fmt"
	"os"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// mustParse parses a Tony document or panics.
func mustParse(s string) *ir.Node {
	node, err := parse.Parse([]byte(s))
	if err != nil {
		panic(err)
	}
	return node
}

// doPatch applies a patch via storage transaction and triggers onCommit.
func doPatch(t *testing.T, store *storage.Storage, srv *Server, path string, data *ir.Node) {
	t.Helper()

	tx, err := store.NewTx(1, nil)
	if err != nil {
		t.Fatalf("NewTx failed: %v", err)
	}

	patcher, err := tx.NewPatcher(&api.Patch{
		PathData: api.PathData{
			Path: path,
			Data: data,
		},
	})
	if err != nil {
		t.Fatalf("NewPatcher failed: %v", err)
	}

	result := patcher.Commit()
	if result.Error != nil {
		t.Fatalf("Commit failed: %v", result.Error)
	}

	srv.onCommit()
}

func TestAutoSnapshotByCommits(t *testing.T) {
	// Create temp directory
	dir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Open storage
	store, err := storage.Open(dir, nil)
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

	// Make commits
	doPatch(t, store, srv, "users", mustParse(`{id: "1", name: "Alice"}`))
	if srv.commitsSinceSnapshot.Load() != 1 {
		t.Errorf("expected 1 commit, got %d", srv.commitsSinceSnapshot.Load())
	}

	doPatch(t, store, srv, "users", mustParse(`{id: "2", name: "Bob"}`))
	if srv.commitsSinceSnapshot.Load() != 2 {
		t.Errorf("expected 2 commits, got %d", srv.commitsSinceSnapshot.Load())
	}

	doPatch(t, store, srv, "users", mustParse(`{id: "3", name: "Charlie"}`))
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
	store, err := storage.Open(dir, nil)
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

	// Check initial size
	size1, _ := store.ActiveLogSize()
	t.Logf("Initial log size: %d", size1)

	// Make a commit with some data
	doPatch(t, store, srv, "users", mustParse(`{id: "1", name: "Alice", email: "alice@example.com", bio: "A long bio text to increase the size of the log entry"}`))
	size2, _ := store.ActiveLogSize()
	t.Logf("After 1st commit, log size: %d, commits: %d", size2, srv.commitsSinceSnapshot.Load())

	doPatch(t, store, srv, "posts", mustParse(`{id: "1", title: "Hello World", content: "This is a test post with some content to increase size"}`))
	size3, _ := store.ActiveLogSize()
	t.Logf("After 2nd commit, log size: %d, commits: %d", size3, srv.commitsSinceSnapshot.Load())

	// Keep adding until snapshot triggers
	for i := 0; i < 10 && srv.commitsSinceSnapshot.Load() > 0; i++ {
		doPatch(t, store, srv, "data", mustParse(fmt.Sprintf(`{i: %d, padding: "more data to fill up the log"}`, i)))
		size, _ := store.ActiveLogSize()
		t.Logf("After commit %d, log size: %d, commits: %d", i+3, size, srv.commitsSinceSnapshot.Load())
	}

	if srv.commitsSinceSnapshot.Load() != 0 {
		t.Errorf("expected snapshot to trigger by size, but commits=%d", srv.commitsSinceSnapshot.Load())
	}
}

// A size threshold bounds the delta a read replays; it is not a cliff the store
// falls off. Read against the whole active log — which never comes back down, since
// a switch does not empty the log it switches into — the threshold was crossed once
// and then never uncrossed: the store snapshotted on EVERY commit for the rest of
// its life, and 400 writes to a 100 KB store left 4 MB of log behind
// (issue ps8kfs9dh12kr777fnn0).
func TestAutoSnapshotBySize_IsABoundNotACliff(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Bytes only: any reset of the commit counter below is a snapshot this policy
	// took because of the size.
	srv := New(&Spec{
		Config:  &Config{Snapshot: &SnapshotConfig{MaxBytes: 3000}},
		Storage: store,
	})

	const commits = 60
	snapshots := 0
	for i := 0; i < commits; i++ {
		doPatch(t, store, srv, "data", mustParse(fmt.Sprintf(
			`{i: %d, padding: "a body long enough that a few of these cross three kilobytes"}`, i)))
		if srv.commitsSinceSnapshot.Load() == 0 {
			snapshots++
		}
	}

	if snapshots == 0 {
		t.Fatalf("no snapshot in %d commits: the size threshold never fired", commits)
	}
	// Each snapshot has to be earned with another MaxBytes of delta. Snapshotting on
	// every commit is the failure; this bound is far looser than the real ratio and
	// still an order of magnitude below it.
	if snapshots > commits/3 {
		t.Errorf("%d snapshots in %d commits: the threshold is firing on nearly every commit",
			snapshots, commits)
	}
	t.Logf("%d snapshots in %d commits", snapshots, commits)
}
