package server

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// An established watch must end with a terminal EVENT, never an error response.
//
// This is the regression test for a measured, silent failure: a slice taking sustained
// writes lost 550 of 1000 events and never recovered. logd dropped the slow watcher and
// reported it "loudly" with an error response stamped with the watch's id; libctl routes
// anything with no Event by request id, a watch id is not in the in-flight request table
// once the watch has opened, so the failure was logged as "dropping response with no
// matching request" and discarded. The client then waited forever on a watch the server
// had already abandoned.
func TestFailWatch_EndsWithTerminalEventNotErrorResponse(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	hub := NewWatchHub()
	store.SetCommitNotifier(hub.Broadcast)

	conn := newMockConn()
	session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: hub})
	defer session.Close()

	watcher := NewWatcher("users", nil, nil, 1)
	watcher.ID = strPtr("w-1")

	const resumeCommit = int64(42)
	session.failWatch(watcher, api.ErrCodeSlowConsumer, "consumer did not keep up", resumeCommit)

	resp := <-session.outgoing

	if resp.Error != nil {
		t.Fatalf("failWatch sent an error response (%s), which a client cannot route to a watch", resp.Error.Code)
	}
	if resp.Event == nil {
		t.Fatal("failWatch sent neither an event nor an error")
	}
	if !resp.Event.Ended {
		t.Error("terminal event does not set Ended, so the client will not fail the watch")
	}
	if resp.Event.EndReason != api.ErrCodeSlowConsumer {
		t.Errorf("EndReason = %q, want %q", resp.Event.EndReason, api.ErrCodeSlowConsumer)
	}
	if resp.Event.Path != "users" {
		t.Errorf("Path = %q, want %q", resp.Event.Path, "users")
	}
	// The id routes the event to the exact watch when several share a path.
	if resp.ID == nil || *resp.ID != "w-1" {
		t.Errorf("response id = %v, want the watch's id", resp.ID)
	}
	// The resume point: a client that reconnects from here does not re-read history it
	// already has.
	if resp.Event.Commit != resumeCommit {
		t.Errorf("Commit = %d, want %d (the highest commit the watch accounted for)", resp.Event.Commit, resumeCommit)
	}
}

// The watch must also be deregistered, so the hub stops selecting it and a later watch on
// the same path is admitted.
func TestFailWatch_Deregisters(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	hub := NewWatchHub()
	conn := newMockConn()
	session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: hub})
	defer session.Close()

	watcher := NewWatcher("users", nil, nil, 1)
	watcher.ID = strPtr("w-1")
	hub.Watch(watcher)
	session.watchMu.Lock()
	session.watches[watchKey(watcher.ID, watcher.Path)] = watcher
	session.watchMu.Unlock()

	session.failWatch(watcher, api.ErrCodeSlowConsumer, "consumer did not keep up", 7)

	if n := hub.WatcherCount(); n != 0 {
		t.Errorf("hub still holds %d watchers after failWatch", n)
	}
	session.watchMu.RLock()
	n := len(session.watches)
	session.watchMu.RUnlock()
	if n != 0 {
		t.Errorf("session still holds %d watches after failWatch", n)
	}
}

func strPtr(s string) *string { return &s }
