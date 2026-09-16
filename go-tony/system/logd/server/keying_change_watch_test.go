package server

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A commit that changes an array's keying ends every watch overlapping the array -- at it,
// under it, and above it -- with keying_changed at that commit, rather than handing the
// watcher a rewrite it would have to interpret: an element renamed reads as deleted, an
// address stops naming anything, and the array's elements are addressed another way from
// then on (62r9amwph12krxfjn9n0).

const (
	keyedByID  = `{define: {runs: {id: !logd-key null}}}`
	keyedBySKU = `{define: {runs: {sku: !logd-key null}}}`
	notKeyed   = `{define: {runs: {id: null}}}`
)

// liveSession is a session over store with its commits wired to the hub, as server.go
// wires them, whose responses can be read while it runs.
type liveSession struct {
	t    *testing.T
	conn *mockConn
	hub  *WatchHub
	done chan error
}

func newLiveSession(t *testing.T, store *storage.Storage) *liveSession {
	t.Helper()
	hub := NewWatchHub()
	store.SetCommitNotifier(hub.Broadcast)
	conn := newMockConn()
	session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: hub})
	done := make(chan error, 1)
	go func() { done <- session.Run() }()
	ls := &liveSession{t: t, conn: conn, hub: hub, done: done}
	t.Cleanup(ls.close)
	return ls
}

func (ls *liveSession) send(req string) { ls.conn.WriteRequest(req) }

func (ls *liveSession) close() {
	ls.conn.Close()
	select {
	case <-ls.done:
	case <-time.After(2 * time.Second):
		ls.t.Error("session did not complete")
	}
}

// byID answers what the session has sent so far, by request id.
func (ls *liveSession) byID() map[string][]*api.SessionResponse {
	ls.t.Helper()
	out := map[string][]*api.SessionResponse{}
	for _, line := range bytes.Split(bytes.TrimSpace(ls.conn.GetResponses()), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var resp api.SessionResponse
		if err := resp.FromTony(bytes.TrimSpace(line)); err != nil {
			ls.t.Fatalf("parse %s: %v", line, err)
		}
		id := ""
		if resp.ID != nil {
			id = *resp.ID
		}
		out[id] = append(out[id], &resp)
	}
	return out
}

