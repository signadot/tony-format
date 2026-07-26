package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/structomit"
	"github.com/signadot/tony-format/go-tony/ir"
)

type reflInner struct {
	Kind string `tony:"field=kind"`
}
type reflConfig struct {
	Addr   string    `tony:"field=addr"`
	Format reflInner `tony:"field=format,omitzero"`
}

// TestUpstreamItem12_StructOmitzero guards item 12: a value-struct field marked
// omitzero must be dropped when zero — the same on the generated and reflection
// paths (codegen emitted the nested call unconditionally, producing format: {}).
func TestUpstreamItem12_StructOmitzero(t *testing.T) {
	gen, err := (&structomit.Config{Addr: "x"}).ToTonyIR()
	if err != nil {
		t.Fatal(err)
	}
	refl, err := gomap.ToTonyIR(&reflConfig{Addr: "x"})
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, n *ir.Node) {
		if v, _ := n.GetPath("$.format"); v != nil {
			t.Errorf("%s: zero struct-omitzero field emitted: %v", name, v)
		}
		if v, _ := n.GetPath("$.addr"); v == nil || v.String != "x" {
			t.Errorf("%s: addr missing", name)
		}
	}
	check("generated", gen)
	check("reflection", refl)

	// A non-zero value is still emitted.
	set, _ := (&structomit.Config{Addr: "x", Format: structomit.Inner{Kind: "k"}}).ToTonyIR()
	if v, _ := set.GetPath("$.format.kind"); v == nil || v.String != "k" {
		t.Errorf("generated: non-zero struct-omitzero field was dropped")
	}
}
