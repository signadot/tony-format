package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/omitzero"
	"github.com/signadot/tony-format/go-tony/ir"
)

type reflOZ struct {
	Plain string  `tony:"field=plain"`
	Str   string  `tony:"field=str,omitzero"`
	Num   int     `tony:"field=num,omitzero"`
	Flt   float64 `tony:"field=flt,omitzero"`
	Flag  bool    `tony:"field=flag,omitzero"`
}

// TestUpstreamItem10_OmitzeroBothPaths guards issue f69agjyeh12ks item 10:
// omitzero must drop a zero-valued scalar on BOTH the generated and reflection
// paths (previously it worked only for bool in codegen and not at all on the
// reflection path), while an untagged empty field is still emitted.
func TestUpstreamItem10_OmitzeroBothPaths(t *testing.T) {
	genZero, err := (&omitzero.OZ{}).ToTonyIR()
	if err != nil {
		t.Fatal(err)
	}
	reflZero, err := gomap.ToTonyIR(&reflOZ{})
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, n *ir.Node) {
		for _, dropped := range []string{"str", "num", "flt", "flag"} {
			if v, _ := n.GetPath("$." + dropped); v != nil {
				t.Errorf("%s: omitzero field %q emitted when zero: %v", name, dropped, v)
			}
		}
		if v, _ := n.GetPath("$.plain"); v == nil {
			t.Errorf("%s: untagged empty field 'plain' was dropped", name)
		}
	}
	check("generated", genZero)
	check("reflection", reflZero)

	// Non-zero values are still emitted.
	genSet, _ := (&omitzero.OZ{Str: "x", Num: 1, Flt: 1.5, Flag: true}).ToTonyIR()
	for _, kept := range []string{"str", "num", "flt", "flag"} {
		if v, _ := genSet.GetPath("$." + kept); v == nil {
			t.Errorf("generated: non-zero omitzero field %q was dropped", kept)
		}
	}
}
