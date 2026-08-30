package api_test

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	api "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// `!raw` ends the chain. Everything composed after it is data that happens to be shaped like
// an operation, which is the whole of what the escape says -- so neither the vocabulary check
// nor the lowering question may read past it.
//
// The escape composes onto the node's OWN tag (`!irtype` escaped is `!raw.irtype`), which is
// what a writer escaping a leaf produces. The subtree form -- `!raw` on a node wrapping
// children -- is the case the `ir.TagHas(n.Tag, "!raw")` short-circuit already covers.
func TestRawEndsTheChain(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		storable  bool
		lowers    bool
	}{
		{"escaped onto the node's own tag", `says: !raw.irtype null`, true, false},
		{"escaped as a subtree", `says: !raw {inner: !irtype null}`, true, false},
		{"not escaped at all", `says: !irtype null`, false, true},
		{"a relative op behind an absolute one", `says: !insert.strdiff null`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := parse.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("parse %q: %v", tc.src, err)
			}
			err = api.ValidateForStorage(n)
			if got := err == nil; got != tc.storable {
				t.Errorf("ValidateForStorage(%s) storable=%v, want %v (%v)",
					tc.src, got, tc.storable, err)
			}
			if op, got := api.NeedsLowering(n); got != tc.lowers {
				t.Errorf("NeedsLowering(%s) = (%q, %v), want %v", tc.src, op, got, tc.lowers)
			}
		})
	}
}
