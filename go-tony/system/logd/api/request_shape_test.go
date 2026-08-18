package api

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
)

// path means the same thing in the same place in all three requests. It did not: match
// kept its under `body`, one level down from where patch and watch keep theirs -- so a
// client writing {match: {path: ...}}, which is what the siblings teach, had its path
// silently ignored and was answered from the ROOT (k0d4y1m6h12kr7cdgdn0).
func TestPathSitsInTheSamePlaceInEveryRequest(t *testing.T) {
	data := ir.FromMap(map[string]*ir.Node{"k": ir.FromString("v")})
	commit := int64(7)

	for _, tc := range []struct {
		name string
		req  *SessionRequest
	}{
		{"match", &SessionRequest{Match: &MatchRequest{PathData: PathData{Path: "a.b", Data: data}, Commit: &commit}}},
		{"patch", &SessionRequest{Patch: &PatchRequest{PathData: PathData{Path: "a.b", Data: data}}}},
		{"watch", &SessionRequest{Watch: &WatchRequest{Path: "a.b"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := tc.req.ToTony(WireOptions()...)
			if err != nil {
				t.Fatalf("encode: %s", err)
			}
			wire := string(b)
			if !strings.Contains(wire, "path: a.b") {
				t.Errorf("path is not directly under the operation: %s", wire)
			}
			if strings.Contains(wire, "body:") {
				t.Errorf("a request carries a body; a body is what a RESPONSE has: %s", wire)
			}
		})
	}
}

// The shape a client writes must be the shape the server reads, or the failure is
// silent: an unread path defaults to "", which is the whole document for a read and the
// document ROOT for a write.
func TestMatchRequestRoundTripsTheFlatShape(t *testing.T) {
	src := `{match: {path: "a.b", data: {k: v}, commit: 7}}`
	var req SessionRequest
	if err := req.FromTony([]byte(src)); err != nil {
		t.Fatalf("parse %s: %s", src, err)
	}
	if req.Match == nil {
		t.Fatal("no match request")
	}
	if req.Match.Path != "a.b" {
		t.Errorf("path is %q, want a.b -- the server read something other than what was written", req.Match.Path)
	}
	if req.Match.Data == nil {
		t.Error("data did not arrive")
	}
	if req.Match.Commit == nil || *req.Match.Commit != 7 {
		t.Errorf("commit is %v, want 7", req.Match.Commit)
	}

	out, err := req.ToTony(gomap.EncodeWire(true))
	if err != nil {
		t.Fatalf("encode: %s", err)
	}
	var back SessionRequest
	if err := back.FromTony(out); err != nil {
		t.Fatalf("re-parse %s: %s", out, err)
	}
	if back.Match == nil || back.Match.Path != "a.b" {
		t.Errorf("round trip lost the path: %s", out)
	}
}
