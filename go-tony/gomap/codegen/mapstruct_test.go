package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/mapstruct"
)

// TestMapStructValueRoundTrip guards issue cc5rbhv8h12k: a field typed as an
// unnamed map literal whose value is a generated struct (map[string]RepoConfig)
// must be inlined and dispatched per value. codegen used to route it to
// s.Repos.ToTonyIR() — a method the map type does not have — producing code that
// would not compile. That this test builds at all proves the generated code is
// valid; the round-trip proves each value is encoded/decoded through its own
// generated codec.
func TestMapStructValueRoundTrip(t *testing.T) {
	in := &mapstruct.GatewayConfig{
		Repos: map[string]mapstruct.RepoConfig{
			"api": {Branch: "main", Status: mapstruct.StatusConfig{OK: true}},
			"web": {Branch: "dev"},
		},
	}

	node, err := in.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	// The map value is a real object (its struct fields), not a method-dispatched
	// scalar — confirm a nested field made it onto the wire.
	branch, _ := node.GetPath("$.repos.api.branch")
	if branch == nil || branch.String != "main" {
		t.Fatalf("map-of-struct value not encoded per field: %v", branch)
	}

	var got mapstruct.GatewayConfig
	if err := got.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if len(got.Repos) != 2 {
		t.Fatalf("round-trip lost map entries: got %d, want 2", len(got.Repos))
	}
	if got.Repos["api"].Branch != "main" || !got.Repos["api"].Status.OK {
		t.Errorf("api entry did not round-trip: %+v", got.Repos["api"])
	}
	if got.Repos["web"].Branch != "dev" {
		t.Errorf("web entry did not round-trip: %+v", got.Repos["web"])
	}
}
