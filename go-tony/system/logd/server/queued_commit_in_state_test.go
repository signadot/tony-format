package server

import (
	"bytes"
	"strconv"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A watch's initial state holds every commit through the commit it was read at. A
// notification for such a commit can still reach the watcher: the store publishes a
// commit before the watcher registers with the hub and dispatches it after, so the
// commit is both in the state and in the queue. It was stepped and sent as a delta the
// client already held (0f0f7jswh12ksyxxmdn0).
func TestQueuedCommitAlreadyInTheStateIsNotSentAgain(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()
	hub := NewWatchHub()
	narrowWrite(t, store, "", "{a: 1, b: 2}")
	head, _ := store.GetCurrentCommit()

	conn := newMockConn()
	session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: hub})
	done := make(chan error)
	go func() { done <- session.Run() }()
	conn.WriteRequest(`{id: "w", watch: {path: "a"}}`)
	waitFor(t, func() bool { return hub.WatcherCount() == 1 && bytes.Contains(conn.GetResponses(), []byte("state")) },
		"the watch to be established")

	// The commit the state was read at, dispatched late.
	stale, _ := parse.Parse([]byte("{a: 1, b: 2}"))
	hub.Broadcast(&storage.CommitNotification{Commit: head, KPaths: []string{"a", "b"}, Patch: stale})
	time.Sleep(200 * time.Millisecond)

	// Then a real change, which is owed.
	store.SetCommitNotifier(hub.Broadcast)
	narrowWrite(t, store, "", "{a: 5}")
	after, _ := store.GetCurrentCommit()
	waitFor(t, func() bool {
		return bytes.Contains(conn.GetResponses(), []byte("commit: "+strconv.FormatInt(after, 10)))
	},
		"the real change to be delivered")

	conn.Close()
	<-done

	var patches []*api.WatchEvent
	for _, line := range bytes.Split(bytes.TrimSpace(conn.GetResponses()), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var resp api.SessionResponse
		if err := resp.FromTony(line); err != nil {
			t.Fatalf("parse %q: %s", line, err)
		}
		if resp.Event != nil && resp.Event.Patch != nil {
			patches = append(patches, resp.Event)
		}
	}
	if len(patches) != 1 || patches[0].Commit != after {
		for _, p := range patches {
			t.Logf("patch event at commit %d", p.Commit)
		}
		t.Fatalf("%d patch events, want one at commit %d and none for commit %d, which the state holds", len(patches), after, head)
	}
}
