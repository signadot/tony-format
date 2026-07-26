package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/omitprobe"
	"github.com/signadot/tony-format/go-tony/ir"
)

// reflProbe mirrors omitprobe.Probe but has no generated codec, so gomap.ToTonyIR
// takes the reflection path.
type reflProbe struct {
	ID     string `tony:"field=id"`
	Secret string `tony:"field=secret,omit"`
	Dashed string `tony:"-"`
	Named  string `tony:"field=-"`
}

// TestUpstreamItem11_OmitBothPaths guards issue f69agjyeh12ks item 11: omit / - /
// field=- must exclude a field from the wire on BOTH the generated and the
// reflection path, so the same tags never produce different documents and a field
// explicitly marked not to serialize (a secret) never leaks.
func TestUpstreamItem11_OmitBothPaths(t *testing.T) {
	gen, err := (&omitprobe.Probe{ID: "x", Secret: "shh", Dashed: "d", Named: "n"}).ToTonyIR()
	if err != nil {
		t.Fatalf("generated ToTonyIR: %v", err)
	}
	refl, err := gomap.ToTonyIR(&reflProbe{ID: "x", Secret: "shh", Dashed: "d", Named: "n"})
	if err != nil {
		t.Fatalf("reflection ToTonyIR: %v", err)
	}

	check := func(name string, n *ir.Node) {
		for _, leaked := range []string{"secret", "Secret", "Dashed", "Named", "dashed", "named"} {
			if v, _ := n.GetPath("$." + leaked); v != nil {
				t.Errorf("%s path leaked omitted field %q: %v", name, leaked, v)
			}
		}
		if v, _ := n.GetPath("$.id"); v == nil || v.String != "x" {
			t.Errorf("%s path dropped id", name)
		}
		if len(n.Fields) != 1 {
			t.Errorf("%s path emitted %d fields, want only id", name, len(n.Fields))
		}
	}
	check("generated", gen)
	check("reflection", refl)
}
