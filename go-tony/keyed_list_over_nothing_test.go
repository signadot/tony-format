package tony

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// A keyed list written where there was no list -- nothing at the path, or a scalar
// -- carries what the patch wrote on it, and not the scalar's tag: !key(name) [..]
// over `!foo 5` came back tagged !foo (4ynqp7wqh12krg32msn0 item 13). The !key
// itself is the patch's instruction rather than the list's data: logd holds identity
// in the schema and raises it onto each patch, and what a client steps to by a delta
// must be what a read answers.
func TestKeyedListWrittenOverAScalarDoesNotWearItsTag(t *testing.T) {
	for _, base := range []string{"{}", "items: !foo 5", "items: null"} {
		out, err := Patch(parseT(t, base), parseT(t, "items: !key(name) [{name: a}]"))
		if err != nil {
			t.Fatalf("over %s: %v", base, err)
		}
		items := ir.Get(out, "items")
		if items == nil || items.Type != ir.ArrayType || len(items.Values) != 1 {
			t.Fatalf("over %s: items = %v", base, items)
		}
		// What the patch wrote on it -- its !bracket -- and nothing else.
		if ir.TagHas(items.Tag, "!foo") || ir.TagHas(items.Tag, "!key") {
			t.Errorf("over %s: items tag %q, want neither the scalar's !foo nor the op's !key", base, items.Tag)
		}
	}
	// A list that is there keeps its own tag, keyed or not.
	out, _ := Patch(parseT(t, "items: !key(name) [{name: a}]"), parseT(t, "items: !key(name) [{name: b}]"))
	if items := ir.Get(out, "items"); !ir.TagHas(items.Tag, "!key") {
		t.Errorf("a keyed list lost its keying: %q", items.Tag)
	}
	out, _ = Patch(parseT(t, "items: [{name: a}]"), parseT(t, "items: !key(name) [{name: b}]"))
	if items := ir.Get(out, "items"); ir.TagHas(items.Tag, "!key") {
		t.Errorf("an unkeyed list gained keying from the patch: %q", items.Tag)
	}
}
