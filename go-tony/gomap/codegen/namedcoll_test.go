package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/namedcoll"
)

// A field whose type is a locally declared named slice or map with no directive
// is that collection under its name: it resolved to int, and the generated code
// did not compile (4ynqp7wqh12krg32msn0 item 24). The fixture building is most of
// the test; the round trip pins the values.
func TestNamedCollectionFieldRoundTrips(t *testing.T) {
	in := &namedcoll.Host{Labels: namedcoll.Labels{"a": "1"}, Names: namedcoll.Names{"x", "y"}}
	node, err := in.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	var out namedcoll.Host
	if err := out.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if out.Labels["a"] != "1" || len(out.Names) != 2 || out.Names[1] != "y" {
		t.Errorf("round trip: %+v", out)
	}
}
