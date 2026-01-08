package eval

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

type envTest struct {
	in, out string
}

func TestEnv(t *testing.T) {
	tests := []envTest{
		{
			in:  "abc",
			out: "abc",
		},
		{
			in:  "$[",
			out: "$[",
		},
		{
			in:  "$[x]",
			out: `X`,
		},
		{
			in:  " $[x]",
			out: ` X`,
		},
		{
			in:  ".[x]",
			out: `X`,
		},
		{
			in:  "$[x",
			out: "$[x",
		},
		{
			in:  "some $[stuff] $[here]",
			out: `some STUFF HERE`,
		},
		{
			in:  "some $[stuff] $[here] trailing",
			out: `some STUFF HERE trailing`,
		},
		{
			in:  "some $[ stuff ] $[here] trailing",
			out: `some STUFF HERE trailing`,
		},
		{
			in:  "$abc",
			out: "$abc",
		},
		{
			in:  " $abc",
			out: " $abc",
		},
	}
	f := EnvToMapAny(map[string]*ir.Node{
		"x":     ir.FromString("X"),
		"stuff": ir.FromString("STUFF"),
		"here":  ir.FromString("HERE"),
		"true":  ir.FromBool(false),
	})
	for i := range tests {
		tc := &tests[i]
		got, err := ExpandString(tc.in, f)
		if err != nil {
			t.Error(err)
			continue
		}
		if got == tc.out {
			continue
		}
		t.Errorf("got %q want %q", got, tc.out)
	}
}

// TestExpandIRCommentTypeEmptyValues tests that ExpandIR handles CommentType nodes
// with empty Values arrays without panicking (issue #105).
func TestExpandIRCommentTypeEmptyValues(t *testing.T) {
	// This YAML has a trailing comment before the closing bracket of the array.
	// When parsed with comments, this creates a CommentType node with empty Values
	// as an element of the array.
	input := `items:
- value1
# trailing comment
`

	node, err := parse.Parse([]byte(input), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// This should not panic
	env := make(map[string]any)
	_, err = ExpandIR(node, env)
	if err != nil {
		t.Fatalf("ExpandIR error: %v", err)
	}
}
