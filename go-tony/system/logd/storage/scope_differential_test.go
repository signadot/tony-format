package storage

import (
	"testing"
)

// compareViews is the reading half of every scope differential: a reference store and a
// subject store that took the same stream, read at each of the commits, in both views, at
// each of the paths, and required to agree byte for byte with comments. The write half is
// the caller's, because what makes the subject differ is the thing under test -- compaction
// (scope_compaction_test), lowering (lower_scope_test), the footprint (scope_plan.md,
// phase 2) -- and the reading is the same for all of them.
//
// The invariant every subject is held to is the one index residency stated for itself:
// whatever the subject did changes a COST, never an answer.
func compareViews(t *testing.T, ref, subj *Storage, commits []int64, paths []string, scope string, ops []scopeOp) {
	t.Helper()
	sc := scope
	for _, commit := range commits {
		for _, view := range []*string{nil, &sc} {
			for _, kp := range paths {
				want, _, err := readSubtreeAt(ref, kp, commit, view)
				if err != nil {
					t.Fatalf("reference read %q at %d: %v", kp, commit, err)
				}
				got, _, err := readSubtreeAt(subj, kp, commit, view)
				if err != nil {
					t.Fatalf("subject read %q at %d: %v", kp, commit, err)
				}
				if withComments(got) != withComments(want) {
					t.Fatalf("read %q at %d (scope %v) differs\n reference %s\n subject   %s\n%s",
						kp, commit, view != nil, withComments(want), withComments(got), dumpScopeOps(ops))
				}
			}
		}
	}
}
