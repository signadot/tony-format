package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
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
	var sawErrorResponse bool
	for _, doc := range splitTonyDocs(responses) {
		var resp api.SessionResponse
		if err := resp.FromTony(doc); err != nil {
			continue
		}
		if resp.Error != nil {
			sawErrorResponse = true
		}
		if resp.Event != nil && resp.Event.Ended {
			gotCode = resp.Event.EndReason
		}
		if resp.Event != nil && resp.Event.ReplayComplete {
			sawReplayComplete = true
		}
		if resp.Event != nil && (resp.Event.State != nil || resp.Event.Patch != nil) {
			sawDataEvent = true
		}
	}

	if gotCode != api.ErrCodeReplayCompacted {
		t.Errorf("terminal event reason = %q, want %q", gotCode, api.ErrCodeReplayCompacted)
	}
	// An error response would be routed by request id, and this watch's request already
	// completed — the client would drop it and wait forever. See Session.failWatch.
	if sawErrorResponse {
		t.Errorf("ended an established watch with an error response, which the client cannot route: %s", responses)
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
		if resp.Event != nil && resp.Event.Ended {
			gotCode = resp.Event.EndReason
		}
		if resp.Event != nil && resp.Event.ReplayComplete {
			sawReplayComplete = true
		}
	}

	if gotCode != "" {
		t.Errorf("unexpected failure %q for an intact cursor", gotCode)
	}
	if !sawReplayComplete {
		t.Error("expected replay to complete for a cursor above the floor")
	}
}

// A cursor at or above the floor has its deltas, but a watch starts from the STATE at the
// cursor, and that can be one the store no longer answers exactly: a read there is
// answered at the first root snapshot at or after it. The watch starts there too, and
// says so (er3dnqpkh12krsb2qxn0).
//
// Here baseline patches 1 and 2 are compacted away and the only snapshot is at 3, a
// scope's commit, which compaction keeps. So the floor is 2, the state at 2 is gone, and
// a watch from 2 starts at 3 -- unless it holds its own state, when nothing would tell
// it so, and it is refused. The ping's floor is where a watch starts as asked, which a
// router resolving a relative cursor goes by.
func TestSession_WatchFromAStateCompactionTook(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	sandbox := "sandbox"
	writeAt(t, store, nil, "users.a", "{n: 1}")
	writeAt(t, store, nil, "users.b", "{n: 2}")
	writeAt(t, store, &sandbox, "users.s", "{n: 3}")
	if err := store.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	cfg := storage.DefaultCompactionConfig()
	cfg.Cutoff = -time.Hour
	if err := store.Compact(cfg); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if floor := store.ReplayFloor(); floor != 2 {
		t.Fatalf("floor %d, want 2: the scope's commit at 3 was not kept", floor)
	}
	writeAt(t, store, nil, "users.c", "{n: 4}")

	for _, tc := range []struct {
		name, request string
		code          string // the watch is refused or ended with it
		from          int64  // where it starts: its state's commit, and replayingFrom
	}{
		{name: "from the floor", request: `watch: {path: users, fromCommit: 2}`, from: 3},
		{name: "from the snapshot", request: `watch: {path: users, fromCommit: 3}`, from: 3},
		{name: "everything there is", request: `watch: {path: users, fromCommit: -1000}`, from: 3},
		{name: "holding its own state", request: `watch: {path: users, fromCommit: 2, noInit: true}`, code: api.ErrCodeReplayCompacted},
		{name: "below the floor", request: `watch: {path: users, fromCommit: 1}`, code: api.ErrCodeReplayCompacted},
		{name: "the ping's floor", request: `ping: {}`, from: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var code string
			var state *api.WatchEvent
			var result *api.WatchResult
			deltas := 0
			for _, resp := range narrowRequestAll(t, store, `{id: "w", `+tc.request+`}`) {
				switch {
				case resp.Error != nil:
					code = resp.Error.Code
				case resp.Result != nil && resp.Result.Pong != nil:
					if resp.Result.Pong.Floor != tc.from {
						t.Errorf("floor %d, want %d", resp.Result.Pong.Floor, tc.from)
					}
					return
				case resp.Result != nil && resp.Result.Watch != nil:
					result = resp.Result.Watch
				case resp.Event != nil && resp.Event.Ended:
					code = resp.Event.EndReason
				case resp.Event != nil && resp.Event.State != nil && state == nil:
					state = resp.Event
				case resp.Event != nil && resp.Event.Patch != nil:
					deltas++
				}
			}
			if code != tc.code {
				t.Fatalf("ended with %q, want %q", code, tc.code)
			}
			if tc.code != "" {
				if state != nil {
					t.Errorf("sent a state before refusing: %+v", state)
				}
				return
			}
			if result == nil || result.ReplayingFrom == nil || *result.ReplayingFrom != tc.from {
				t.Errorf("watch result %+v, want replayingFrom %d", result, tc.from)
			}
			if state == nil || state.Commit != tc.from {
				t.Fatalf("initial state %+v, want one at %d", state, tc.from)
			}
			if ir.Get(state.State, "a") == nil || ir.Get(state.State, "b") == nil || len(state.State.Fields) != 2 {
				t.Errorf("the state at %d is not {a, b}: %+v", tc.from, state.State)
			}
			if deltas != 1 {
				t.Errorf("%d deltas after the state, want commit 4's alone", deltas)
			}
		})
	}
}
