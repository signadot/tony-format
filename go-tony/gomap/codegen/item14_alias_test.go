package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/aliasref"
	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/aliastarget"
)

// TestUpstreamItem14_CrossPackageAlias guards issue f69agjyeh12ks item 14: a field
// whose type is a type alias re-exported from another package
// (type Format = format.Format) must resolve to the target named type (via
// types.Unalias) rather than failing with "is not a named type".
func TestUpstreamItem14_CrossPackageAlias(t *testing.T) {
	orig := &aliasref.Host{Default: &aliastarget.Format{V: "x"}}
	node, err := orig.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	var got aliasref.Host
	if err := got.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if got.Default == nil || got.Default.V != "x" {
		t.Fatalf("alias field did not round-trip: %+v", got.Default)
	}
}
