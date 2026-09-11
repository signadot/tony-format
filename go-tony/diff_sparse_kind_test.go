package tony

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A diff between an object and a sparse array turns one into the other, in both
// directions and in both of Diff's modes. It answered with the target itself, which a
// patch merges, so Patch({a: 1}, Diff({a: 1}, !sparsearray {0: y})) kept a: 1 -- and
// logd's lowering, which diffs absolutely, stored that merge (07g0rn9xh12ksz5xmdn0).
func TestDiffBetweenAnObjectAndASparseArrayChangesTheKind(t *testing.T) {
	p := func(s string) *ir.Node {
		n, err := parse.Parse([]byte(s))
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return n
	}
	pairs := [][2]string{
		{`{x: {a: 1}}`, `{x: !sparsearray {0: y}}`},
		{`{x: !sparsearray {0: y}}`, `{x: {a: 1}}`},
		{`{x: 5}`, `{x: !sparsearray {0: y}}`},
	}
	for _, pair := range pairs {
		for _, absolute := range []bool{false, true} {
			name := pair[0] + " -> " + pair[1]
			if absolute {
				name += " absolute"
			}
			t.Run(name, func(t *testing.T) {
				from, to := p(pair[0]), p(pair[1])
				d := DiffWith(from, to, DiffAbsolute(absolute))
				got, err := Patch(from, d)
				if err != nil {
					t.Fatalf("patch with %s: %v", encode.MustString(d), err)
				}
				if left := leftover(got, to); left != nil {
					t.Errorf("Patch(from, Diff(from, to)) = %s, want %s\n diff %s",
						encode.MustString(got), encode.MustString(to), encode.MustString(d))
				}
				gx, _ := got.GetKPath("x")
				tx, _ := to.GetKPath("x")
				if gx == nil || isSparse(gx) != isSparse(tx) {
					t.Errorf("x is %v, want the target's kind", gx)
				}
				if err := sparseKeysAreIntegers(gx); err != nil {
					t.Error(err)
				}
			})
		}
	}
}
