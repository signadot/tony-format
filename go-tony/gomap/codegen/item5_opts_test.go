package codegen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/schema"
)

// TestUpstreamItem5_NestedCallsPassOpts guards issue f69agjyeh12ks item 5: a nested
// field call in generated code must pass opts... A local type resolved from source
// is a placeholder reflect.Type with no package path, which methodAcceptsOpts failed
// to recognize, so every nested call in this repo's generated files was bare.
func TestUpstreamItem5_NestedCallsPassOpts(t *testing.T) {
	pkgDir, err := filepath.Abs("testdata/nestedopts")
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
	if err := ResolveFieldTypes(structs, pkgDir, "nestedopts"); err != nil {
		t.Fatalf("ResolveFieldTypes: %v", err)
	}
	var host *StructInfo
	for _, s := range structs {
		if s.Name == "Host" {
			host = s
		}
	}
	if host == nil {
		t.Fatal("Host not found")
	}
	const pkgPath = "github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/nestedopts"
	sch := &schema.Schema{Signature: &schema.Signature{Name: "nestedopts-host"}}

	toCode, err := GenerateToTonyIRMethod(host, sch, pkgPath)
	if err != nil {
		t.Fatalf("GenerateToTonyIRMethod: %v", err)
	}
	fromCode, err := GenerateFromTonyIRMethod(host, sch, pkgPath)
	if err != nil {
		t.Fatalf("GenerateFromTonyIRMethod: %v", err)
	}

	// A value struct field and a pointer field, both encode and decode, must pass opts.
	if !strings.Contains(toCode, "s.Child.ToTonyIR(opts...)") {
		t.Errorf("value struct field ToTonyIR is bare:\n%s", toCode)
	}
	if !strings.Contains(fromCode, "s.Child.FromTonyIR(fieldNode, opts...)") {
		t.Errorf("value struct field FromTonyIR is bare:\n%s", fromCode)
	}
	if strings.Contains(toCode, "ToTonyIR()") || strings.Contains(fromCode, "FromTonyIR(fieldNode)") {
		t.Errorf("a nested call is still bare (no opts)")
	}
}
