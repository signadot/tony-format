package storage

import (
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A deleted scope stays deleted (05d8w3cjh12kswb1msn0, items 3 and 4). The log is the
// record, so the deletion is in it, and every way the index is made from the log -- the
// re-index after a compaction, a rebuild, the catch-up after an unclean stop -- leaves
// the scope's entries out.

func writeAs(t *testing.T, s *Storage, scope *string, path, body string) {
	t.Helper()
	node, err := parse.Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse %q: %v", body, err)
	}
	tx, err := s.NewTx(1, scope)
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: path, Data: node}})
	if err != nil {
		t.Fatalf("NewPatcher: %v", err)
	}
	if r := p.Commit(); !r.Committed {
		t.Fatalf("commit %s %q: %v", path, body, r.Error)
	}
}

// scopedX answers a.x as the scope sees it at the head.
func scopedX(t *testing.T, s *Storage, scope string) string {
	t.Helper()
	commit, _ := s.GetCurrentCommit()
	doc, err := readStateAt(s, "", commit, &scope)
	if err != nil {
		t.Fatalf("scoped read: %v", err)
	}
	return getString(doc, "a", "x")
}

func deletedScopeStore(t *testing.T) (*Storage, string, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	scope := "s1"
	writeAs(t, s, nil, "", `{a: {x: "1"}}`)
	writeAs(t, s, &scope, "a.x", `"2"`)
	if got := scopedX(t, s, scope); got != "2" {
		t.Fatalf("before delete: a.x = %q, want 2", got)
	}
	if err := s.DeleteScope(scope); err != nil {
		t.Fatalf("DeleteScope: %v", err)
	}
	if got := scopedX(t, s, scope); got != "1" {
		t.Fatalf("after delete: a.x = %q, want 1", got)
	}
	return s, dir, scope
}

// cutoffOf is a compaction configuration whose cutoff is d ago.
func cutoffOf(d time.Duration) *CompactionConfig {
	return &CompactionConfig{
		Cutoff:       d,
		BaseInterval: time.Hour,
		SlotsPerTier: 8,
		Multiplier:   2,
		GracePeriod:  10 * time.Millisecond,
	}
}

// The re-index after a compaction re-adds every survivor; a deleted scope's entry within
// the cutoff survived, and came back with the rest.
func TestDeleteScope_SurvivesCompactionReindex(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	scope := "s1"
	writeAs(t, s, nil, "", `{a: {x: "1"}}`)
	writeAs(t, s, nil, "b", `"old"`)    // beyond the cutoff below: what makes the compaction a rewrite
	time.Sleep(1200 * time.Millisecond) // entry times are whole seconds
	writeAs(t, s, &scope, "a.x", `"2"`) // within it
	if err := s.DeleteScope(scope); err != nil {
		t.Fatalf("DeleteScope: %v", err)
	}
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	if err := s.Compact(cutoffOf(time.Second)); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if got := scopedX(t, s, scope); got != "1" {
		t.Errorf("after compaction within the cutoff: scoped a.x = %q, want 1 (the deleted scope came back)", got)
	}
	if scopes := s.index.Footprint().Scopes(); len(scopes) != 0 {
		t.Errorf("footprint holds %v after delete and compaction, want none", scopes)
	}

	// And beyond the cutoff, where the entry itself is dropped, it stays gone.
	writeAs(t, s, nil, "b", `"later"`)
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	if err := s.Compact(everythingBeyondCutoff()); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if got := scopedX(t, s, scope); got != "1" {
		t.Errorf("after compaction beyond the cutoff: scoped a.x = %q, want 1", got)
	}
}

// A rebuild from the log alone finds the deletion in the log.
func TestDeleteScope_SurvivesRebuild(t *testing.T) {
	s, dir, scope := deletedScopeStore(t)
	s = reopenRebuilt(t, s, dir)
	defer s.Close()
	if got := scopedX(t, s, scope); got != "1" {
		t.Errorf("after rebuild: scoped a.x = %q, want 1 (the deleted scope came back)", got)
	}
}

// An unclean stop leaves a manifest from before the delete; the catch-up from it finds
// the deletion in the log, whatever commit the manifest describes.
func TestDeleteScope_SurvivesUncleanReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	scope := "s1"
	writeAs(t, s, nil, "", `{a: {x: "1"}}`)
	writeAs(t, s, &scope, "a.x", `"2"`)
	if err := s.index.Persist(s.logGenerations()); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if err := s.DeleteScope(scope); err != nil {
		t.Fatalf("DeleteScope: %v", err)
	}
	// No Close: the manifest predates the delete.
	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if got := scopedX(t, s2, scope); got != "1" {
		t.Errorf("after an unclean reopen: scoped a.x = %q, want 1 (the deleted scope came back)", got)
	}
}

// A scope written again after its deletion is a new scope under the old name: what it
// writes is seen, what was deleted is not, before and after a rebuild.
func TestDeleteScope_ReusedNameIsANewScope(t *testing.T) {
	s, dir, scope := deletedScopeStore(t)
	writeAs(t, s, &scope, "a.y", `"3"`)
	check := func(when string) {
		t.Helper()
		commit, _ := s.GetCurrentCommit()
		doc, err := readStateAt(s, "", commit, &scope)
		if err != nil {
			t.Fatalf("%s: scoped read: %v", when, err)
		}
		if x, y := getString(doc, "a", "x"), getString(doc, "a", "y"); x != "1" || y != "3" {
			t.Errorf("%s: scoped a = {x: %q, y: %q}, want {x: 1, y: 3}", when, x, y)
		}
	}
	check("live")
	s = reopenRebuilt(t, s, dir)
	defer s.Close()
	check("rebuilt")
}
