package server

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A stand-alone patch is recorded as written by the author it names, else the one its
// session's hello named, else no one; and every delta event for the commit says who,
// live and on a replay (cn1n32yph12ks5wrmhn0). A server multiplexing principals onto
// one session names each on the patch; a client with one names it once.
func TestPatchIsRecordedUnderItsAuthor(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()
	hub := NewWatchHub()
	store.SetCommitNotifier(hub.Broadcast)

	conn := newMockConn()
	for _, req := range []string{
		`{id: "h", hello: {clientId: verse, protocol: 3, author: verse}}`,
		`{id: "w", watch: {path: "a", waitIfAbsent: true}}`,
		`{id: "p1", patch: {path: "a", data: {n: 1}}}`,
		`{id: "p2", patch: {path: "a", data: {n: 2}, author: alice}}`,
		`{id: "h2", hello: {clientId: verse, protocol: 3}}`,
		`{id: "p3", patch: {path: "a", data: {n: 3}}}`,
	} {
		conn.WriteRequest(req)
	}
	session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: hub})
	done := make(chan error)
	go func() { done <- session.Run() }()
	time.Sleep(300 * time.Millisecond)
	conn.Close()
	<-done

	var live []string
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
			live = append(live, resp.Event.Author)
			// The one field whose purpose is to be kept is on the wire when it is set,
			// and absent when it is not, rather than an empty string standing in.
			if want := strings.Contains(string(line), "author:"); want != (resp.Event.Author != "") {
				t.Errorf("event %d: author on the wire = %v, in the event = %q", resp.Event.Commit, want, resp.Event.Author)
			}
		}
	}
	want := []string{"verse", "alice", ""}
	if strings.Join(live, ",") != strings.Join(want, ",") {
		t.Errorf("live events say authors %q, want %q (the hello's, the patch's own, and none after a hello with none)", live, want)
	}

	var replayed []string
	for _, ev := range narrowRequestEvents(t, store, `{id: "w", watch: {path: "a", fromCommit: 0, noInit: true}}`) {
		if ev.Patch != nil {
			replayed = append(replayed, ev.Author)
		}
	}
	if strings.Join(replayed, ",") != strings.Join(want, ",") {
		t.Errorf("replayed events say authors %q, want %q", replayed, want)
	}
}

// A transaction's author is fixed by newtx -- the author it names, else the session's
// -- and its participants inherit it: a participant on a session whose hello names
// another author is still the transaction's. A participant naming an author of its own
// is refused, invalid_tx, rather than having the one field that exists to be kept
// quietly dropped; and the transaction stands.
func TestATransactionHasTheAuthorNewtxGaveIt(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()
	store.SetTxTimeout(2 * time.Second)

	// Two sessions: one creates the transaction as alice; the other, whose hello says
	// bob, joins it.
	creator := newMockConn()
	creator.WriteRequest(`{id: "h", hello: {clientId: c, protocol: 3, author: alice}}`)
	creator.WriteRequest(`{id: "t", newtx: {participants: 2}}`)
	creator.WriteRequest(`{id: "p1", patch: {txId: 1, path: "a", data: 1}}`)
	joiner := newMockConn()
	joiner.WriteRequest(`{id: "h", hello: {clientId: j, protocol: 3, author: bob}}`)
	joiner.WriteRequest(`{id: "p2", patch: {txId: 1, path: "b", data: 2, author: bob}}`)
	joiner.WriteRequest(`{id: "p3", patch: {txId: 1, path: "b", data: 2}}`)

	hub := NewWatchHub()
	done := make(chan error, 2)
	// The creator first, so the transaction exists before the joiner's participants
	// name it.
	for _, conn := range []*mockConn{creator, joiner} {
		s := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: hub})
		go func() { done <- s.Run() }()
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	creator.Close()
	joiner.Close()
	<-done
	<-done

	byID := map[string]*api.SessionResponse{}
	for _, conn := range []*mockConn{creator, joiner} {
		for _, line := range bytes.Split(bytes.TrimSpace(conn.GetResponses()), []byte("\n")) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var resp api.SessionResponse
			if err := resp.FromTony(line); err != nil {
				t.Fatalf("parse %q: %s", line, err)
			}
			if resp.ID != nil {
				byID[*resp.ID] = &resp
			}
		}
	}
	if resp := byID["p2"]; resp == nil || resp.Error == nil || resp.Error.Code != api.ErrCodeInvalidTx {
		t.Errorf("a participant naming an author was answered %+v, want %s", resp, api.ErrCodeInvalidTx)
	} else if !strings.Contains(resp.Error.Message, `"bob"`) || !strings.Contains(resp.Error.Message, "newtx") {
		t.Errorf("the refusal does not say what is wrong: %s", resp.Error.Message)
	}
	var commit int64
	for _, id := range []string{"p1", "p3"} {
		resp := byID[id]
		if resp == nil || resp.Error != nil || resp.Result == nil || resp.Result.Patch == nil {
			t.Fatalf("%s: the participant was answered %+v, want a commit", id, resp)
		}
		commit = resp.Result.Patch.Commit
	}

	for _, ev := range narrowRequestEvents(t, store, `{id: "w", watch: {path: "", fromCommit: 0, noInit: true}}`) {
		if ev.Patch != nil && ev.Commit == commit && ev.Author != "alice" {
			t.Errorf("the transaction is recorded as written by %q, want alice (newtx's session), not bob (the joiner's)", ev.Author)
		}
	}
}
