package tony

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// A namespaced tag (!acme:thing, mergeop.NamespaceSep) is a tag a diff can carry
// across: Diff wrote !insert(acme:thing) and Patch refused the ':' in the argument,
// so Patch(a, Diff(a, b)) was not b for any document carrying one
// (p478tacqh12krg32msn0 item 2).
func TestNamespacedTagSurvivesDiffAndPatch(t *testing.T) {
	a, b := parseT(t, "{}"), parseT(t, "a: !acme:thing 1\nb: !acme:other {x: 1}\n")
	d := Diff(a, b)
	out, err := Patch(a, d)
	if err != nil {
		t.Fatalf("Patch(a, Diff(a, b)): %v\ndiff:\n%s", err, encodeT(t, d))
	}
	if Diff(out, b) != nil {
		t.Errorf("Patch(a, Diff(a, b)) != b:\n%s\nwant\n%s", encodeT(t, out), encodeT(t, b))
	}
	if !ir.TagHas(ir.Get(out, "a").Tag, "!acme:thing") {
		t.Errorf("a carries %q, want !acme:thing", ir.Get(out, "a").Tag)
	}
	// And back: removing the tag is a diff too.
	if out, err := Patch(b, Diff(b, a)); err != nil || Diff(out, a) != nil {
		t.Errorf("Patch(b, Diff(b, a)) != a: %v", err)
	}
}
