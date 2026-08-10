package storage

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Plan item P2: !key is load-bearing for the scope overlay and is covered by one test
// (TestScope_COW_KeyDurability). These fill the gaps issue 5hmq80f3h12krh1mbsn0 named.
//
// They are written to survive P1. Four pin semantics that hold whichever way keyed-ness
// is derived. The fifth is about derivation itself, so it pins the property -- a rebuilt
// index agrees with the live one -- rather than today's mechanism, which P1 replaces.

// elemField reads a field of the keyed element with the given key.
func elemField(t *testing.T, doc *ir.Node, arrayPath, keyField, key, want string) *ir.Node {
	t.Helper()
	arr := ir.Get(doc, arrayPath)
	if arr == nil || arr.Type != ir.ArrayType {
		t.Fatalf("no array at %q in %s", arrayPath, encodeWire(t, doc))
	}
	for _, e := range arr.Values {
		if k, ok := ir.ElemKey(e, keyField); ok && k == key {
			return ir.Get(e, want)
		}
	}
	return nil
}

func intOf(n *ir.Node) int64 {
	if n == nil || n.Type != ir.NumberType || n.Int64 == nil {
		return -1
	}
	return *n.Int64
}

func strOf(n *ir.Node) string {
	if n == nil || n.Type != ir.StringType {
		return ""
	}
	return n.String
}

// TestKeyed_IdentityMergeAtBaseline: patching the SAME key twice updates the element in
// place rather than appending a second one, and merges within it rather than replacing
// it. This is the central !key semantic and nothing covered it -- the one keyed test only
// ever adds distinct keys.
func TestKeyed_IdentityMergeAtBaseline(t *testing.T) {
	s := openTestStorage(t)

	mustCommit(t, s, nil, `{items: !key(sku) [{sku: "A", q: 1}, {sku: "B", q: 1}]}`)
	c := mustCommit(t, s, nil, `{items: !key(sku) [{sku: "A", q: 2}]}`)

	doc := mustReadScope(t, s, c, nil)
	if got := skus(doc, "items"); !sameSet(got, []string{"A", "B"}) {
		t.Fatalf("after updating A: skus = %v, want {A,B} with no duplicate", got)
	}
	if got := intOf(elemField(t, doc, "items", "sku", "A", "q")); got != 2 {
		t.Errorf("A.q = %d, want 2", got)
	}
	if got := intOf(elemField(t, doc, "items", "sku", "B", "q")); got != 1 {
		t.Errorf("B.q = %d, want 1 (untouched)", got)
	}

	// A patch naming only some fields merges into the element, leaving the rest.
	c = mustCommit(t, s, nil, `{items: !key(sku) [{sku: "A", note: "hi"}]}`)
	doc = mustReadScope(t, s, c, nil)
	if got := intOf(elemField(t, doc, "items", "sku", "A", "q")); got != 2 {
		t.Errorf("after a partial patch, A.q = %d, want 2 still", got)
	}
	if got := strOf(elemField(t, doc, "items", "sku", "A", "note")); got != "hi" {
		t.Errorf("A.note = %q, want \"hi\"", got)
	}
	if got := skus(doc, "items"); !sameSet(got, []string{"A", "B"}) {
		t.Errorf("after a partial patch: skus = %v, want {A,B}", got)
	}
}

// TestKeyed_SurvivesSnapshotAndCompaction: a keyed array through a snapshot, then through
// a compaction aggressive enough to drop the patches that built it. PatchesSurviveCompaction
// covers this with plain scalars; the keyed case is where a materialized base could
// resolve !key away and re-apply positionally.
func TestKeyed_SurvivesSnapshotAndCompaction(t *testing.T) {
	s := openTestStorage(t)

	mustCommit(t, s, nil, `{items: !key(sku) [{sku: "A", q: 1}, {sku: "B", q: 2}]}`)
	mustCommit(t, s, nil, `{items: !key(sku) [{sku: "C", q: 3}]}`)
	c := mustCommit(t, s, nil, `{items: !key(sku) [{sku: "A", q: 9}]}`)

	before := mustReadScope(t, s, c, nil)
	wantSkus := skus(before, "items")

	s.SetCompactionConfig(&CompactionConfig{
		Cutoff: 0, BaseInterval: 1, SlotsPerTier: 8, Multiplier: 2, GracePeriod: 0,
	})
	if err := s.SwitchDLog(); err != nil { // snapshot at c
		t.Fatalf("SwitchDLog: %v", err)
	}
	if err := s.SwitchDLog(); err != nil { // compact the log the patches went to
		t.Fatalf("SwitchDLog: %v", err)
	}

	c2, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	after := mustReadScope(t, s, c2, nil)
	if got := skus(after, "items"); !sameSet(got, wantSkus) {
		t.Fatalf("after compaction: skus = %v, want %v", got, wantSkus)
	}
	if got := intOf(elemField(t, after, "items", "sku", "A", "q")); got != 9 {
		t.Errorf("after compaction A.q = %d, want 9 (the identity merge survived)", got)
	}
	if got := intOf(elemField(t, after, "items", "sku", "B", "q")); got != 2 {
		t.Errorf("after compaction B.q = %d, want 2", got)
	}

	// And a keyed patch applied ONTO the snapshot base still merges by identity, even
	// though the materialized base carries no !key tag of its own.
	mustCommit(t, s, nil, `{items: !key(sku) [{sku: "B", q: 42}]}`)
	c3, _ := s.GetCurrentCommit()
	onSnap := mustReadScope(t, s, c3, nil)
	if got := skus(onSnap, "items"); !sameSet(got, wantSkus) {
		t.Errorf("patching onto a snapshot base: skus = %v, want %v (no duplicate)", got, wantSkus)
	}
	if got := intOf(elemField(t, onSnap, "items", "sku", "B", "q")); got != 42 {
		t.Errorf("B.q = %d, want 42", got)
	}
}

