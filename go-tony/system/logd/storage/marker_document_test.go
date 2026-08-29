package storage

import (
	"strings"
	"testing"
)

// !logd-patch-root is logd's own bookkeeping: it says where a stored entry is applied
// FROM, and it is no part of anybody's document. A document carrying one hands it to the
// next operation as though it were data, and an operation which asks the document for
// its tag then refuses:
//
//	delete patching "!logd-patch-root\n{\n  k1: 4\n}"
//	gave doc tag !bracket.logd-patch-root at $ didn't match bracket
//
// The empty-base branch folds each patch into an accumulating document and stripped the
// marker once, after the last fold -- so every patch after the first was applied to a
// document still wearing the previous patch's marker. Four baseline writes at the root
// reach it, with no scope, no snapshot and nothing keyed (2w62pyyah12ksqh0jdn0).
//
// It takes a delta marked at the document ROOT, which is what markDeltaRoots produces
// when it cannot descend to a deeper container, so lowering is what makes it reachable.
// Both arms are kept because the difference between them is the point: the same four
// writes are clean when only the writes that need lowering are lowered.
func TestAPatchRootMarkerDoesNotReachTheDocument(t *testing.T) {
	for _, lowerAll := range []bool{false, true} {
		name := "lowering the writes that need it"
		if lowerAll {
			name = "lowering every write"
		}
		t.Run(name, func(t *testing.T) {
			s := openTestStorage(t)
			if lowerAll {
				s.LowerEverything(true)
			}
			for _, src := range []string{
				`{k0: 0}`,
				`!rename [{from: "k0", to: "k0"}]`,
				`{k1: 4}`,
				`!delete`,
			} {
				if _, err := applyScopeOp(t, s, scopeOp{path: "", src: src}, "s1"); err != nil {
					t.Fatalf("%s: %v", src, err)
				}
			}
			c, err := s.GetCurrentCommit()
			if err != nil {
				t.Fatal(err)
			}
			doc, err := s.ReadStateAt("", c, nil)
			if err != nil {
				t.Fatalf("baseline read: %v", err)
			}
			if doc != nil {
				if got := withComments(doc); strings.Contains(got, "logd-patch-root") {
					t.Errorf("the document carries logd's marker: %s", got)
				}
			}
		})
	}
}
