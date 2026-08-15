package tony_test

import (
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func withComments(t *testing.T, src string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(src), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return n
}

// TestMatchIsCommentBlindByDefault: a comment describes a value and is not what
// the value IS, so a document parsed with comments must answer a pattern the
// same way as one without. It did not: a head comment wraps its value in a
// CommentType node, and the match saw the wrapper, so {name: svc} did not match
// "# lead\nname: svc" -- false, with no error, which is how a rule silently
// stops applying to documents somebody commented.
func TestMatchIsCommentBlindByDefault(t *testing.T) {
	for _, tc := range []struct {
		name, doc, pattern string
		want               bool
	}{
		{"a head comment on the document", "# lead\nname: svc\n", "{name: svc}", true},
		{"a line comment", "name: svc # latch\n", "{name: svc}", true},
		{"a head comment inside", "a:\n # about\n  b: 1\n", "{a: {b: 1}}", true},
		{"comments on the pattern instead", "name: svc\n", "# lead\n{name: svc} # latch", true},
		{"comments on both", "# a\nname: svc # b\n", "# c\n{name: svc} # d", true},
		{"a real mismatch still fails", "# lead\nname: svc\n", "{name: other}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tony.Match(withComments(t, tc.doc), withComments(t, tc.pattern))
			if err != nil {
				t.Fatalf("match: %v", err)
			}
			if got != tc.want {
				t.Fatalf("match = %v, want %v", got, tc.want)
			}
		})
	}
}
