package server

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// respText is a response as the wire carries it, for a failure to show.
func respText(t *testing.T, r *api.SessionResponse) string {
	t.Helper()
	data, err := r.ToTony(api.WireOptions()...)
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
	return string(data)
}

// Scratch over the wire (r482sz5rh12ksj0cvdn0): one operation, `scratch`, holding a
// write or a read. A write is applied before it is answered, so a read pipelined
// behind it on the same session reads it; a scoped session reaches the same scratch,
// since scratch has no scopes; absent is an omitted data, not not_found; and the
// client's mistakes come back as the document's codes.
func TestScratch_OverTheWire(t *testing.T) {
	store := openStore(t)

	ls := newLiveSession(t, store)
	ls.send(`{id: "w1", scratch: {write: {path: "verse.iods.x.out", data: {n: 1, note: "first"}}}}`)
	ls.send(`{id: "w2", scratch: {write: {path: "verse.iods.x.out", data: {n: 2, note: !delete null}}}}`)
	ls.send(`{id: "r1", scratch: {read: {path: "verse.iods.x.out"}}}`)
	ls.send(`{id: "r2", scratch: {read: {path: "verse.iods.nope"}}}`)
	ls.send(`{id: "e1", scratch: {read: {path: "verse.*"}}}`)
	ls.send(`{id: "e2", scratch: {write: {path: "verse.iods.x.out.n[0]", data: 3}}}`)
	ls.send(`{id: "e3", scratch: {read: {path: "verse.iods.x.out.n.deeper"}}}`)
	ls.send(`{id: "e4", scratch: {write: {path: "verse.iods.x.cmd", data: !pipe "echo hi"}}}`)
	ls.send(`{id: "e5", scratch: {write: {path: "verse.iods.x.out"}}}`)
	ls.send(`{id: "e6", scratch: {write: {path: "a", data: 1}, read: {path: "a"}}}`)
	ls.send(`{id: "e7", scratch: {}}`)
	got := ls.until("every answer", func(m map[string][]*api.SessionResponse) bool {
		return len(m["e7"]) > 0
	})

	one := func(id string) *api.SessionResponse {
		t.Helper()
		rs := got[id]
		if len(rs) != 1 {
			t.Fatalf("%s: %d responses, want 1", id, len(rs))
		}
		return rs[0]
	}
	wantErr := func(id, code string) {
		t.Helper()
		r := one(id)
		if r.Error == nil || r.Error.Code != code {
			t.Fatalf("%s: answered %s, want error %s", id, respText(t, r), code)
		}
	}

	// A write echoes its data.
	if w := one("w1"); w.Error != nil || w.Result == nil || w.Result.Scratch == nil || w.Result.Scratch.Write == nil ||
		wireOf(t, w.Result.Scratch.Write.Data) != `{n: 1 note: first}` {
		t.Fatalf("w1 answered %s", respText(t, w))
	}
	// The read behind the writes reads both.
	if r := one("r1"); r.Error != nil || r.Result == nil || r.Result.Scratch == nil || r.Result.Scratch.Read == nil ||
		wireOf(t, r.Result.Scratch.Read.Data) != `{n: 2}` {
		t.Fatalf("r1 answered %s", respText(t, r))
	}
	// Absent is an omitted data, and not an error.
	if r := one("r2"); r.Error != nil || r.Result == nil || r.Result.Scratch == nil || r.Result.Scratch.Read == nil ||
		r.Result.Scratch.Read.Data != nil {
		t.Fatalf("r2 answered %s, want an omitted data", respText(t, r))
	}
	wantErr("e1", api.ErrCodeInvalidPath)    // a set
	wantErr("e2", api.ErrCodeInvalidDiff)    // a dense index into a scalar: the apply refuses it
	wantErr("e3", api.ErrCodePathConflict)   // a field under a scalar
	wantErr("e4", api.ErrCodeInvalidDiff)    // an operation which executes
	wantErr("e5", api.ErrCodeInvalidDiff)    // no data
	wantErr("e6", api.ErrCodeInvalidMessage) // both
	wantErr("e7", api.ErrCodeInvalidMessage) // neither

	// A scoped session reaches the one scratch document: scratch has no scopes.
	scoped := newLiveSession(t, store)
	scoped.send(`{id: "h", hello: {clientId: "scoped", scope: "s1"}}`)
	scoped.send(`{id: "r", scratch: {read: {path: "verse.iods.x.out"}}}`)
	sg := scoped.until("the scoped read", func(m map[string][]*api.SessionResponse) bool { return len(m["r"]) > 0 })
	if r := sg["r"][0]; r.Error != nil || r.Result == nil || r.Result.Scratch == nil || r.Result.Scratch.Read == nil ||
		wireOf(t, r.Result.Scratch.Read.Data) != `{n: 2}` {
		t.Fatalf("scoped read answered %s", respText(t, r))
	}
}