// until waits for cond over what the session has sent.
func (ls *liveSession) until(what string, cond func(map[string][]*api.SessionResponse) bool) map[string][]*api.SessionResponse {
	ls.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := ls.byID()
		if cond(got) {
			return got
		}
		if time.Now().After(deadline) {
			ls.t.Fatalf("waited 3s for %s; the session sent %s", what, ls.conn.GetResponses())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// schemaCommit sets the schema over the wire and answers the commit it took.
func (ls *liveSession) schemaCommit(id, schema string, force bool) int64 {
	ls.t.Helper()
	ls.send(fmt.Sprintf(`{id: %q, schema: {set: {schema: %s, force: %v}}}`, id, schema, force))
	got := ls.until("the schema set", func(m map[string][]*api.SessionResponse) bool { return len(m[id]) > 0 })
	r := got[id][0]
	if r.Error != nil || r.Result == nil || r.Result.Schema == nil {
		ls.t.Fatalf("schema set answered %+v", r)
	}
	return r.Result.Schema.Commit
}

// ended answers the terminal event among a watch's responses, or nil.
func ended(rs []*api.SessionResponse) *api.WatchEvent {
	for _, r := range rs {
		if r.Event != nil && r.Event.Ended {
			return r.Event
		}
	}
	return nil
}

// deltasFrom answers a watch's patch events at or after commit.
func deltasFrom(rs []*api.SessionResponse, commit int64) []*api.WatchEvent {
	var out []*api.WatchEvent
	for _, r := range rs {
		if ev := r.Event; ev != nil && !ev.Ended && !ev.ReplayComplete && ev.State == nil && ev.Commit >= commit {
			if ev.Patch != nil || ev.Absent {
				out = append(out, ev)
			}
		}
	}
	return out
}

func setSchema(t *testing.T, store *storage.Storage, schema string, force bool) int64 {
	t.Helper()
	n, err := parse.Parse([]byte(schema))
	if err != nil {
		t.Fatalf("parse schema %s: %v", schema, err)
	}
	commit, err := store.SetSchema(n, force)
	if err != nil {
		t.Fatalf("SetSchema %s: %v", schema, err)
	}
	return commit
}

// TestWatch_KeyingChangeEndsOverlappingWatches: losing, gaining and changing an array's
// identity each end the watches at the array, under it and above it, at the schema commit
// and with nothing delivered for that commit first; a watch on a path beside it --
// including one whose name merely starts the same way -- carries on.
func TestWatch_KeyingChangeEndsOverlappingWatches(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		force          bool
		element        string // the element's path as the first schema spells it
	}{
		{"lose", keyedByID, notKeyed, true, "runs(r1)"},
		{"gain", notKeyed, keyedByID, false, "runs[0]"},
		{"re-key", keyedByID, keyedBySKU, false, "runs(r1)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := openStore(t)
			setSchema(t, store, tc.from, false)
			narrowWrite(t, store, "", `{runs: [{id: r1, sku: A, n: 1}, {id: r2, sku: B, n: 2}], runs2: [{n: 1}], other: {a: 1}}`)

			ls := newLiveSession(t, store)
			watches := map[string]string{
				"root":    "",
				"array":   "runs",
				"element": tc.element,
				"under":   tc.element + ".n",
				"prefix":  "runs2",
				"other":   "other",
			}
			for id, path := range watches {
				ls.send(fmt.Sprintf(`{id: %q, watch: {path: %q}}`, id, path))
			}
			waitFor(t, func() bool { return ls.hub.WatcherCount() == len(watches) }, "the watches")

			c := ls.schemaCommit("s", tc.to, tc.force)
			ls.send(`{id: "p1", patch: {path: "other", data: {a: 2}}}`)
			ls.send(`{id: "p2", patch: {path: "runs2[0]", data: {n: 2}}}`)
			got := ls.until("the writes after the change to reach the watches beside it", func(m map[string][]*api.SessionResponse) bool {
				return len(deltasFrom(m["other"], c+1)) > 0 && len(deltasFrom(m["prefix"], c+1)) > 0
			})

			for _, id := range []string{"root", "array", "element", "under"} {
				end := ended(got[id])
				if end == nil {
					t.Errorf("%s (%q) was not ended; answered %d responses", id, watches[id], len(got[id]))
					continue
				}
				if end.EndReason != api.ErrCodeKeyingChanged || end.Commit != c || !strings.Contains(end.EndMessage, `"runs"`) {
					t.Errorf("%s ended with %s at %d (%q), want %s at %d naming runs",
						id, end.EndReason, end.Commit, end.EndMessage, api.ErrCodeKeyingChanged, c)
				}
				if d := deltasFrom(got[id], c); len(d) != 0 {
					t.Errorf("%s was handed %d delta(s) at or after the change: %+v", id, len(d), d[0])
				}
			}
			for _, id := range []string{"prefix", "other"} {
				if end := ended(got[id]); end != nil {
					t.Errorf("%s (%q) beside the array was ended: %s %s", id, watches[id], end.EndReason, end.EndMessage)
				}
			}
			// Ended watches are out of the hub, so none is left for an unwatch that can no
			// longer spell it.
			if n := ls.hub.WatcherCount(); n != 2 {
				t.Errorf("%d watchers in the hub, want the 2 beside the array", n)
			}
		})
	}
}

