package storage

import (
	"testing"
	"time"
)

// TestCompactionDropsPathIndexing shows what compacting a patch away costs the index:
// a patch entry is indexed at the root AND at every path inside it (indexPatchRec),
// but the snapshot that replaces it is indexed ONLY at the root (createSnapshot sets
// KindedPath ""). So once compaction removes the patches, a path-level lookup — the
// one thing the per-path index exists for, watch replay via ReadPatchesInRange — finds
// nothing for that range, even though the root lookup still finds the snapshot.
//
// This is the mechanism behind "scopes would lose indexing if compacted": scope
// patches are exempt from compaction today (selectSurvivors keeps every one), so this
// is demonstrated on baseline, where compaction does run. Any scope compaction that
// replaced patches with a materialized snapshot would land here identically.
func TestCompactionDropsPathIndexing(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	for i := 0; i < 5; i++ {
		scalingCommit(t, s, nil, `{users: {alice: {age: 1}}}`, nil)
	}

	// LookupRange(kp) returns ancestors as well as exact matches — the root's own
	// segments come back for every path — so count only the copies actually indexed AT
	// the path. Those are the ones indexPatchRec created and the ones that tell a watch
	// replay "this commit touched users.alice".
	from, to := int64(0), int64(100)
	atPath := func() int {
		n := 0
		for _, seg := range s.index.LookupRange("users.alice", &from, &to, nil) {
			if seg.KindedPath == "users.alice" {
				n++
			}
		}
		return n
	}
	beforePath := atPath()
	if beforePath == 0 {
		t.Fatal("expected path-level segments before compaction")
	}

	// Cutoff 0: every patch is older than "now minus nothing", so none survives.
	s.SetCompactionConfig(&CompactionConfig{
		Cutoff:       0,
		BaseInterval: time.Hour,
		SlotsPerTier: 8,
		Multiplier:   2,
		GracePeriod:  0,
	})
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	// Compaction runs on the INACTIVE log, which is the one the patches went to only
	// after the switch above; switch once more so it is compacted.
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}

	afterPath := atPath()
	afterRoot := s.index.LookupRange("", &from, &to, nil)

	t.Logf("segments indexed AT users.alice: before=%d after=%d", beforePath, afterPath)
	t.Logf("root-level segments: after=%d", len(afterRoot))
	for _, seg := range afterRoot {
		kind := "patch"
		if seg.StartCommit == seg.EndCommit {
			kind = "snapshot"
		}
		t.Logf("  root %s [%d,%d] path=%q", kind, seg.StartCommit, seg.EndCommit, seg.KindedPath)
	}

	if len(afterRoot) == 0 {
		t.Fatal("expected the snapshot to remain at the root")
	}
	if afterPath != 0 {
		t.Fatalf("expected path-level indexing to be gone, got %d segments", afterPath)
	}
}
