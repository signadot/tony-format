package tony

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// Diff leaves its inputs as it found them. It put the input nodes themselves into
// the diff, so after Diff(a, b) a.x's path was "a.from", a keyed element's was under
// the diff, and for a sparse/object mismatch the diff WAS b's value
// (4ynqp7wqh12krg32msn0 item 14).
func TestDiffDoesNotReparentItsInputs(t *testing.T) {
	check := func(name string, from, to string) {
		t.Helper()
		a, b := parseT(t, from), parseT(t, to)
		before := encodeT(t, a) + encodeT(t, b)
		d := Diff(a, b)
		for _, n := range []*ir.Node{a, b} {
			var walk func(*ir.Node)
			walk = func(n *ir.Node) {
				for i, v := range n.Values {
					if v.Parent != n || v.ParentIndex != i {
						t.Errorf("%s: %s is parented under %v after Diff", name, encodeT(t, v), v.Parent)
					}
					walk(v)
				}
			}
			walk(n)
		}
		if d != nil && (d == a || d == b || d == ir.Get(b, "a")) {
			t.Errorf("%s: the diff is an input", name)
		}
		if after := encodeT(t, a) + encodeT(t, b); after != before {
			t.Errorf("%s: inputs changed:\n%s\nwas:\n%s", name, after, before)
		}
		if out, err := Patch(a, d); err != nil || Diff(out, b) != nil {
			t.Errorf("%s: Patch(a, Diff(a, b)) != b: %v\n%s\nwant\n%s", name, err, encodeT(t, out), encodeT(t, b))
		}
	}
	check("replace", "a: {x: 1}", "a: 2")
	// Block form: a bracketed sparse array carries !bracket, which the patched
	// result does not, and Diff reads a presentation tag as a change.
	check("sparse mismatch", "a: {x: 1}", "a: !sparsearray\n  0: 1\n")
	check("keyed", "l: !key(n) [{n: a, v: 1}, {n: b, v: 1}]", "l: !key(n) [{n: a, v: 2}, {n: c, v: 1}]")
	check("index", "l: [1, 2, 3]", "l: [1, 5, 3, 4]")
}
