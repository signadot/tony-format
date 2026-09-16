package server

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

func wire(n *ir.Node) string {
	if n == nil {
		return "<nil>"
	}
	var b bytes.Buffer
	if err := encode.Encode(n, &b, encode.EncodeWire(true)); err != nil {
		return "<" + err.Error() + ">"
	}
	return b.String()
}

func TestZZWatchAcrossSchema(t *testing.T) {
	const (
		keyedID  = `{define: {runs: {id: !logd-key null}}}`
		keyedSKU = `{define: {runs: {sku: !logd-key null}}}`
		unkeyed  = `{define: {runs: {id: null}}}`
	)
	for _, tc := range []struct {
		name, from, to string
		force          bool
		watches        map[string]string // id -> path
		after          string            // a write after the change, head spelling
		reads          map[string]string // id -> path read at head at the end
	}{
		{"control", keyedID, keyedID, false,
			map[string]string{"wa": "runs", "we": "runs(r1)", "wn": "runs(r1).n"},
			`runs(r1)`, map[string]string{"ra": "runs", "re": "runs(r1)", "rn": "runs(r1).n"}},
		{"lose", keyedID, unkeyed, true,
			map[string]string{"wa": "runs", "we": "runs(r1)", "wn": "runs(r1).n"},
			`runs[0]`, map[string]string{"ra": "runs", "re": "runs[0]", "rn": "runs[0].n"}},
		{"gain", unkeyed, keyedID, false,
			map[string]string{"wa": "runs", "we": "runs[0]", "wn": "runs[0].n"},
			`runs(r1)`, map[string]string{"ra": "runs", "re": "runs(r1)", "rn": "runs(r1).n"}},
		{"re-key", keyedID, keyedSKU, false,
			map[string]string{"wa": "runs", "we": "runs(r1)", "wn": "runs(r1).n"},
			`runs(A)`, map[string]string{"ra": "runs", "re": "runs(A)", "rn": "runs(A).n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := openStore(t)
			sch, _ := parse.Parse([]byte(tc.from))
			if _, err := store.SetSchema(sch, false); err != nil {
				t.Fatal(err)
			}
			narrowWrite(t, store, "", `{runs: [{id: r1, sku: A, n: 1}, {id: r2, sku: B, n: 2}]}`)

			before := liveStreams()
			conn := newMockConn()
			hub := NewWatchHub()
			store.SetCommitNotifier(hub.Broadcast)
			session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: hub})
			done := make(chan error)
			go func() { done <- session.Run() }()
			for _, id := range []string{"wa", "we", "wn"} {
				conn.WriteRequest(fmt.Sprintf(`{id: %q, watch: {path: %q}}`, id, tc.watches[id]))
			}
			waitFor(t, func() bool { return liveStreams() == before+3 }, "watches")
			conn.WriteRequest(fmt.Sprintf(`{id: "s", schema: {set: {schema: %s, force: %v}}}`, tc.to, tc.force))
			time.Sleep(300 * time.Millisecond)
			conn.WriteRequest(fmt.Sprintf(`{id: "p", patch: {path: %q, data: {n: 10}}}`, tc.after))
			time.Sleep(300 * time.Millisecond)
			for _, id := range []string{"ra", "re", "rn"} {
				conn.WriteRequest(fmt.Sprintf(`{id: %q, match: {path: %q}}`, id, tc.reads[id]))
			}
			time.Sleep(300 * time.Millisecond)
			// Replay across the change: the array, the element as the head spells it, and the
			// element as the commit before the change spelled it.
			conn.WriteRequest(`{id: "xa", watch: {path: "runs", fromCommit: 2}}`)
			conn.WriteRequest(fmt.Sprintf(`{id: "xe", watch: {path: %q, fromCommit: 2}}`, tc.reads["re"]))
			conn.WriteRequest(fmt.Sprintf(`{id: "xo", watch: {path: %q, fromCommit: 2}}`, tc.watches["we"]))
			time.Sleep(400 * time.Millisecond)
			conn.Close()
			<-done

			folded := map[string]*ir.Node{}
			absent := map[string]bool{}
			for _, line := range bytes.Split(bytes.TrimSpace(conn.GetResponses()), []byte("\n")) {
				var resp api.SessionResponse
				if err := resp.FromTony(bytes.TrimSpace(line)); err != nil {
					t.Fatalf("parse %s: %v", line, err)
				}
				id := "<none>"
				if resp.ID != nil {
					id = *resp.ID
				}
				switch {
				case resp.Error != nil:
					t.Logf("%s ERROR %s: %s", id, resp.Error.Code, resp.Error.Message)
				case resp.Event != nil:
					ev := resp.Event
					switch {
					case ev.Ended:
						t.Logf("%s ENDED %s %s", id, ev.EndReason, ev.EndMessage)
					case ev.ReplayComplete:
					case ev.State != nil || (ev.Patch == nil && !ev.Ended):
						t.Logf("%s state c%d path=%s absent=%v %s", id, ev.Commit, ev.Path, ev.Absent, wire(ev.State))
						folded[id], absent[id] = ev.State, ev.Absent
					default:
						next, err := applyAt(folded[id], ev.Patch)
						t.Logf("%s patch c%d path=%s absent=%v %s -> %s (err %v)", id, ev.Commit, ev.Path, ev.Absent, wire(ev.Patch), wire(next), err)
						folded[id], absent[id] = next, ev.Absent
					}
				case resp.Result != nil && resp.Result.Match != nil:
					t.Logf("%s read %s", id, wire(resp.Result.Match.Body))
				case resp.Result != nil && resp.Result.Schema != nil:
					t.Logf("%s schema set at c%d", id, resp.Result.Schema.Commit)
				case resp.Result != nil && resp.Result.Patch != nil:
					t.Logf("%s patch committed c%d", id, resp.Result.Patch.Commit)
				case resp.Result != nil:
				}
			}
			for _, id := range []string{"xa", "xe", "xo"} {
				t.Logf("FOLDED %s: absent=%v %s", id, absent[id], wire(folded[id]))
			}
			for _, id := range []string{"wa", "we", "wn"} {
				t.Logf("FOLDED %s (%s): absent=%v %s", id, tc.watches[id], absent[id], wire(folded[id]))
			}
		})
	}
}
