package tx

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// TestValueAtPath_QuotedSegments guards the CAS-precondition path against the
// quoted-segment bug: a digit-first decision id (e.g. "9digitdid") is stored
// unquoted but SplitAll yields it quoted. Comparing verbatim makes the precondition
// read null, fail the CAS, and revoke the commit — the residual §8 monitor spin.
func TestValueAtPath_QuotedSegments(t *testing.T) {
	doc := mustParseTx(t, `{vote: {"9digitdid": {alice: "yes"}, letterdid: {bob: "no"}}}`)

	cases := []struct {
		name    string
		path    string
		wantNil bool   // the resolved value should be null (absent)
		wantHas string // otherwise the resolved subtree must contain this field
	}{
		{name: "digitFirst quoted", path: `vote."9digitdid"`, wantHas: "alice"},
		{name: "letterFirst bare", path: `vote.letterdid`, wantHas: "bob"},
		{name: "absent digit sibling", path: `vote."8absentdid"`, wantNil: true},
		{name: "top level", path: `vote`, wantHas: "9digitdid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := valueAtPath(doc, tc.path)
			if tc.wantNil {
				if got == nil || got.Type != ir.NullType {
					t.Fatalf("valueAtPath(%q) = %v, want null", tc.path, got)
				}
				return
			}
			if got == nil || !hasField(got, tc.wantHas) {
				t.Fatalf("valueAtPath(%q) did not resolve to the subtree containing %q", tc.path, tc.wantHas)
			}
		})
	}
}

func hasField(n *ir.Node, name string) bool {
	for _, f := range n.Fields {
		if f.String == name {
			return true
		}
	}
	return false
}

func mustParseTx(t *testing.T, s string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(s))
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return n
}
