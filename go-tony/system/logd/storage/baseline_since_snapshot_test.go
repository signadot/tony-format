package storage

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// scope_scaling_test measures baseline immediately after a snapshot, which is its BEST
// case, not its steady state. This separates the two mechanisms that keep baseline fast,
// because they have different bounds and the plan leaned on the wrong one:
//
//   - a snapshot truncates the COLD read path (replayBaselineAt replays from the last
//     snapshot forward, with nothing cached), so a baseline read costs O(patches since
//     the snapshot) and only resets when one is taken
//   - the stepped head (head.go) and the stepped watch document (session.go) are not
//     bounded by the snapshot at all: they fold each commit's patch into a document they
//     keep, so a CAS precondition and a watch event cost the size of the patch
//
// If that reading is right, holding the snapshot fixed and adding M commits after it
// makes the read grow while the conditional write stays flat.
func TestBaseline_CostSinceSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("scaling measurement")
	}
	const reps = 20
	sizes := []int{0, 50, 100, 200, 400}

	t.Log("baseline READ, M commits after the snapshot (no stepping on this path):")
	for _, m := range sizes {
		s, _ := setupStore(t, 20, 0, nil)
		for i := 0; i < m; i++ {
			scalingCommit(t, s, nil, `{ctr: 1, tag: "t"}`, nil)
		}
		commit, err := s.GetCurrentCommit()
		if err != nil {
			t.Fatalf("GetCurrentCommit: %v", err)
		}
		d := timeN(reps, func() {
			if _, err := s.ReadStateAt("", commit, nil); err != nil {
				t.Fatalf("read: %v", err)
			}
		})
		t.Logf("  M=%4d  %v", m, d)
		s.Close()
	}

	t.Log("baseline CONDITIONAL write, M commits after the snapshot (stepped head):")
	for _, m := range sizes {
		s, _ := setupStore(t, 20, 0, nil)
		for i := 0; i < m; i++ {
			scalingCommit(t, s, nil, `{ctr: 1, tag: "t"}`, nil)
		}
		match := matchTag(t)
		d := timeN(reps, func() {
			scalingCommit(t, s, nil, `{ctr: 1, tag: "t"}`, match)
		})
		t.Logf("  M=%4d  %v", m, d)
		s.Close()
	}

	t.Log("")
	t.Log("A read that grows with M while a conditional write stays flat says the head is")
	t.Log("what keeps the write flat, not the snapshot -- and that the two costs have")
	t.Log("different bounds, so a scope reaching parity needs BOTH mechanisms.")
}

var _ = api.Patch{}
