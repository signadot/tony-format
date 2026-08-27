package tony

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A document may HOLD a patch, a rule, a charter -- values whose own tags are
// operator tags. Diffing one has a step the ordinary case does not: libdiff.Escape
// wraps the value in !raw so the delta does not read as an instruction, and Patch
// unwraps it, so the invariant rests on the two being exact inverses.
//
// The property generator cannot reach this. Its tags come from
//
//	nonOpTags = []string{"", "", "", "!t1", "!t2"}
//
// which are deliberately unregistered, so no value it produces would ever be
// escaped. These are the cases it cannot generate, written out.
//
// They pass -- the escape path works. That is worth pinning rather than assuming,
// since the escape is invisible in the result and a regression in it would look
// like a patch that quietly deleted something.
func TestOperatorTagAsData(t *testing.T) {
	tests := []struct{ name, a, b string }{{
		name: "install a subtree that holds a !delete",
		a:    "{rule: {}}",
		b:    "rule:\n  spec: !delete null",
	}, {
		name: "install a !let",
		a:    "{}",
		b:    "tpl: !let\n- x: 1\n- .[x]",
	}, {
		name: "install a whole patch as a value",
		a:    "{}",
		b:    "patch:\n  a: !delete null\n  b: !insert 3",
	}, {
		name: "change a value beside a held op",
		a:    "rule:\n  n: 1\n  spec: !delete null",
		b:    "rule:\n  n: 2\n  spec: !delete null",
	}, {
		name: "remove a held op",
		a:    "rule:\n  spec: !delete null",
		b:    "{rule: {}}",
	}, {
		name: "an !insert as data",
		a:    "{}",
		b:    "p: !insert 5",
	}, {
		name: "the op changes, which is a change of data",
		a:    "p: !delete null",
		b:    "p: !insert 5",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			a, err := parse.Parse([]byte(test.a))
			if err != nil {
				t.Fatalf("a: %s", err)
			}
			b, err := parse.Parse([]byte(test.b))
			if err != nil {
				t.Fatalf("b: %s", err)
			}
			d := Diff(a.Clone(), b.Clone())
			if d == nil {
				t.Fatalf("no diff between\n a: %s\n b: %s", test.a, test.b)
			}
			got, err := Patch(a.Clone(), d.Clone())
			if err != nil {
				t.Fatalf("Patch failed: %s\n delta: %s", err, encode.MustString(d))
			}
			if got == nil {
				t.Fatalf("Patch resolved the document away\n delta: %s", encode.MustString(d))
			}
			// Fields are compared in the order Patch leaves them, which is sorted;
			// see the note on leftover. Both sides here are written sorted.
			if !got.DeepEqual(b) {
				t.Errorf("Patch(a, Diff(a,b)) != b\n delta: %s\n got:   %s\n want:  %s",
					encode.MustString(d), encode.MustString(got), encode.MustString(b))
			}
		})
	}
}