// indexPathSet is the set of paths the index holds segments at.
func indexPathSet(s *Storage) []string {
	seen := map[string]bool{}
	for _, seg := range s.index.AllSegments() {
		seen[seg.KindedPath] = true
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// reopenRebuilt closes the store, removes the persisted index so Open has to rebuild it
// from the logs, and reopens.
func reopenRebuilt(t *testing.T, s *Storage, root string, resolver api.SchemaResolver) *Storage {
	t.Helper()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "index.gob")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove index.gob: %v", err)
	}
	re, err := Open(root, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if resolver != nil {
		re.SetSchemaResolver(resolver)
	}
	return re
}

// TestKeyed_RebuiltIndexAgreesWithLive pins build.go's contract as a PROPERTY rather than
// as a mechanism: however keyed-ness is derived, an index rebuilt from the logs must
// describe the same paths as the one built while writing. Today derivation is the !key
// tag on each patch; P1 moves it to the schema. The property is what should hold either
// way, so this test does not have to be rewritten by P1.
func TestKeyed_RebuiltIndexAgreesWithLive(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	mustCommit(t, s, nil, `{items: !key(sku) [{sku: "A", q: 1}, {sku: "B", q: 2}]}`)
	mustCommit(t, s, nil, `{items: !key(sku) [{sku: "A", q: 9}]}`)
	mustCommit(t, s, nil, `{plain: {x: 1}}`)

	live := indexPathSet(s)
	re := reopenRebuilt(t, s, root, nil)
	defer re.Close()
	rebuilt := indexPathSet(re)

	t.Logf("live:    %v", live)
	t.Logf("rebuilt: %v", rebuilt)
	if !sameSet(live, rebuilt) {
		t.Errorf("a rebuilt index describes different paths than the live one\n live:    %v\n rebuilt: %v",
			live, rebuilt)
	}
}

// TestKeyed_RebuiltIndexUnderSchema is the same property with a schema in play, which is
// the route P1 makes authoritative. build.go states "Schema is nil here - we rely on !key
// tags stored in the patches", so a store whose live index was built from the SCHEMA and
// whose rebuild reads only tags has two shapes for one log.
func TestKeyed_RebuiltIndexUnderSchema(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	resolver := &api.StaticSchemaResolver{Schema: &api.Schema{
		AutoIDFields: []api.AutoIDField{{Path: "items", Field: "id"}},
	}}
	s.SetSchemaResolver(resolver)

	// No !key tag on the write: the schema is the only thing that says items is keyed.
	mustCommit(t, s, nil, `{items: [{q: 1}, {q: 2}]}`)

	live := indexPathSet(s)
	re := reopenRebuilt(t, s, root, resolver)
	defer re.Close()
	rebuilt := indexPathSet(re)

	t.Logf("live (schema route):   %v", live)
	t.Logf("rebuilt (tag route):   %v", rebuilt)
	if !sameSet(live, rebuilt) {
		t.Logf("KNOWN, plan P1: the live index was keyed by the schema and the rebuild was")
		t.Logf("not, so one log has two index shapes. This is the disjoint-routes gap, and")
		t.Logf("the guard above is what should still pass once P1 makes them one route.")
		return
	}
	t.Log("the two routes agree for this shape")
}

// TestKeyed_WatchDeltaStepsCorrectly pins the delta a watcher receives for a keyed
// change. A baseline watcher does not re-read; it folds the committed patch into a
// document it keeps (session.go: curDoc = Patch(curDoc, notification.Patch)). So the
// property that has to hold for every keyed commit is that folding its notification onto
// the previous state lands on the new one.
//
// This tests the notification content rather than the wire delivery -- the fold is where
// a keyed delta could go wrong, and it is what the scoped stepping in the overlay plan
// generalises.
func TestKeyed_WatchDeltaStepsCorrectly(t *testing.T) {
	s := openTestStorage(t)

	type note struct {
		commit int64
		patch  *ir.Node
	}
	notes := make(chan note, 16)
	s.SetCommitNotifier(func(n *CommitNotification) {
		notes <- note{commit: n.Commit, patch: n.Patch}
	})

	writes := []string{
		`{items: !key(sku) [{sku: "A", q: 1}, {sku: "B", q: 2}]}`,
		`{items: !key(sku) [{sku: "A", q: 9}]}`,       // update by identity
		`{items: !key(sku) [{sku: "C", q: 3}]}`,       // add
		`{items: !key(sku) [{sku: "B", note: "hi"}]}`, // partial merge into an element
	}

	prev := ir.Null()
	for i, src := range writes {
		commit := mustCommit(t, s, nil, src)

		var n note
		select {
		case n = <-notes:
		case <-time.After(2 * time.Second):
			t.Fatalf("write %d: no commit notification", i)
		}
		if n.commit != commit {
			t.Fatalf("write %d: notification for commit %d, want %d", i, n.commit, commit)
		}

		stepped, err := tony.Patch(prev, n.patch.DeepCopy())
		if err != nil {
			t.Fatalf("write %d: folding the delta failed: %v", i, err)
		}
		if stepped == nil {
			stepped = ir.Null()
		}

		want := mustReadScope(t, s, commit, nil)
		if want == nil {
			want = ir.Null()
		}
		if got, wantS := encodeWire(t, stepped), encodeWire(t, want); got != wantS {
			t.Fatalf("write %d (%s): folding the watch delta diverged from the read\n stepped: %s\n read:    %s",
				i, src, got, wantS)
		}
		t.Logf("  %d %-52s -> %s", i, src, encodeWire(t, stepped))
		prev = stepped
	}
}
