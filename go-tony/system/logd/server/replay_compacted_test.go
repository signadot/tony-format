package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A watch whose fromCommit predates the retained delta window must be told so
// specifically. Before the replay floor, the store answered with whatever patches
// happened to survive and reported success, so the client took erased history for a
// quiet period. "replay_failed" would not do either: it reads as a transient fault worth
// retrying with the same doomed cursor, where "replay_compacted" tells the client to
// re-watch without fromCommit and re-initialize.
func TestSession_WatchBelowReplayFloorReportsCompacted(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	hub := NewWatchHub()
	store.SetCommitNotifier(hub.Broadcast)

	for i := 1; i <= 3; i++ {
		tx, err := store.NewTx(1, nil)
		if err != nil {
			t.Fatalf("NewTx: %v", err)
		}
		data, _ := parse.Parse(fmt.Appendf(nil, `{users: {user%d: {name: "User %d"}}}`, i, i))
		p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: data}})
		if err != nil {
			t.Fatalf("NewPatcher: %v", err)
		}
		if r := p.Commit(); !r.Committed {
			t.Fatalf("commit %d: %v", i, r.Error)
		}
	}

	// Compact the whole history away, as a cutoff older than every patch would.
	if err := store.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	cfg := storage.DefaultCompactionConfig()
	cfg.Cutoff = -time.Hour
	if err := store.Compact(cfg); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if store.ReplayFloor() == 0 {
		t.Fatal("expected a non-zero replay floor after compaction")
	}

	conn := newMockConn()
	conn.WriteRequest(`{watch: {path: users, fromCommit: 1}}`)

	session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: hub})
	done := make(chan error)
	go func() { done <- session.Run() }()

	time.Sleep(100 * time.Millisecond)
	conn.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session did not complete")
	}

	responses := conn.GetResponses()
	t.Logf("Responses: %s", responses)

	var gotCode string
	var sawReplayComplete bool
	var sawDataEvent bool
	for _, doc := range splitTonyDocs(responses) {
		var resp api.SessionResponse
		if err := resp.FromTony(doc); err != nil {
			continue
		}
		if resp.Error != nil {
			gotCode = resp.Error.Code
		}
		if resp.Event != nil && resp.Event.ReplayComplete {
			sawReplayComplete = true
		}
		if resp.Event != nil && (resp.Event.State != nil || resp.Event.Patch != nil) {
			sawDataEvent = true
		}
	}

	if gotCode != api.ErrCodeReplayCompacted {
		t.Errorf("error code = %q, want %q", gotCode, api.ErrCodeReplayCompacted)
	}
	if sawReplayComplete {
		t.Error("replay reported complete despite history having been compacted away")
	}
	// Nothing may go out ahead of the error: a state read below the floor is approximate,
	// so an init state event here would be telling the client something untrue.
	if sawDataEvent {
		t.Errorf("sent state/patch data before failing a doomed cursor: %s", responses)
	}
}

// A watch above the floor still replays normally — the guard must not reject a cursor
// whose history is intact.
func TestSession_WatchAboveReplayFloorReplays(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	hub := NewWatchHub()
	store.SetCommitNotifier(hub.Broadcast)

	for i := 1; i <= 3; i++ {
		tx, err := store.NewTx(1, nil)
		if err != nil {
			t.Fatalf("NewTx: %v", err)
		}
		data, _ := parse.Parse(fmt.Appendf(nil, `{users: {user%d: {name: "User %d"}}}`, i, i))
		p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: data}})
		if err != nil {
			t.Fatalf("NewPatcher: %v", err)
		}
		if r := p.Commit(); !r.Committed {
			t.Fatalf("commit %d: %v", i, r.Error)
		}
	}

	conn := newMockConn()
	conn.WriteRequest(`{watch: {path: users, fromCommit: 1}}`)

	session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: hub})
	done := make(chan error)
	go func() { done <- session.Run() }()

	time.Sleep(100 * time.Millisecond)
	conn.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("session did not complete")
	}

	var sawReplayComplete bool
	var gotCode string
	for _, doc := range splitTonyDocs(conn.GetResponses()) {
		var resp api.SessionResponse
		if err := resp.FromTony(doc); err != nil {
			continue
		}
		if resp.Error != nil {
			gotCode = resp.Error.Code
		}
		if resp.Event != nil && resp.Event.ReplayComplete {
			sawReplayComplete = true
		}
	}

	if gotCode != "" {
		t.Errorf("unexpected error code %q for an intact cursor", gotCode)
	}
	if !sawReplayComplete {
		t.Error("expected replay to complete for a cursor above the floor")
	}
}
