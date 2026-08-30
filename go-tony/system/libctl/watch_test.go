package libctl

import (
	"context"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// recvEvent receives one event from a watch, failing the test on timeout or
// unexpected close.
func recvEvent(t *testing.T, w *Watch, d time.Duration) *api.WatchEvent {
	t.Helper()
	select {
	case ev, ok := <-w.Events():
		if !ok {
			t.Fatalf("watch on %q closed unexpectedly: %v", w.Path(), w.Err())
		}
		return ev
	case <-time.After(d):
		t.Fatalf("timed out waiting for event on %q", w.Path())
		return nil
	}
}

func TestLogdSession_WatchInitialState(t *testing.T) {
	srv := startLogd(t)
	session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "test-client"})
	defer session.Close()

	ctx := context.Background()

	// Seed some state.
	data := ir.FromMap(map[string]*ir.Node{"name": ir.FromString("Alice")})
	if _, err := session.Patch(ctx, "users/1", data); err != nil {
		t.Fatalf("Patch failed: %v", err)
	}

	// Watch should deliver the current state as the first event.
	w, err := session.Watch(ctx, "users/1", nil)
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer w.Close()

	ev := recvEvent(t, w, 2*time.Second)
	if ev.State == nil {
		t.Fatalf("expected initial state event, got %+v", ev)
	}
	nameNode, err := ev.State.GetPath("$.name")
	if err != nil {
		t.Fatalf("GetPath failed: %v", err)
	}
	if nameNode == nil || nameNode.String != "Alice" {
		t.Errorf("expected initial state name='Alice', got %v", nameNode)
	}
}

func TestLogdSession_WatchLiveEvents(t *testing.T) {
	srv := startLogd(t)
	session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "test-client"})
	defer session.Close()

	ctx := context.Background()

	// Skip the initial state; we only want live deltas.
	w, err := session.Watch(ctx, "users/1", &WatchOptions{NoInit: true})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer w.Close()

	// A patch after the watch is established should stream as a delta.
	data := ir.FromMap(map[string]*ir.Node{"name": ir.FromString("Bob")})
	if _, err := session.Patch(ctx, "users/1", data); err != nil {
		t.Fatalf("Patch failed: %v", err)
	}

	ev := recvEvent(t, w, 2*time.Second)
	if ev.Patch == nil {
		t.Fatalf("expected delta patch event, got %+v", ev)
	}
	if ev.Path != "users/1" {
		t.Errorf("expected event path 'users/1', got %q", ev.Path)
	}
	if ev.Commit == 0 {
		t.Errorf("expected non-zero commit, got %d", ev.Commit)
	}
}

// TestLogdSession_PatchReportsCommit proves a writer can name what it just did:
// the commit Patch returns is the commit the watch reports for that same write,
// so a client no longer has to guess its own write out of a watch stream. It also
// checks that the data the server stored comes back — the channel auto-generated
// ids ride on.
func TestLogdSession_PatchReportsCommit(t *testing.T) {
	srv := startLogd(t)
	session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "test-client"})
	defer session.Close()

	ctx := context.Background()

	w, err := session.Watch(ctx, "users/1", &WatchOptions{NoInit: true})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer w.Close()

	// Three writes, so a stale or off-by-one commit cannot pass by coincidence.
	for i, name := range []string{"Alice", "Bob", "Carol"} {
		data := ir.FromMap(map[string]*ir.Node{"name": ir.FromString(name)})
		res, err := session.Patch(ctx, "users/1", data)
		if err != nil {
			t.Fatalf("Patch %d failed: %v", i, err)
		}
		if res == nil {
			t.Fatalf("Patch %d returned no result", i)
		}

		ev := recvEvent(t, w, 2*time.Second)
		if res.Commit != ev.Commit {
			t.Errorf("write %d: Patch reported commit %d, watch reported %d", i, res.Commit, ev.Commit)
		}

		// The stored data comes back, not just the commit.
		if res.Data == nil {
			t.Fatalf("write %d: Patch returned no data", i)
		}
		got, err := res.Data.GetPath("$.name")
		if err != nil {
			t.Fatalf("write %d: GetPath failed: %v", i, err)
		}
		if got == nil || got.String != name {
			t.Errorf("write %d: expected returned data name=%q, got %v", i, name, got)
		}
	}
}

// TestLogdSession_WatchConcurrent verifies that two watches multiplexed over a
// single session connection each receive their own events, routed by path.
func TestLogdSession_WatchConcurrent(t *testing.T) {
	srv := startLogd(t)
	session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "test-client"})
	defer session.Close()

	ctx := context.Background()

	wa, err := session.Watch(ctx, "a/1", &WatchOptions{NoInit: true})
	if err != nil {
		t.Fatalf("Watch a failed: %v", err)
	}
	defer wa.Close()

	wb, err := session.Watch(ctx, "b/1", &WatchOptions{NoInit: true})
	if err != nil {
		t.Fatalf("Watch b failed: %v", err)
	}
	defer wb.Close()

	// Patch both paths on the same connection.
	if _, err := session.Patch(ctx, "a/1", ir.FromMap(map[string]*ir.Node{"v": ir.FromString("A")})); err != nil {
		t.Fatalf("Patch a failed: %v", err)
	}
	if _, err := session.Patch(ctx, "b/1", ir.FromMap(map[string]*ir.Node{"v": ir.FromString("B")})); err != nil {
		t.Fatalf("Patch b failed: %v", err)
	}

	// Each watch must see an event tagged with its own path.
	evA := recvEvent(t, wa, 2*time.Second)
	if evA.Path != "a/1" {
		t.Errorf("watch a received event for %q, want a/1", evA.Path)
	}
	evB := recvEvent(t, wb, 2*time.Second)
	if evB.Path != "b/1" {
		t.Errorf("watch b received event for %q, want b/1", evB.Path)
	}
}

func TestLogdSession_WatchClose(t *testing.T) {
	srv := startLogd(t)
	session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "test-client"})
	defer session.Close()

	ctx := context.Background()

	w, err := session.Watch(ctx, "users/1", &WatchOptions{NoInit: true})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// After Close, the events channel drains and closes with no error.
	drained := make(chan struct{})
	go func() {
		for range w.Events() {
		}
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("events channel did not close after Close")
	}
	if err := w.Err(); err != nil {
		t.Errorf("expected nil Err after clean Close, got %v", err)
	}

	// Requests on the session still work after a watch is closed.
	if _, err := session.Ping(ctx); err != nil {
		t.Errorf("Ping after watch Close failed: %v", err)
	}

	// Re-watching the same path should now succeed.
	w2, err := session.Watch(ctx, "users/1", &WatchOptions{NoInit: true})
	if err != nil {
		t.Fatalf("re-Watch after Close failed: %v", err)
	}
	w2.Close()
}
