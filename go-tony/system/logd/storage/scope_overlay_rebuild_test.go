package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// An overlay is recognised from the LOG, not inferred from the index, so it survives an
// index rebuild -- which is what happens whenever index.gob is missing or a compaction
// left it inconsistent with the log (persistedIndexStale).
//
// The spike inferred it from the index instead (EndTx == -1), which a rebuild cannot
// reproduce: index.Build takes the tx from entry.TxSource and an overlay has none. That
// degraded gracefully -- the overlay became an ordinary scope patch, replayed redundantly,
// and the read came out the same -- but it silently cost the whole optimisation until the
// next snapshot, and left the read path unable to exclude the entry it is the base of.
func TestScopeOverlay_SurvivesIndexRebuild(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	scope := "s1"
	s.EnableScopeOverlay(true)

	mustCommit(t, s, nil, `{a: {x: 0}, keep: 1}`)
	for i := 0; i < 5; i++ {
		commitAt(t, s, &scope, "a.x", "7")
	}
	commit, _ := s.GetCurrentCommit()
	if err := s.WriteScopeOverlay(scope, commit); err != nil {
		t.Fatalf("WriteScopeOverlay: %v", err)
	}
	// Writes on both layers after the overlay, so ordering matters.
	commitAt(t, s, &scope, "a.y", "9")
	commitAt(t, s, nil, "keep", "2")
	c, _ := s.GetCurrentCommit()

	before, beforeReplay := readBoth(t, s, c, scope)
	if before != beforeReplay {
		t.Fatalf("overlay and replay already disagree before the restart:\n %s\n %s", before, beforeReplay)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "index.gob")); err != nil {
		t.Fatalf("remove index: %v", err)
	}
	re, err := Open(root, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer re.Close()

	seen := 0
	for _, seg := range re.index.AllSegments() {
		if seg.KindedPath == "" && isOverlaySegment(seg) {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("after a rebuild the index recognises %d overlays, want 1", seen)
	}

	after, afterReplay := readBoth(t, re, c, scope)
	t.Logf("before restart: %s", before)
	t.Logf("after restart:  %s", after)
	if after != before {
		t.Errorf("a restart changed the overlay read\n before: %s\n after:  %s", before, after)
	}
	if after != afterReplay {
		t.Errorf("after the restart overlay and replay disagree\n overlay: %s\n replay:  %s",
			after, afterReplay)
	}
}
