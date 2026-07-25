package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/anymap"
)

// TestUpstreamItem7_AnyMapRoundTrip guards issue f69agjyeh12ks item 7: a
// map[string]any field must generate (codegen previously errored) and round-trip
// through the reflection path.
func TestUpstreamItem7_AnyMapRoundTrip(t *testing.T) {
	orig := &anymap.Pattern{Fields: map[string]any{
		"s": "hello",
		"n": int64(42),
		"b": true,
	}}
	node, err := orig.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	var got anymap.Pattern
	if err := got.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if got.Fields["s"] != "hello" {
		t.Errorf("string value lost: %v", got.Fields["s"])
	}
	if got.Fields["n"] != int64(42) {
		t.Errorf("int value lost: %v (%T)", got.Fields["n"], got.Fields["n"])
	}
	if got.Fields["b"] != true {
		t.Errorf("bool value lost: %v", got.Fields["b"])
	}
}
