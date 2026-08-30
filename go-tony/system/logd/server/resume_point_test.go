package server

import (
	"bytes"
	"strconv"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

func mustParseNode(t *testing.T, src string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %s", src, err)
	}
	return n
}

// decodeResponses reads every response written to a mock conn.
func decodeResponses(t *testing.T, raw []byte) []*api.SessionResponse {
	t.Helper()
	var out []*api.SessionResponse
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var resp api.SessionResponse
		if err := resp.FromTony(line); err != nil {
			continue // a partial line while the writer is mid-flight
		}
		out = append(out, &resp)
	}
	return out
}

// When a watch ends, the terminal event carries the last commit it accounted for, so the
// client re-watches from there instead of replaying history it already has. A SCOPED watch
// never advanced it while streaming live -- only its initial read and its replay did -- so
// a watch dropped after an hour handed back the commit it started at, and the re-watch
// either replayed the hour or was refused as replay_compacted.
func TestScopedWatchAdvancesItsResumePoint(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()
	for i := 0; i < 5; i++ {
		narrowWrite(t, store, "verse.entities.e"+strconv.Itoa(i), "{n: "+strconv.Itoa(i)+"}")
	}

	scope := "sandbox"
	conn := newMockConn()
	session := NewSession("test", conn, &SessionConfig{Storage: store, Hub: NewWatchHub()})
	session.scope.Store(&scope)

	watcher := NewWatcher("verse.entities", &scope, nil, 16)
	id := "w1"
	watcher.ID = &id

	// The writer is what puts responses on the connection; the reader is not needed,
	// since this drives the watch directly.
	go session.writer()
	defer session.Close()

	head, _ := store.GetCurrentCommit()
	go session.forwardEvents(watcher, nil, true /*noInit*/, head)

	// Live commits reach the watch.
	for i := 5; i < 9; i++ {
		narrowWrite(t, store, "verse.entities.e"+strconv.Itoa(i), "{n: "+strconv.Itoa(i)+"}")
		commit, _ := store.GetCurrentCommit()
		watcher.Events <- &storage.CommitNotification{
			Commit: commit,
			KPaths: []string{"verse"},
			Patch:  mustParseNode(t, `{verse: {entities: {e`+strconv.Itoa(i)+`: {n: `+strconv.Itoa(i)+`}}}}`),
		}
	}
	time.Sleep(300 * time.Millisecond)
	last, _ := store.GetCurrentCommit()

	// Drop it the way a slow consumer is dropped, and read the terminal event.
	watcher.failOnce.Do(func() { close(watcher.Failed) })

	deadline := time.Now().Add(2 * time.Second)
	var ended *api.WatchEvent
	for time.Now().Before(deadline) && ended == nil {
		for _, resp := range decodeResponses(t, conn.GetResponses()) {
			if resp.Event != nil && resp.Event.Ended {
				ended = resp.Event
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if ended == nil {
		t.Fatal("no terminal event after the watch was dropped")
	}
	if ended.Commit != last {
		t.Errorf("the watch resumes from commit %d after streaming through %d: it did not account for what it delivered",
			ended.Commit, last)
	}
}
