package server

import (
	"strconv"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A watched path that is absent, then holds null, then is absent again has changed twice.
// Null is a value and absence is not one, and a watch owes its watcher the difference: a
// gate that is handed null for both sees no change at either step (presence.md).
func TestWatchTellsNullFromAbsent(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()

	narrowWrite(t, store, "verse.y", "{n: 0}") // so the store is not empty when the watch seeds
	before, _ := store.GetCurrentCommit()
	narrowWrite(t, store, "verse.x", "null") // null arrives where there was nothing
	nullAt, _ := store.GetCurrentCommit()
	narrowWrite(t, store, "verse.x", "!delete null") // and leaves again
	goneAt, _ := store.GetCurrentCommit()
	narrowWrite(t, store, "verse.y", "{n: 1}") // a sibling, which is not a change to x

	// The leaf is absent at head, which without waitIfAbsent is a refusal rather than a
	// watch; the question here is about the history, not about whether it is there now.
	events := narrowRequestEvents(t, store,
		`{id: "w", watch: {path: "verse.x", fromCommit: `+strconv.FormatInt(before, 10)+`, waitIfAbsent: true}}`)

	var patched []int64
	for _, ev := range events {
		if ev.Patch != nil {
			patched = append(patched, ev.Commit)
		}
	}
	want := []int64{nullAt, goneAt}
	if len(patched) != len(want) {
		t.Fatalf("patch events at commits %v, want %v (null written at %d, deleted at %d)",
			patched, want, nullAt, goneAt)
	}
	for i := range want {
		if patched[i] != want[i] {
			t.Errorf("event %d at commit %d, want %d", i, patched[i], want[i])
		}
	}
}

// The scoped emitter is handed the two states directly, and a nil on either side is a
// state: arrival is an insert and departure a delete, never a null (presence.md).
func TestScopedDeltaStatesAbsence(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()

	for _, tc := range []struct {
		name       string
		prev, next *ir.Node
		wantEvent  bool
		wantTag    string
	}{
		{"absent stays absent", nil, nil, false, ""},
		{"null arrives", nil, ir.Null(), true, "!insert"},
		{"null leaves", ir.Null(), nil, true, "!delete"},
		{"null stays null", ir.Null(), ir.Null(), false, ""},
		{"a value arrives", nil, ir.FromInt(1), true, "!insert"},
		{"a value leaves", ir.FromInt(1), nil, true, "!delete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := NewSession("test-server", newMockConn(), &SessionConfig{Storage: store, Hub: NewWatchHub()})
			id := "w"
			// send hands the response to the session's writer and waits for it, so the
			// emitter runs beside the test and the test is the writer.
			done := make(chan error, 1)
			go func() {
				_, err := session.emitScopedDeltaFrom(&id, "verse.x", 7, tc.prev, tc.next)
				done <- err
			}()
			var sent *api.SessionResponse
			select {
			case sent = <-session.outgoing:
				if err := <-done; err != nil {
					t.Fatalf("emit: %s", err)
				}
			case err := <-done:
				if err != nil {
					t.Fatalf("emit: %s", err)
				}
			}
			if !tc.wantEvent {
				if sent != nil {
					t.Fatalf("no change, but a response was sent: %+v", sent)
				}
				return
			}
			if sent == nil || sent.Event == nil || sent.Event.Patch == nil {
				t.Fatalf("want one patch event, got %+v", sent)
			}
			leaf, err := sent.Event.Patch.GetKPath("verse.x")
			if err != nil {
				t.Fatalf("the delta is not rooted at the path: %s", err)
			}
			if leaf == nil || leaf.Tag != tc.wantTag {
				t.Errorf("delta at verse.x is %v, want a %s", leaf, tc.wantTag)
			}
		})
	}
}
