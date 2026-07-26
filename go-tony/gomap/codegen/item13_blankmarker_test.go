package codegen

import (
	"strings"
	"testing"
)

// TestUpstreamItem13_BlankFieldMarkerDiagnosed guards issue f69agjyeh12ks item 13:
// a schema tag on a named (blank) field is not the recognized marker form; codegen
// must diagnose it rather than process the package and silently generate nothing.
func TestUpstreamItem13_BlankFieldMarkerDiagnosed(t *testing.T) {
	file, _, err := ParseFile("testdata/blankmarker/types.go")
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	_, err = ExtractTypes(file, "testdata/blankmarker/types.go")
	if err == nil {
		t.Fatal("expected a diagnostic for the blank-field schema marker, got nil (silently ignored)")
	}
	for _, want := range []string{`"_"`, "schema tag", "doc comment"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("diagnostic missing %q; got: %v", want, err)
		}
	}
}
