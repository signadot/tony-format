package server

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

func liveStreams() int {
	buf := make([]byte, 1<<22)
	n := runtime.Stack(buf, true)
	return strings.Count(string(buf[:n]), "(*watchStream).live(")
}

// An unwatch ends the stream that served the watch. The hub stopped sending to it,
// but nothing ended the goroutine, which sat on the watcher's Events until the
// session closed: one leaked per unwatch, for the life of the session
// (4jhyq24nh12kszvjmsn0).
func TestUnwatchEndsTheStream(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()
	narrowWrite(t, store, "", "{a: 1}")

	before := liveStreams()
	conn := newMockConn()
	session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: NewWatchHub()})
	done := make(chan error)
	go func() { done <- session.Run() }()

	conn.WriteRequest(`{id: "w", watch: {path: "a"}}`)
	waitFor(t, func() bool { return liveStreams() == before+1 }, "the watch's stream to start")
	conn.WriteRequest(`{id: "u", unwatch: {path: "a", watchId: "w"}}`)
	waitFor(t, func() bool { return liveStreams() == before }, "the unwatch to end the stream")

	conn.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("session did not complete")
	}
}

// A watch open waits for a read slot off the request loop, so an unwatch can reach the
// loop and end the watch before it is established. The watch request is still answered,
// once, and the answer is that it is not watching: no confirmation, no stream
// (x32pp5j0h12krtnvwnn0). The loop answers a ping meanwhile.
func TestUnwatchWhileTheOpenWaitsForASlot(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()
	narrowWrite(t, store, "", "{a: 1}")

	before := liveStreams()
	hub := NewWatchHub()
	conn := newMockConn()
	session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: hub})
	for range maxConcurrentReads {
		session.readSlots <- struct{}{} // every slot taken, as by reads in flight
	}
	ls := &liveSession{t: t, conn: conn, hub: hub, done: make(chan error, 1)}
	go func() { ls.done <- session.Run() }()
	t.Cleanup(ls.close)

	ls.send(`{id: "w", watch: {path: "a"}}`)
	ls.send(`{id: "p", ping: {}}`)
	ls.send(`{id: "u", unwatch: {path: "a", watchId: "w"}}`)
	got := ls.until("the pong and the unwatch", func(m map[string][]*api.SessionResponse) bool {
		return len(m["p"]) > 0 && len(m["u"]) > 0
	})
	if r := got["u"][0]; r.Error != nil {
		t.Fatalf("unwatch answered %+v, want it done", r)
	}
	if r := got["w"]; len(r) != 0 {
		t.Fatalf("the watch was answered %+v while it waited for a slot", r[0])
	}

	for range maxConcurrentReads {
		<-session.readSlots
	}
	got = ls.until("the watch's answer", func(m map[string][]*api.SessionResponse) bool { return len(m["w"]) > 0 })
	if e := got["w"][0].Error; e == nil || e.Code != api.ErrCodeNotWatching {
		t.Errorf("the watch was answered %+v, want not_watching", got["w"][0])
	}
	time.Sleep(50 * time.Millisecond)
	if r := ls.byID()["w"]; len(r) != 1 {
		t.Errorf("the watch was answered %d times, want once", len(r))
	}
	if n := liveStreams(); n != before {
		t.Errorf("%d streams running, want %d", n, before)
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("waited 2s for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
