package mergeop_test

import (
	"strings"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

func doc(t *testing.T, src string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(src), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return n
}

func show(t *testing.T, n *ir.Node) string {
	t.Helper()
	if n == nil {
		return "<nil>"
	}
	var b strings.Builder
	if err := encode.Encode(n, &b, encode.EncodeComments(true)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b.String()
}

// TestCommentOp: !comment states what the comments at a node are. It exists so
// that a comment change is a delta about the comment: without it the only way to
// say one had changed was to replace the node, which carries the value it
// describes -- the whole subtree, twice.
func TestCommentOp(t *testing.T) {
	for _, tc := range []struct{ name, doc, patch, want string }{
		{"set a head comment", "name: svc\n", `!comment {head: ["# new"]}`, "# new\nname: svc\n"},
		{"replace a head comment", "# old\nname: svc\n", `!comment {head: ["# new"]}`, "# new\nname: svc\n"},
		{"remove a head comment", "# old\nname: svc\n", `!comment {head: []}`, "name: svc\n"},
		{"set a line comment", "a: 1\n", `{a: !comment {line: [" # new"]}}`, "a: 1 # new\n"},
		{"remove a line comment", "a: 1 # old\n", `{a: !comment {line: []}}`, "a: 1\n"},
		{"both at once", "a: 1\n", `{a: !comment {head: ["# h"], line: [" # l"]}}`, "# h\na: 1 # l\n"},
		// The head comment on a's value is not named by the operand and stays;
		// the line comment lands on the key's line, which is where a latch on a
		// block value goes.
		{"a position not named is left alone", "a:\n  # keep\n  b: 1\n", `{a: !comment {line: [" # new"]}}`, "a: # new\n  # keep\n  b: 1\n"},
		{"the value is untouched", "a:\n  b: 1\n", `!comment {head: ["# c"]}`, "# c\na:\n  b: 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tony.Patch(doc(t, tc.doc), doc(t, tc.patch), mergeop.Comments(true))
			if err != nil {
				t.Fatalf("patch: %v", err)
			}
			if show(t, got) != tc.want {
				t.Errorf("got %q, want %q", show(t, got), tc.want)
			}
		})
	}
}

// TestCommentOpIsAbsolute: it says what IS, never what was, so applying it twice
// is applying it once and it does not consult what it finds. That is what lets a
// store keep it -- see logd's storage vocabulary.
func TestCommentOpIsAbsolute(t *testing.T) {
	patch := doc(t, `!comment {head: ["# c"]}`)
	once, err := tony.Patch(doc(t, "a: 1\n"), patch, mergeop.Comments(true))
	if err != nil {
		t.Fatal(err)
	}
	twice, err := tony.Patch(once, doc(t, `!comment {head: ["# c"]}`), mergeop.Comments(true))
	if err != nil {
		t.Fatal(err)
	}
	if show(t, once) != show(t, twice) {
		t.Errorf("applying twice differs from once:\n%q\n%q", show(t, once), show(t, twice))
	}
	// and against a document whose comment is something else entirely
	other, err := tony.Patch(doc(t, "# whatever\na: 1\n"), patch, mergeop.Comments(true))
	if err != nil {
		t.Fatalf("against a moved base: %v", err)
	}
	if show(t, other) != show(t, once) {
		t.Errorf("the result depended on what was there: %q against %q", show(t, other), show(t, once))
	}
}

// TestCommentOpRejectsNonsense: the operand names positions, and a position it
// does not know is a mistake rather than a silent no-op.
func TestCommentOpRejectsNonsense(t *testing.T) {
	for _, patch := range []string{
		`!comment {middle: ["# x"]}`,
		`!comment ["# x"]`,
		`!comment(head) ["# x"]`,
		`!comment {head: "# not a list"}`,
		`!comment {head: [1]}`,
	} {
		if _, err := tony.Patch(doc(t, "a: 1\n"), doc(t, patch), mergeop.Comments(true)); err == nil {
			t.Errorf("%s was accepted", patch)
		}
	}
}
