package server

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// The protocol's safety rested on a deployment convention. A request field a server does
// not know is IGNORED, and an unread path is "" -- the whole document for a read, the ROOT
// for a write -- so a client one version ahead is not refused, it is answered wrongly, and
// the wrong answer looks like success. A mismatched pair was indistinguishable from a
// working one until something read the root (k0d4y1m6h12kr7cdgdn0).
func TestHandshakeRefusesAProtocolItDoesNotSpeak(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()

	t.Run("a version from the future is refused", func(t *testing.T) {
		resp := narrowRequest(t, store, `{id: "h", hello: {clientId: ahead, protocol: 99}}`)
		if resp.Error == nil {
			t.Fatalf("a client speaking protocol 99 was accepted: %+v", resp)
		}
		if resp.Error.Code != api.ErrCodeProtocolMismatch {
			t.Errorf("refused with %q, want %q", resp.Error.Code, api.ErrCodeProtocolMismatch)
		}
		if !strings.Contains(resp.Error.Message, "deploy them together") {
			t.Errorf("the refusal does not say what is wrong: %s", resp.Error.Message)
		}
	})

	t.Run("the current version is accepted, and answered with its own", func(t *testing.T) {
		resp := narrowRequest(t, store,
			`{id: "h", hello: {clientId: current, protocol: `+itoa(api.ProtocolVersion)+`}}`)
		if resp.Error != nil {
			t.Fatalf("the current protocol was refused: %s", resp.Error)
		}
		if resp.Result == nil || resp.Result.Hello == nil {
			t.Fatalf("no hello result: %+v", resp)
		}
		if got := resp.Result.Hello.Protocol; got != api.ProtocolVersion {
			t.Errorf("the server reports protocol %d, want %d", got, api.ProtocolVersion)
		}
	})

	t.Run("a client from before the check is accepted", func(t *testing.T) {
		resp := narrowRequest(t, store, `{id: "h", hello: {clientId: old}}`)
		if resp.Error != nil {
			t.Errorf("a client predating the version field was refused: %s", resp.Error)
		}
	})

	// Protocol 3 is the commit's author (api.ProtocolVersion). A client at 2 would be
	// answered, and a client at 3 by a server at 2 would have the one field whose whole
	// purpose is to be kept dropped and its write answered with a commit -- an audit
	// record that looks kept and is not, which is what the handshake exists to refuse.
	t.Run("the version is 3, and 2 is refused", func(t *testing.T) {
		if api.ProtocolVersion != 3 {
			t.Fatalf("ProtocolVersion = %d, want 3", api.ProtocolVersion)
		}
		for _, behind := range []int{1, 2} {
			resp := narrowRequest(t, store, `{id: "h", hello: {clientId: behind, protocol: `+itoa(behind)+`}}`)
			if resp.Error == nil || resp.Error.Code != api.ErrCodeProtocolMismatch {
				t.Fatalf("a client speaking protocol %d was not refused: %+v", behind, resp)
			}
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// A refused hello refuses the session: nothing after it on the connection is answered but
// with the same refusal, until a hello this server speaks. The refusal was sent and then
// the requests behind it served -- a patch committed, a match answered, a watch opened --
// so a client pipelining past its hello had its writes made by a server that had just
// said it would not make them (mygs3chwh12ksyxxmdn0).
func TestARefusedHelloRefusesTheSession(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()

	conn := newMockConn()
	for _, req := range []string{
		`{id: "h", hello: {clientId: ahead, protocol: 99}}`,
		`{id: "p", patch: {path: "a", data: 1}}`,
		`{id: "m", match: {path: ""}}`,
		`{id: "w", watch: {path: "a", waitIfAbsent: true}}`,
		`{id: "h2", hello: {clientId: caught-up, protocol: ` + strconv.Itoa(api.ProtocolVersion) + `}}`,
		`{id: "m2", match: {path: ""}}`,
	} {
		conn.WriteRequest(req)
	}
	session := NewSession("test-server", conn, &SessionConfig{Storage: store, Hub: NewWatchHub()})
	done := make(chan error)
	go func() { done <- session.Run() }()
	time.Sleep(200 * time.Millisecond)
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
		if resp.ID != nil {
			byID[*resp.ID] = &resp
		}
	}
	for _, id := range []string{"h", "p", "m", "w"} {
		resp := byID[id]
		if resp == nil || resp.Error == nil || resp.Error.Code != api.ErrCodeProtocolMismatch {
			t.Errorf("%s: answered %+v, want %s", id, resp, api.ErrCodeProtocolMismatch)
		}
	}
	if head, _ := store.GetCurrentCommit(); head != 0 {
		t.Errorf("the refused session's patch committed: head is %d", head)
	}
	// The store is empty, so the match is answered not_found -- answered, not refused.
	if resp := byID["m2"]; resp == nil || (resp.Error != nil && resp.Error.Code == api.ErrCodeProtocolMismatch) {
		t.Errorf("after a hello the server speaks, the match was answered %+v", resp)
	}
}
