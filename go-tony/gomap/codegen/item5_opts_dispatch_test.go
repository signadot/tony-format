package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/namedmap"
)

// TestUpstreamItem5_NestedCodecReceivesOpts guards issue f69agjyeh12ks item 5:
// generated nested calls dropped opts..., so a nested codec could not see the
// encode options — including the target format, which it needs to know when a
// tag has to be spelled differently for JSON.
//
// The shape that kept reproducing after the common cases were fixed is this
// one: a hand-written codec on a type resolved from source. methodAcceptsOpts
// answers from reflection, and a source-resolved type has a placeholder
// reflect.Type with no package path and no methods, so it reported "no opts"
// and the call went out bare. The resolver now asks go/types instead.
func TestUpstreamItem5_NestedCodecReceivesOpts(t *testing.T) {
	namedmap.LastToOpts, namedmap.LastFromOpts = 0, 0

	host := &namedmap.Host{M: namedmap.Match{"k": "v"}}
	node, err := host.ToTonyIR(gomap.EncodeWire(true), gomap.EncodeFormat(format.JSONFormat))
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	if namedmap.LastToOpts != 2 {
		t.Errorf("nested ToTonyIR saw %d options, want 2 — the generated call dropped opts",
			namedmap.LastToOpts)
	}

	var got namedmap.Host
	if err := got.FromTonyIR(node, gomap.ParseFormat(format.JSONFormat)); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if namedmap.LastFromOpts != 1 {
		t.Errorf("nested FromTonyIR saw %d options, want 1 — the generated call dropped opts",
			namedmap.LastFromOpts)
	}
}
