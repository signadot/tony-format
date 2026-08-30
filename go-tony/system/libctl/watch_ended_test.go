package libctl

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// End to end, against a real logd: when the server ends an established watch, the client
// must find out.
//
// This is the chain from the measured failure — a slice taking sustained writes lost 550
// of 1000 events and never recovered:
//
//  1. logd drops a watch it cannot serve and reports it "loudly".
//  2. "Loudly" was an error response stamped with the watch's id.
//  3. readPump sends anything with no Event to deliverResponse, which resolves ids
//     against the in-flight REQUEST table. A watch's request completed when the watch
//     opened, so nothing matched: "dropping response with no matching request".
//
// The watch then sat open on the client with no error and no events, forever. Asserting
// through the real client is the point — a server-side unit test cannot tell whether the
// message it sends is one the client can route.
func TestWatchEnded_ClientLearnsTheWatchDied(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.Open(dir, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srv := newLogdOn(t, store)

	session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "test-client"})
	defer session.Close()
	ctx := context.Background()

	// Seed history, then compact it all away so a cursor into it is unserviceable.
	for i := range 3 {
		data := ir.FromMap(map[string]*ir.Node{"n": ir.FromInt(int64(i))})
		if _, err := session.Patch(ctx, "users/1", data); err != nil {
			t.Fatalf("Patch %d failed: %v", i, err)
		}
	}
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

	from := int64(1)
	w, err := session.Watch(ctx, "users/1", &WatchOptions{FromCommit: &from})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	defer w.Close()

	// The client must observe the watch ending. Before the fix this blocked until the
	// deadline: the events channel stayed open and empty because the failure had been
	// dropped as an unroutable response.
	select {
	case _, ok := <-w.Events():
		if ok {
			t.Fatal("expected no events on a watch the server refused, got one")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client never learned the watch ended — it is still waiting on a watch the server abandoned")
	}

	var ended *WatchEndedError
	if !errors.As(w.Err(), &ended) {
		t.Fatalf("Err() = %v, want a WatchEndedError", w.Err())
	}
	if ended.Reason != api.ErrCodeReplayCompacted {
		t.Errorf("Reason = %q, want %q", ended.Reason, api.ErrCodeReplayCompacted)
	}
	if ended.Path != "users/1" {
		t.Errorf("Path = %q, want %q", ended.Path, "users/1")
	}

	// And WHAT THE STORE STILL HOLDS, which is the part a reason code cannot carry. A
	// client told only "replay_compacted" knows its cursor is gone and nothing else --
	// it cannot tell a person where history now starts, or pick a cursor that would
	// work. The floor was composed into a message and then written to the server's log
	// alone (zmq8bdhwh12kstkqjhn0).
	if ended.Message == "" {
		t.Fatal("the ending carried no message, so the client cannot say what the store still holds")
	}
	if floor := store.ReplayFloor(); !strings.Contains(ended.Message, strconv.FormatInt(floor+1, 10)) {
		t.Errorf("message %q names no exact-from commit; the floor is %d so it should name %d",
			ended.Message, floor, floor+1)
	}
	// Error() puts it where a person reading a log will see it.
	if !strings.Contains(ended.Error(), ended.Message) {
		t.Errorf("Error() = %q, which drops the message again", ended.Error())
	}
}

// After being told, the client can re-establish and get current state — the recovery the
// terminal event exists to enable. "Never recovered" was the other half of the failure.
func TestWatchEnded_ClientCanReEstablish(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.Open(dir, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srv := newLogdOn(t, store)

	session := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "test-client"})
	defer session.Close()
	ctx := context.Background()

	for i := range 3 {
		data := ir.FromMap(map[string]*ir.Node{"n": ir.FromInt(int64(i))})
		if _, err := session.Patch(ctx, "users/1", data); err != nil {
			t.Fatalf("Patch %d failed: %v", i, err)
		}
	}
	if err := store.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	cfg := storage.DefaultCompactionConfig()
	cfg.Cutoff = -time.Hour
	if err := store.Compact(cfg); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	from := int64(1)
	doomed, err := session.Watch(ctx, "users/1", &WatchOptions{FromCommit: &from})
	if err != nil {
		t.Fatalf("Watch failed: %v", err)
	}
	select {
	case <-doomed.Events():
	case <-time.After(3 * time.Second):
		t.Fatal("client never learned the watch ended")
	}
	doomed.Close()

	// Re-watch without a cursor, as the terminal event's reason instructs.
	w, err := session.Watch(ctx, "users/1", waitAbsent)
	if err != nil {
		t.Fatalf("re-Watch failed: %v", err)
	}
	defer w.Close()

	ev := recvEvent(t, w, 3*time.Second)
	if ev.State == nil {
		t.Fatalf("expected an initial state event after re-establishing, got %+v", ev)
	}
}
