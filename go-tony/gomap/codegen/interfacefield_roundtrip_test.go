package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/anyfield"
)

// TestUpstreamItem7_InterfaceFieldRoundTrip guards the latent bug found alongside
// issue f69agjyeh12ks item 7: a bare interface{} field generated calls to
// toIRInterface/fromIRInterface, which do not exist (uncompilable code). It now
// dispatches through gomap.ToTonyIR/FromTonyIR and round-trips.
func TestUpstreamItem7_InterfaceFieldRoundTrip(t *testing.T) {
	orig := &anyfield.Box{Any: "hello"}
	node, err := orig.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	var got anyfield.Box
	if err := got.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if got.Any != "hello" {
		t.Errorf("interface field value lost: %v", got.Any)
	}
}
