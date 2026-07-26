package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/emptycoll"
	"github.com/signadot/tony-format/go-tony/ir"
)

type reflColl struct {
	Items []string `tony:"field=items"`
	OZ    []string `tony:"field=oz,omitzero"`
}

// TestUpstreamItem10B_EmptySlice guards item 10B: an untagged empty slice is
// emitted (as []) so a caller can put an explicitly-empty slice on the wire, while
// an omitzero one is dropped — the same on both the generated and reflection paths.
func TestUpstreamItem10B_EmptySlice(t *testing.T) {
	gen, err := (&emptycoll.Coll{}).ToTonyIR()
	if err != nil {
		t.Fatal(err)
	}
	refl, err := gomap.ToTonyIR(&reflColl{})
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, n *ir.Node) {
		if v, _ := n.GetPath("$.items"); v == nil || v.Type != ir.ArrayType {
			t.Errorf("%s: untagged empty slice not emitted as []: %v", name, v)
		}
		if v, _ := n.GetPath("$.oz"); v != nil {
			t.Errorf("%s: omitzero empty slice was emitted: %v", name, v)
		}
	}
	check("generated", gen)
	check("reflection", refl)
}
