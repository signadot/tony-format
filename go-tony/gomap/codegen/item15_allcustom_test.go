package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/allcustom"
)

// TestUpstreamItem15_AllCustomPackage guards the edge case found alongside item 14:
// a package whose schema types are all codec=custom must generate a compilable
// file (package-only, no unused imports) rather than a header + imports with no
// methods. Importing and using the package here proves it compiles; the hand-
// written codec still works.
func TestUpstreamItem15_AllCustomPackage(t *testing.T) {
	node, err := (&allcustom.Thing{V: "x"}).ToTonyIR()
	if err != nil {
		t.Fatal(err)
	}
	var got allcustom.Thing
	if err := got.FromTonyIR(node); err != nil {
		t.Fatal(err)
	}
	if got.V != "x" {
		t.Fatalf("codec=custom round-trip failed: %q", got.V)
	}
}
