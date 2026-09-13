package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/namedslice"
)

// A field of a named slice type with a codec of its own dispatches to that codec,
// as a named map already did; inlined element by element, the generated code did
// not compile (p478tacqh12krg32msn0 item 25, the slice form of f69agjye item 2).
func TestNamedSliceFieldDispatchesToItsCodec(t *testing.T) {
	node, err := (&namedslice.Host{N: namedslice.Names{"a"}}).ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	if n := node.Values[0]; n.String != "NAMES" {
		t.Errorf("encoded through the generator, not the codec: %v", n)
	}
	var out namedslice.Host
	if err := out.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if len(out.N) != 2 || out.N[0] != "decoded" {
		t.Errorf("decoded through the generator, not the codec: %v", out.N)
	}
}
