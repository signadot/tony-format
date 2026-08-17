package server

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A path restricts a read to the subdocument, and a pattern is evaluated within it.
// That is what the request has always meant; what changed is that it now costs a
// subtree rather than the document (ap8ddvp2h12krd43gdn0), so these are the answers a
// narrow read has to keep giving.
func TestSessionMatchRestrictsToTheSubdocument(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open storage: %s", err)
	}
	defer store.Close()

	// a broad tree, and a snapshot in the middle so the read has both a base and a
	// delta to narrow
	blob := strings.Repeat("x", 200)
	for i := 0; i < 30; i++ {
		id := "e" + strconv.Itoa(i)
		narrowWrite(t, store, "verse.entities."+id, "{id: "+id+", blob: "+blob+"}")
	}
	if err := store.SwitchDLog(); err != nil {
		t.Fatalf("snapshot: %s", err)
	}
	narrowWrite(t, store, "verse.meta", "{rev: 7, note: hello}")

	for _, test := range []struct {
		name    string
		request string
		want    string // the body, wire-encoded
	}{{
		name:    "a path answers its own subtree",
		request: `{id: "r", match: {body: {path: "verse.meta"}}}`,
		want:    `{note: hello rev: 7}`, // keys come back sorted, which is the store's contract
	}, {
		name:    "a leaf answers its value",
		request: `{id: "r", match: {body: {path: "verse.meta.rev"}}}`,
		want:    `7`,
	}, {
		name:    "an entity out of the tree answers only itself",
		request: `{id: "r", match: {body: {path: "verse.entities.e7.id"}}}`,
		want:    `e7`,
	}, {
		// the pattern is evaluated within the path AND selects: what comes back is
		// what it named, which is a projection of the subdocument
		name:    "a pattern is evaluated within the path, and selects",
		request: `{id: "r", match: {body: {path: "verse.meta", data: {rev: !irtype 1}}}}`,
		want:    `{rev: 7}`,
	}, {
		name:    "a pattern which does not hold answers nothing",
		request: `{id: "r", match: {body: {path: "verse.meta", data: {rev: !irtype ""}}}}`,
		want:    `null`,
	}} {
		t.Run(test.name, func(t *testing.T) {
			resp := narrowRequest(t, store, test.request)
			if resp.Result == nil || resp.Result.Match == nil {
				t.Fatalf("no match result: %+v", resp)
			}
			got := wireOf(t, resp.Result.Match.Body)
			if got != test.want {
				t.Errorf("body is %s, want %s", got, test.want)
			}
		})
	}
}

func narrowWrite(t *testing.T, store *storage.Storage, path, body string) {
	t.Helper()
	tx, err := store.NewTx(1, nil)
	if err != nil {
		t.Fatalf("newtx: %s", err)
	}
	data, err := parse.Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse %q: %s", body, err)
	}
	p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: path, Data: data}})
	if err != nil {
		t.Fatalf("patcher: %s", err)
	}
	if r := p.Commit(); !r.Committed {
		t.Fatalf("commit %s: %v", path, r.Error)
	}
}

// narrowRequest runs one request through a session and answers the response.
func narrowRequest(t *testing.T, store *storage.Storage, request string) api.SessionResponse {
	t.Helper()
	conn := newMockConn()
	conn.WriteRequest(request)
	session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: NewWatchHub()})
	done := make(chan error)
	go func() { done <- session.Run() }()
	time.Sleep(50 * time.Millisecond)
	conn.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("session did not complete")
	}
	var resp api.SessionResponse
	if err := resp.FromTony(bytes.TrimSpace(conn.GetResponses())); err != nil {
		t.Fatalf("parse response %q: %s", conn.GetResponses(), err)
	}
	return resp
}

// wireOf renders a node the way the wire does, so a case reads as the answer it is
// about.
func wireOf(t *testing.T, n *ir.Node) string {
	t.Helper()
	if n == nil {
		return "<nil>"
	}
	var b bytes.Buffer
	if err := encode.Encode(n, &b, encode.EncodeWire(true)); err != nil {
		t.Fatalf("encode: %s", err)
	}
	return strings.TrimSpace(b.String())
}