// TestWatch_KeyingChangeOnAnAbsentArrayEnds: a schema that keys an array holding nothing
// rewrites nothing, and still changes how the path is addressed, so the watches over it end.
func TestWatch_KeyingChangeOnAnAbsentArrayEnds(t *testing.T) {
	store := openStore(t)
	narrowWrite(t, store, "", `{other: {a: 1}}`)
	ls := newLiveSession(t, store)
	ls.send(`{id: "root", watch: {path: ""}}`)
	ls.send(`{id: "array", watch: {path: "runs", waitIfAbsent: true}}`)
	ls.send(`{id: "other", watch: {path: "other"}}`)
	waitFor(t, func() bool { return ls.hub.WatcherCount() == 3 }, "the watches")

	c := ls.schemaCommit("s", keyedByID, false)
	got := ls.until("the watches over runs to end", func(m map[string][]*api.SessionResponse) bool {
		return ended(m["root"]) != nil && ended(m["array"]) != nil
	})
	for _, id := range []string{"root", "array"} {
		if end := ended(got[id]); end.EndReason != api.ErrCodeKeyingChanged || end.Commit != c {
			t.Errorf("%s ended with %s at %d, want %s at %d", id, end.EndReason, end.Commit, api.ErrCodeKeyingChanged, c)
		}
	}
	if end := ended(got["other"]); end != nil {
		t.Errorf("other was ended: %s", end.EndReason)
	}
}

// TestWatch_KeyingChangeEndsScopedWatches: a scope reads under the store's one schema, so
// a watch in a scoped session over the array ends as a baseline one does, and one beside it
// carries on.
func TestWatch_KeyingChangeEndsScopedWatches(t *testing.T) {
	store := openStore(t)
	setSchema(t, store, keyedByID, false)
	narrowWrite(t, store, "", `{runs: [{id: r1, sku: A, n: 1}], other: {a: 1}}`)
	ls := newLiveSession(t, store)
	ls.send(`{hello: {clientId: c, protocol: 3, scope: s1}}`)
	ls.send(`{id: "element", watch: {path: "runs(r1)"}}`)
	ls.send(`{id: "other", watch: {path: "other"}}`)
	waitFor(t, func() bool { return ls.hub.WatcherCount() == 2 }, "the watches")

	c := setSchema(t, store, keyedBySKU, false) // a scoped session may not set one
	narrowWrite(t, store, "other", `{a: 2}`)
	got := ls.until("the element's watch to end and the other's to step", func(m map[string][]*api.SessionResponse) bool {
		return ended(m["element"]) != nil && len(deltasFrom(m["other"], c+1)) > 0
	})
	if end := ended(got["element"]); end.EndReason != api.ErrCodeKeyingChanged || end.Commit != c {
		t.Errorf("the scoped element watch ended with %s at %d, want %s at %d", end.EndReason, end.Commit, api.ErrCodeKeyingChanged, c)
	}
	if end := ended(got["other"]); end != nil {
		t.Errorf("the scoped watch beside the array was ended: %s", end.EndReason)
	}
}

// TestWatch_SchemaSetThatKeepsKeyingEndsNothing: a schema commit that changes no array's
// keying is not a reason to end anything.
func TestWatch_SchemaSetThatKeepsKeyingEndsNothing(t *testing.T) {
	store := openStore(t)
	setSchema(t, store, keyedByID, false)
	narrowWrite(t, store, "", `{runs: [{id: r1, n: 1}]}`)
	ls := newLiveSession(t, store)
	ls.send(`{id: "root", watch: {path: ""}}`)
	ls.send(`{id: "element", watch: {path: "runs(r1)"}}`)
	waitFor(t, func() bool { return ls.hub.WatcherCount() == 2 }, "the watches")

	c := ls.schemaCommit("s", `{define: {runs: {id: !logd-key null}, jobs: {x: null}}}`, false)
	ls.send(`{id: "p", patch: {path: "runs(r1)", data: {n: 2}}}`)
	got := ls.until("the write to reach both watches", func(m map[string][]*api.SessionResponse) bool {
		return len(deltasFrom(m["root"], c+1)) > 0 && len(deltasFrom(m["element"], c+1)) > 0
	})
	for _, id := range []string{"root", "element"} {
		if end := ended(got[id]); end != nil {
			t.Errorf("%s was ended by a schema that changed no keying: %s", id, end.EndReason)
		}
	}
}

