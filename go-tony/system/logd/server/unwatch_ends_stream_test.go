package server

import (
	"runtime"
	"strings"
	"testing"
	"time"

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
