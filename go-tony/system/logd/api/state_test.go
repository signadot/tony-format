package api

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func mustParseCommented(t *testing.T, src string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(src), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return n
}

// TestSameStateCountsComments is the choice itself: a comment-only edit is a
// change, so a store that keeps one and the watchers following it agree that
// something happened (3cdjz00jh12krns4g1n0, section 4).
func TestSameStateCountsComments(t *testing.T) {
	for _, tc := range []struct {
		name, a, b string
		same       bool
	}{
		{"identical", "name: svc\n", "name: svc\n", true},
		{"a value changed", "name: svc\n", "name: other\n", false},
		{"a head comment added", "name: svc\n", "# lead\nname: svc\n", false},
		{"a head comment changed", "# old\nname: svc\n", "# new\nname: svc\n", false},
		{"a head comment removed", "# lead\nname: svc\n", "name: svc\n", false},
		{"a line comment added", "name: svc\n", "name: svc # note\n", false},
		{"a line comment changed", "name: svc # old\n", "name: svc # new\n", false},
		{"a comment deep in the document", "a:\n  b: 1\n", "a:\n  # note\n  b: 1\n", false},
		{"the same comments", "# lead\nname: svc # note\n", "# lead\nname: svc # note\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := mustParseCommented(t, tc.a), mustParseCommented(t, tc.b)
			if got := SameState(a, b); got != tc.same {
				t.Errorf("SameState(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.same)
			}
			if got := SameState(b, a); got != tc.same {
				t.Errorf("the answer is not symmetric: SameState(%q, %q) = %v", tc.b, tc.a, got)
			}
		})
	}
}

// TestSameStateEmptyIsOrdinary: empty state reads back as a nil node, which the
// watch paths compare like any other.
func TestSameStateEmptyIsOrdinary(t *testing.T) {
	if !SameState(nil, nil) {
		t.Error("two empty states differ")
	}
	if SameState(nil, mustParseCommented(t, "a: 1\n")) {
		t.Error("empty state equals a document")
	}
	if SameState(mustParseCommented(t, "a: 1\n"), nil) {
		t.Error("a document equals empty state")
	}
}