// TestWatch_ReplayAcrossKeyingChange: a replay whose range crosses a keying change over
// its path sends what it can say before the change and ends at the change, as a live watch
// would have; one whose path does not name the same place before the change says nothing
// but the ending; and one resuming from the change's commit replays under the new keying
// to the head.
func TestWatch_ReplayAcrossKeyingChange(t *testing.T) {
	store := openStore(t)
	setSchema(t, store, keyedByID, false)
	narrowWrite(t, store, "", `{runs: [{id: r1, sku: A, n: 1}, {id: r2, sku: B, n: 2}]}`)
	from, err := store.GetCurrentCommit()
	if err != nil {
		t.Fatal(err)
	}
	narrowWrite(t, store, `runs."(id=r1)"`, `{n: 5}`)
	c := setSchema(t, store, keyedBySKU, false)
	narrowWrite(t, store, `runs."(sku=A)"`, `{n: 10}`)

	ls := newLiveSession(t, store)
	ls.send(fmt.Sprintf(`{id: "array", watch: {path: runs, fromCommit: %d}}`, from))
	ls.send(fmt.Sprintf(`{id: "renamed", watch: {path: "runs(A)", fromCommit: %d}}`, from))
	ls.send(fmt.Sprintf(`{id: "resumed", watch: {path: "runs(A)", fromCommit: %d}}`, c))
	ls.send(`{id: "head", match: {path: "runs(A)"}}`)
	got := ls.until("the replays", func(m map[string][]*api.SessionResponse) bool {
		replayed := false
		for _, r := range m["resumed"] {
			replayed = replayed || (r.Event != nil && r.Event.ReplayComplete)
		}
		return ended(m["array"]) != nil && ended(m["renamed"]) != nil && replayed && len(m["head"]) > 0
	})

	// The array: its state before the change, the write before the change, then the end.
	var kinds []string
	for _, r := range got["array"] {
		switch ev := r.Event; {
		case ev == nil:
		case ev.Ended:
			kinds = append(kinds, fmt.Sprintf("ended %s @%d", ev.EndReason, ev.Commit))
		case ev.ReplayComplete:
			kinds = append(kinds, "replayComplete")
		case ev.State != nil:
			kinds = append(kinds, fmt.Sprintf("state @%d", ev.Commit))
		default:
			kinds = append(kinds, fmt.Sprintf("patch @%d", ev.Commit))
		}
	}
	want := []string{fmt.Sprintf("state @%d", from), fmt.Sprintf("patch @%d", from+1), fmt.Sprintf("ended %s @%d", api.ErrCodeKeyingChanged, c)}
	if !equalStrings(kinds, want) {
		t.Errorf("runs replayed across the change: %v, want %v", kinds, want)
	}

	// runs(A) names nothing before sku was the key: nothing to say but the ending.
	var events []*api.WatchEvent
	for _, r := range got["renamed"] {
		if r.Event != nil {
			events = append(events, r.Event)
		}
	}
	if len(events) != 1 || !events[0].Ended || events[0].EndReason != api.ErrCodeKeyingChanged || events[0].Commit != c {
		t.Errorf("runs(A) from before the change answered %+v, want only %s at %d", events, api.ErrCodeKeyingChanged, c)
	}

	// Resumed from the change's commit: under the new keying, to the head.
	if end := ended(got["resumed"]); end != nil {
		t.Fatalf("runs(A) from the change's commit was ended: %s %s", end.EndReason, end.EndMessage)
	}
	var state *ir.Node
	for _, r := range got["resumed"] {
		switch ev := r.Event; {
		case ev == nil || ev.ReplayComplete:
		case ev.State != nil:
			state = ev.State
		default:
			if state, err = applyAt(state, ev.Patch); err != nil {
				t.Fatalf("fold: %v", err)
			}
		}
	}
	head := got["head"][0]
	// Compared as documents: a node parsed off the wire carries presentation tags a folded
	// one does not.
	if head.Result == nil || head.Result.Match == nil || state == nil || encode.MustString(state) != encode.MustString(head.Result.Match.Body) {
		t.Errorf("runs(A) resumed from the change folds to %s, and the head reads %s", encode.MustString(state), encode.MustString(head.Result.Match.Body))
	}
}
