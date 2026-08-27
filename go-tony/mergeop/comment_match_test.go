package mergeop_test

import (
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func mustParseCommented(t *testing.T, s string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(s), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return n
}

// !comment asks of the comments what it states about them: a position the operand
// names is compared exactly, a position it does not name is not asked about, and no
// lines is the absence of a comment.
//
// The format says tools support "matching comments if so desired" and there was no
// way to ask (8241kcggh12krgh4g1n0). The option that was tried and removed --
// comments participating in every comparison -- could not work: matchNode has no
// case which compares them, so two IDENTICAL comments still mismatched. Asking
// explicitly is a question the walk can answer.
func TestCommentMatches(t *testing.T) {
	// `# lead` wraps the whole document; `# above b` wraps a's VALUE, which is
	// where a comment written above a nested key lives
	const doc = "# lead\na: 1 # after\nb:\n  # inner\n  c: 2\n"

	for _, tc := range []struct {
		name, pattern string
		want          bool
	}{
		{"the document's head comment", `!comment {head: ["# lead"]}`, true},
		{"and a different one", `!comment {head: ["# other"]}`, false},
		{"a line comment", `{a: !comment {line: [" # after"]}}`, true},
		{"and a different one", `{a: !comment {line: [" # nope"]}}`, false},
		{"a nested value's head comment", `{b: !comment {head: ["# inner"]}}`, true},
		{"a position not named is not asked", `{a: !comment {line: [" # after"]}}`, true},
		{"no lines asks for no comment", `{b: !comment {line: []}}`, true},
		{"and fails where there is one", `{a: !comment {line: []}}`, false},
		{"no head comment", `{a: !comment {head: []}}`, true},
		{"both positions at once", `!comment {head: ["# lead"], line: []}`, true},
		{"both, one of them wrong", `!comment {head: ["# lead"], line: ["# no"]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tony.Match(mustParseCommented(t, doc), mustParseNode(t, tc.pattern))
			if err != nil {
				t.Fatalf("match: %v", err)
			}
			if got != tc.want {
				t.Errorf("matched=%v, want %v", got, tc.want)
			}
		})
	}
}

// It asks about the comments and NOT about the value, as the patch changes the
// comments and not the value. Asking about both is the composition it looks like --
// and it needs no special arrangement, because a node keeps its parent however it
// was reached, and the head comment IS the parent.
func TestCommentComposes(t *testing.T) {
	const doc = "# lead\na: 1\n"
	for _, tc := range []struct {
		name, pattern string
		want          bool
	}{
		{"comment and value", `!and [!comment {head: ["# lead"]}, {a: 1}]`, true},
		{"comment right, value wrong", `!and [!comment {head: ["# lead"]}, {a: 2}]`, false},
		{"comment wrong, value right", `!and [!comment {head: ["# no"]}, {a: 1}]`, false},
		{"either", `!or [!comment {head: ["# no"]}, !comment {head: ["# lead"]}]`, true},
		{"negated", `!not.comment {head: ["# no"]}`, true},
		{"negated, and it is that one", `!not.comment {head: ["# lead"]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tony.Match(mustParseCommented(t, doc), mustParseNode(t, tc.pattern))
			if err != nil {
				t.Fatalf("match: %v", err)
			}
			if got != tc.want {
				t.Errorf("matched=%v, want %v", got, tc.want)
			}
		})
	}
}

// Everything else stays comment-blind, which is the part that could not be an
// option: a document parsed with comments answers an ordinary pattern the same way
// as one parsed without.
func TestMatchingStaysCommentBlind(t *testing.T) {
	const pattern = `{a: 1, b: 2}`
	for _, doc := range []string{
		"a: 1\nb: 2\n",
		"# lead\na: 1\nb: 2\n",
		"a: 1 # after\nb: 2\n",
		"# lead\na: 1 # after\n# above b\nb: 2\n",
	} {
		with, err := tony.Match(mustParseCommented(t, doc), mustParseNode(t, pattern))
		if err != nil {
			t.Fatalf("%q: %v", doc, err)
		}
		if !with {
			t.Errorf("%q did not match %s", doc, pattern)
		}
	}
}

// A document parsed without comments has none, which is a fact a pattern can ask
// about rather than a reason to error.
func TestCommentMatchesADocumentWithoutComments(t *testing.T) {
	doc := mustParseNode(t, "# lead\na: 1\n") // comments off: dropped at the parse
	for _, tc := range []struct {
		pattern string
		want    bool
	}{
		{`!comment {head: []}`, true},
		{`!comment {head: ["# lead"]}`, false},
	} {
		got, err := tony.Match(doc, mustParseNode(t, tc.pattern))
		if err != nil {
			t.Fatalf("match: %v", err)
		}
		if got != tc.want {
			t.Errorf("%s: matched=%v, want %v", tc.pattern, got, tc.want)
		}
	}
}

// The operand is read the same way for a match as for a patch, so a malformed one
// is refused where it is built rather than answering false.
func TestCommentMatchRefusesABadOperand(t *testing.T) {
	for _, pattern := range []string{
		`!comment 3`,
		`!comment {nope: []}`,
		`!comment {head: 3}`,
		`!comment {head: [3]}`,
	} {
		if _, err := tony.Match(mustParseNode(t, "a: 1"), mustParseNode(t, pattern)); err == nil {
			t.Errorf("%s was accepted", pattern)
		}
	}
}
