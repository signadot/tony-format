package server

import (
	"bytes"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

func runRequests(t *testing.T, store *storage.Storage, hub *WatchHub, reqs ...string) map[string]*api.SessionResponse {
	t.Helper()
	conn := newMockConn()
	for _, r := range reqs {
		conn.WriteRequest(r)
	}
	session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: hub})
	done := make(chan error)
	go func() { done <- session.Run() }()
	time.Sleep(300 * time.Millisecond)
	conn.Close()
	<-done
	byID := map[string]*api.SessionResponse{}
	for _, line := range bytes.Split(bytes.TrimSpace(conn.GetResponses()), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var resp api.SessionResponse
		if err := resp.FromTony(line); err != nil {
			t.Fatalf("parse %q: %s", line, err)
		}
		if resp.ID != nil && resp.Event == nil {
			byID[*resp.ID] = &resp
		}
	}
	return byID
}

// A session says hello once. A second hello is refused, and the session goes on
// answering for the scope and author the first one fixed: a second hello that
// dropped the scope left a scoped watch dereferencing nil at the next commit
// (4ynqp7wqh12krg32msn0 item 2).
func TestASessionSaysHelloOnce(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()
	hub := NewWatchHub()
	store.SetCommitNotifier(hub.Broadcast)
	narrowWrite(t, store, "", "{a: 1}")

	byID := runRequests(t, store, hub,
		`{id: "h", hello: {clientId: c, protocol: 3, scope: s1}}`,
		`{id: "w", watch: {path: "a"}}`,
		`{id: "h2", hello: {clientId: c, protocol: 3}}`,
		`{id: "p", patch: {path: "a", data: 2}}`, // a baseline commit under the scoped watch
		`{id: "m", match: {path: "a"}}`,
	)
	if byID["h"] == nil || byID["h"].Error != nil {
		t.Fatalf("first hello: %v", byID["h"])
	}
	if r := byID["h2"]; r == nil || r.Error == nil || r.Error.Code != api.ErrCodeHelloRepeated {
		t.Fatalf("second hello answered %v, want %s", r, api.ErrCodeHelloRepeated)
	}
	for _, id := range []string{"w", "p", "m"} {
		if r := byID[id]; r == nil || r.Error != nil {
			t.Errorf("%s after the refused hello: %v, want it served under the first hello's scope", id, r)
		}
	}
}

// A newtx's participants is how many the transaction waits for, not a size to
// allocate: a count the client made up panicked the request loop (makeslice) or
// reserved gigabytes per request (4ynqp7wqh12krg32msn0 item 1). The loop answers,
// and goes on answering.
func TestNewTxWithAnAbsurdParticipantCountDoesNotKillTheLoop(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()
	byID := runRequests(t, store, NewWatchHub(),
		`{id: "t", newtx: {participants: 1125899906842624, timeout: "1s"}}`,
		`{id: "t2", newtx: {participants: 1000000000, timeout: "1s"}}`,
		`{id: "ping", ping: {}}`,
	)
	for _, id := range []string{"t", "t2", "ping"} {
		if r := byID[id]; r == nil || r.Error != nil {
			t.Errorf("%s: %v, want an answer", id, r)
		}
	}
}
