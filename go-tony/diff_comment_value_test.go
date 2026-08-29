package tony

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A node whose COMMENT and whose VALUE both changed is one statement: !comment
// carrying the positions and the value's own difference under value:.
//
// Before, commentDiff declined such a node and MakeDiff stated it WHOLE. For the
// ordinary diff that is right -- !replace carries both sides and installs the new one
// entire. For an absolute diff there is no !replace, and stating an object whole
// restates the whole document: a write of one field came out as every field, and what
// the store replayed was far wider than what was written (xqpvk3ehh12ks89mj5n0).
func TestDiffCommentAndValue(t *testing.T) {
	tests := []struct {
		name, a, b string
		// wantIn is what the delta must contain; wantOut what it must not.
		wantIn, wantOut []string
	}{{
		// The whole point: one field changed, so one field is stated -- not the
		// three others that did not.
		name:    "a comment and one field, out of four",
		a:       "a: 1\nb: 2\nc: 3\n",
		b:       "# note\na: 1\nb: 9\nc: 3\n",
		wantIn:  []string{"!comment", "head", "# note", "value", "b"},
		wantOut: []string{"c: 3"},
	}, {
		name:    "a comment alone still states only the comment",
		a:       "a: 1\n",
		b:       "# note\na: 1\n",
		wantIn:  []string{"!comment", "# note"},
		wantOut: []string{"value"},
	}, {
		name:    "a value alone still states only the value",
		a:       "a: 1\nb: 2\n",
		b:       "a: 9\nb: 2\n",
		wantOut: []string{"!comment"},
		wantIn:  []string{"a"},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a, err := parse.Parse([]byte(test.a), parse.ParseComments(true))
			if err != nil {
				t.Fatalf("a: %s", err)
			}
			b, err := parse.Parse([]byte(test.b), parse.ParseComments(true))
			if err != nil {
				t.Fatalf("b: %s", err)
			}
			d := DiffWith(a.Clone(), b.Clone(), DiffComments(true), DiffAbsolute(true))
			if d == nil {
				t.Fatal("no difference claimed between documents that differ")
			}
			got := strings.Join(strings.Fields(encode.MustString(d)), " ")
			for _, want := range test.wantIn {
				if !strings.Contains(got, want) {
					t.Errorf("the delta is %s, which does not hold %q", got, want)
				}
			}
			for _, avoid := range test.wantOut {
				if strings.Contains(got, avoid) {
					t.Errorf("the delta is %s, which restates %q", got, avoid)
				}
			}
			// And it still arrives at b, comments included.
			res, err := Patch(a.Clone(), d.Clone(), mergeop.Comments(true))
			if err != nil {
				t.Fatalf("patch: %s\n delta: %s", err, got)
			}
			if res == nil || !res.DeepEqualWithComments(b) {
				t.Errorf("the round trip did not arrive at b\n delta: %s", got)
			}
		})
	}
}
