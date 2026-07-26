package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/namedmap"
)

// TestUpstreamItem2_NamedMapDispatch guards issue f69agjyeh12ks item 2: a field
// whose type is a named map with its own codec must dispatch to that codec, not
// have codegen inline the map (which lost the codec and generated invalid Go).
func TestUpstreamItem2_NamedMapDispatch(t *testing.T) {
	node, err := (&namedmap.Host{M: namedmap.Match{"k": "v"}}).ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	m, _ := node.GetPath("$.m")
	if m == nil || m.String != "MATCH" {
		t.Fatalf("named map field did not dispatch to hand-written ToTonyIR: %v", m)
	}
	var got namedmap.Host
	if err := got.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if got.M["decoded"] != "MATCH" {
		t.Fatalf("named map field did not dispatch to hand-written FromTonyIR: %v", got.M)
	}
}
