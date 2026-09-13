package eval

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A container rebuilt by expansion keeps its line comment as it keeps its tag:
// `a: # why` over a block lost its why through every expansion
// (p478tacqh12krg32msn0 item 19).
func TestExpansionKeepsAContainersLineComment(t *testing.T) {
	src := "a: # why\n  x: $[v]\nl: # list\n- $[v]\n"
	node, err := parse.Parse([]byte(src), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	out, err := ExpandIR(node, map[string]any{"v": "V"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, want string }{{"a", " # why"}, {"l", " # list"}} {
		c := ir.Uncomment(ir.Get(out, tc.key))
		if c.Comment == nil || len(c.Comment.Lines) != 1 || c.Comment.Lines[0] != tc.want {
			var lines []string
			if c.Comment != nil {
				lines = c.Comment.Lines
			}
			t.Errorf("%s: line comment %q, want [%q]", tc.key, lines, tc.want)
		}
	}
	if x := ir.Get(ir.Get(out, "a"), "x"); x == nil || x.String != "V" {
		t.Errorf("a.x = %v, want V expanded", x)
	}
}

// A non-scalar interpolated into a string is its JSON, as build-eval.md says
// (String Expansion); it was written as tony text (p478tacqh12krg32msn0 item 19).
func TestNonScalarInterpolationIsJSON(t *testing.T) {
	env := map[string]any{
		"m": map[string]any{"b": 1.0, "a": "x"},
		"l": []any{1.0, "two", true},
		"n": map[string]*ir.Node{"k": ir.FromInt(1)},
	}
	for _, tc := range []struct{ in, want string }{
		{"m=$[m]", `m={"a":"x","b":1}`},
		{"l=$[l]", `l=[1,"two",true]`},
		{"s=$[m.a]", "s=x"},
		{"n=$[n.k]", "n=1"},
	} {
		got, err := ExpandString(tc.in, env)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.in, got, tc.want)
		}
	}
}
