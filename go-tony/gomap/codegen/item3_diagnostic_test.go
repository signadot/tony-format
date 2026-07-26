package codegen

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestUpstreamItem3_UnresolvableTypeDiagnostic guards issue f69agjyeh12ks item 3:
// a field referencing a type codegen cannot resolve must produce an actionable
// diagnostic, not silently emit invalid Go (ir.FromInt on a struct).
func TestUpstreamItem3_UnresolvableTypeDiagnostic(t *testing.T) {
	// go/packages needs an absolute dir (or import path) to populate go/types.
	pkgDir, err := filepath.Abs("testdata/unresolvable")
	if err != nil {
		t.Fatal(err)
	}
	file, _, err := ParseFile(pkgDir + "/types.go")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	structs, err := ExtractTypes(file, pkgDir+"/types.go")
	if err != nil {
		t.Fatalf("ExtractTypes: %v", err)
	}
	err = ResolveFieldTypes(structs, pkgDir, "unresolvable")
	if err == nil {
		t.Fatal("expected a diagnostic error for the unresolvable struct reference, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"Host.ValLeaf", "Leaf", "codec=custom", "cannot resolve"} {
		if !strings.Contains(msg, want) {
			t.Errorf("diagnostic missing %q; got: %v", want, msg)
		}
	}
}
