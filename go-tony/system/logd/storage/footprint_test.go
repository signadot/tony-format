package storage

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

// liveOf renders a scope's live statements at the root as "path@commit file:pos".
func liveOf(s *Storage, scope string) string {
	var parts []string
	for _, st := range s.index.Footprint().Live(scope, "") {
		parts = append(parts, fmt.Sprintf("%s@%d %s:%d", st.Path, st.Commit, st.LogFile, st.LogPosition))
	}
	return strings.Join(parts, "\n")
}

// The footprint a store reopens with is the one it closed with, and the one a rebuild
// from the log arrives at.
func TestFootprintReopenEqualsRebuild(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	const scope = "s1"
	rng := rand.New(rand.NewSource(7))
	for _, o := range genScopeOps(rng, 80) {
		if _, err := applyScopeOp(t, s, o, scope); err != nil {
			t.Fatalf("%s: %v", o, err)
		}
	}
	closed := liveOf(s, scope)
	if closed == "" {
		t.Fatalf("the stream left no live statement")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := liveOf(s, scope); got != closed {
		t.Errorf("reopened footprint differs\n closed:\n%s\n reopened:\n%s", closed, got)
	}
	s.index.RebuildFootprint()
	if got := liveOf(s, scope); got != closed {
		t.Errorf("rebuilt footprint differs\n closed:\n%s\n rebuilt:\n%s", closed, got)
	}
}

// Deleting a scope pages in the nodes the scope wrote at, above and beneath, and nothing
// else: under a ceiling that has evicted most of a store of a thousand paths, a scope
// that wrote under three of them costs a handful of page-ins, not the index.
func TestDeleteScopePagesNothingOutsideItsFootprint(t *testing.T) {
	s, paths := shapedStore(t, shape{paths: 300, writesPerPath: 2, snapshotEvery: 0,
		ancestors: []string{"verse.git.ref", "verse.github.issue", "verse.github.comment"}})
	sc := "sandbox"
	for _, p := range paths[:3] {
		if err := scopedCommit(t, s, &sc, p, `{v: 9}`); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetIndexCeiling(index.MinIndexCeiling); err != nil {
		t.Fatalf("SetIndexCeiling: %v", err)
	}
	before := s.index.Residency().Stats()
	if before.Evictions == 0 {
		t.Fatalf("the ceiling evicted nothing; the test cannot tell paging apart")
	}
	if err := s.DeleteScope(sc); err != nil {
		t.Fatalf("DeleteScope: %v", err)
	}
	after := s.index.Residency().Stats()
	paged := after.Misses - before.Misses
	// The nodes on three paths of depth five, and every region of each: the root and the
	// shared ancestors are large, since every write passes through them, so this is not a
	// handful, but it is the scope's nodes and not the trie's 300 paths' worth.
	if paged > before.Evictions/4 {
		t.Errorf("deleting a scope of three paths paged in %d regions of the %d evicted", paged, before.Evictions)
	}
	t.Logf("DeleteScope paged in %d regions of an index that had evicted %d", paged, before.Evictions)
	for seg := range s.index.Segments("", nil, nil, &sc) {
		if seg.ScopeID != nil && *seg.ScopeID == sc {
			t.Fatalf("a segment of the deleted scope remains: %v", seg)
		}
	}
	head, _ := s.GetCurrentCommit()
	if got, _, err := readSubtreeAt(s, paths[0]+".v", head, &sc); err != nil || got == nil || got.Int64 == nil || *got.Int64 != 1 {
		t.Errorf("after the delete the scope reads %v at %s.v, want baseline's 1 (err %v)", got, paths[0], err)
	}
}
