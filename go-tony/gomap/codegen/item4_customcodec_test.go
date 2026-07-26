package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/customcodec"
)

// TestUpstreamItem4_CustomCodec guards issue f69agjyeh12ks item 4: a type marked
// codec=custom is resolvable (the referencing Host calls its methods) but its
// codec is not generated, so the package compiles alongside the hand-written
// methods and those methods are what actually run.
func TestUpstreamItem4_CustomCodec(t *testing.T) {
	node, err := (&customcodec.Host{Leaf: customcodec.Leaf{V: "x"}}).ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	leaf, _ := node.GetPath("$.leaf")
	if leaf == nil || leaf.String != "LEAF:x" {
		t.Fatalf("hand-written Leaf.ToTonyIR not used: %v", leaf)
	}
	var got customcodec.Host
	if err := got.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if got.Leaf.V != "from:LEAF:x" {
		t.Fatalf("hand-written Leaf.FromTonyIR not used: %q", got.Leaf.V)
	}
}
