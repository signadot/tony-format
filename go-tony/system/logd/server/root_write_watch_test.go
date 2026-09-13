package server

import (
	"bytes"
	"strconv"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A write that states the whole root -- an operator there, or a value that is not an
// object -- changes every path, and a watcher of a path it removed is owed the delta
// (api/doc.go: every event after the state is the delta of one commit, with no gaps).
// The fan-out named only the fields the stored patch contained, so such a commit reached
// no child watcher live, and its replay found nothing either (05d8w3cjh12kswb1msn0,
// item 8).
func TestRootTotalWriteReachesChildWatchers(t *testing.T) {
	for _, tc := range []struct{ name, patch string }{
		{"insert empty", `!insert {}`},
		{"insert other", `!insert {c: 1}`},
		{"delete", `!delete null`},
		{"scalar", `7`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := storage.Open(t.TempDir(), nil)
			if err != nil {
				t.Fatalf("open: %s", err)
			}
			defer store.Close()
			hub := NewWatchHub()
			store.SetCommitNotifier(hub.Broadcast)

			narrowWrite(t, store, "", "{a: 1, b: 2}")
			before, _ := store.GetCurrentCommit()

			conn := newMockConn()
			conn.WriteRequest(`{id: "w", watch: {path: "a"}}`)
			conn.WriteRequest(`{id: "p", patch: {path: "", data: ` + tc.patch + `}}`)
			session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: hub})
			done := make(chan error)
			go func() { done <- session.Run() }()
			time.Sleep(300 * time.Millisecond)
			conn.Close()
			<-done

			var live []*api.WatchEvent
			for _, line := range bytes.Split(bytes.TrimSpace(conn.GetResponses()), []byte("\n")) {
				if len(bytes.TrimSpace(line)) == 0 {
					continue
				}
				var resp api.SessionResponse
				if err := resp.FromTony(line); err != nil {
					t.Fatalf("parse %q: %s", line, err)
				}
				if resp.Error != nil {
					t.Fatalf("%v: %s", resp.ID, resp.Error)
				}
				if resp.Event != nil && resp.Event.Patch != nil {
					live = append(live, resp.Event)
				}
			}
			after, _ := store.GetCurrentCommit()
			if after != before+1 {
				t.Fatalf("the root write did not commit: head %d, was %d", after, before)
			}
			if len(live) != 1 || live[0].Commit != after || !live[0].Absent {
				t.Errorf("live: %d patch events %+v, want one at commit %d saying a is absent", len(live), live, after)
			}

			var replayed []*api.WatchEvent
			for _, ev := range narrowRequestEvents(t, store,
				`{id: "r", watch: {path: "a", fromCommit: `+strconv.FormatInt(before, 10)+`, waitIfAbsent: true}}`) {
				if ev.Patch != nil {
					replayed = append(replayed, ev)
				}
			}
			if len(replayed) != 1 || replayed[0].Commit != after || !replayed[0].Absent {
				t.Errorf("replay: %d patch events %+v, want one at commit %d saying a is absent", len(replayed), replayed, after)
			}
		})
	}
}
