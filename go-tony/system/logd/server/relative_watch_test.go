package server

import (
	"bytes"
	"strconv"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A watch may say how much history it wants without knowing where the store is: a
// negative fromCommit is relative to the watermark, so -N is "the last N commits".
// Naming a commit requires knowing one; asking for a window does not.
func TestRelativeWatchCursor(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()
	for i := 0; i < 20; i++ {
		narrowWrite(t, store, "verse.entities.e"+strconv.Itoa(i), "{n: "+strconv.Itoa(i)+"}")
	}
	head, err := store.GetCurrentCommit()
	if err != nil {
		t.Fatalf("commit: %s", err)
	}

	for _, tc := range []struct {
		name    string
		request string
		from    int64 // the commit the replay must start from
		replays bool
	}{
		{"the last five", `{id: "w", watch: {path: "verse.entities", fromCommit: -5}}`, head - 5, true},
		{"the last one", `{id: "w", watch: {path: "verse.entities", fromCommit: -1}}`, head - 1, true},
		{"more than there is", `{id: "w", watch: {path: "verse.entities", fromCommit: -1000}}`, 0, true},
		{"no history", `{id: "w", watch: {path: "verse.entities"}}`, 0, false},
		{"absolute, as before", `{id: "w", watch: {path: "verse.entities", fromCommit: 3}}`, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var w *api.WatchResult
			for _, resp := range narrowRequestAll(t, store, tc.request) {
				if resp.Error != nil {
					t.Fatalf("watch: %s", resp.Error)
				}
				if resp.Result != nil && resp.Result.Watch != nil {
					w = resp.Result.Watch
				}
			}
			if w == nil {
				t.Fatal("no watch result")
			}
			if !tc.replays {
				if w.ReplayingFrom != nil || w.ReplayingTo != nil {
					t.Errorf("a watch with no cursor is replaying: from %v to %v", w.ReplayingFrom, w.ReplayingTo)
				}
				return
			}
			if w.ReplayingFrom == nil {
				t.Fatalf("the watch is replaying but does not say from where: %+v", w)
			}
			if *w.ReplayingFrom != tc.from {
				t.Errorf("replaying from %d, want %d (head is %d)", *w.ReplayingFrom, tc.from, head)
			}
			if w.ReplayingTo == nil || *w.ReplayingTo != head {
				t.Errorf("replaying to %v, want %d", w.ReplayingTo, head)
			}
		})
	}
}

// -N and the absolute commit it resolves to must be the same watch. A relative cursor is
// a convenience for naming the commit, not a second kind of replay.
func TestRelativeAndAbsoluteWatchAgree(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()
	for i := 0; i < 12; i++ {
		narrowWrite(t, store, "verse.entities.e"+strconv.Itoa(i), "{n: "+strconv.Itoa(i)+"}")
	}
	head, _ := store.GetCurrentCommit()

	relative := narrowRequestEvents(t, store, `{id: "w", watch: {path: "verse.entities", fromCommit: -4}}`)
	absolute := narrowRequestEvents(t, store, `{id: "w", watch: {path: "verse.entities", fromCommit: `+strconv.FormatInt(head-4, 10)+`}}`)

	if len(relative) == 0 {
		t.Fatal("the relative watch replayed nothing")
	}
	if len(relative) != len(absolute) {
		t.Fatalf("relative gave %d events, absolute %d", len(relative), len(absolute))
	}
	for i := range relative {
		if relative[i].Commit != absolute[i].Commit {
			t.Errorf("event %d: relative at commit %d, absolute at %d", i, relative[i].Commit, absolute[i].Commit)
		}
	}
}

// narrowRequestAll runs one request and answers every response it produced -- a watch
// answers with a result and then events, which narrowRequest's single-document parse
// cannot hold.
func narrowRequestAll(t *testing.T, store *storage.Storage, request string) []*api.SessionResponse {
	t.Helper()
	conn := newMockConn()
	conn.WriteRequest(request)
	session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: NewWatchHub()})
	done := make(chan error)
	go func() { done <- session.Run() }()
	time.Sleep(200 * time.Millisecond)
	conn.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("session did not complete")
	}

	var out []*api.SessionResponse
	for _, line := range bytes.Split(bytes.TrimSpace(conn.GetResponses()), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var resp api.SessionResponse
		if err := resp.FromTony(line); err != nil {
			t.Fatalf("parse %q: %s", line, err)
		}
		out = append(out, &resp)
	}
	return out
}

// narrowRequestEvents runs one request and answers the watch events it produced.
func narrowRequestEvents(t *testing.T, store *storage.Storage, request string) []*api.WatchEvent {
	t.Helper()
	var events []*api.WatchEvent
	for _, resp := range narrowRequestAll(t, store, request) {
		if resp.Event != nil && !resp.Event.ReplayComplete {
			events = append(events, resp.Event)
		}
	}
	return events
}
