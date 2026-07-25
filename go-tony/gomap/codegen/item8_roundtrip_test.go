package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/namedscalar"
)

// TestUpstreamItem8_NamedScalarRoundTrip guards issue f69agjyeh12ks item 8: a
// named scalar type used as a slice element must round-trip through generated
// codecs (previously it fell back to int and failed to compile / convert).
func TestUpstreamItem8_NamedScalarRoundTrip(t *testing.T) {
	orig := &namedscalar.GateMatch{
		Verbs:  []namedscalar.Verb{"read", "9write"},
		Counts: []namedscalar.Count{1, 2, 3},
		Names:  []string{"a", "b"},
	}
	node, err := orig.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	var got namedscalar.GateMatch
	if err := got.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if len(got.Verbs) != 2 || got.Verbs[0] != "read" || got.Verbs[1] != "9write" {
		t.Errorf("Verbs round-trip mismatch: %v", got.Verbs)
	}
	if len(got.Counts) != 3 || got.Counts[2] != 3 {
		t.Errorf("Counts round-trip mismatch: %v", got.Counts)
	}
	_ = gomap.EncodeWire
}
