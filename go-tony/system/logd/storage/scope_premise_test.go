package storage

import (
	"sort"
	"strings"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"testing"
)

// The planned scope overlay (issue 5hmq80f3h12krh1mbsn0) materializes a scope's writes
// as "the latest write per index path, plus tombstones for !delete". These record the
// two shapes that premise has to survive, both of which the scope layer handles today
// only because it replays the patches in order.

// TestScope_DeleteIsSticky: a scope deletes a key, then BASELINE writes it again. The
// scope must keep seeing it gone -- the delete is the scope taking ownership of that
// path, not a one-off edit -- which is what a tombstone in an overlay would have to
// reproduce.
func TestScope_DeleteIsSticky(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	scope := "s1"

	scalingCommit(t, s, nil, `{a: 1, b: 2, c: 3}`, nil)
	showDoc(t, s, nil, "baseline")

	// A delete is written AT the path, the way libctl does it (watch_stepping_test:
	// {"doc.extra", `!delete`}); `{b: !delete}` inline does not parse -- the op has no
	// child there.
	commitAt(t, s, &scope, "b", `!delete`)
	afterDelete := showDoc(t, s, &scope, "scoped after !delete b")

	scalingCommit(t, s, nil, `{b: 4}`, nil)
	showDoc(t, s, nil, "baseline after b=4")
	afterBaseline := showDoc(t, s, &scope, "scoped after baseline b=4")

	if strings.Contains(afterDelete, "b:") {
		t.Errorf("scope still sees b right after deleting it: %s", afterDelete)
	}
	if strings.Contains(afterBaseline, "b:") {
		t.Errorf("baseline's later write to b reappeared in the scope: %s\n"+
			"want the delete to be sticky, i.e. {a: 1 c: 3}", afterBaseline)
	}
}

// TestScope_OverwriteContainerWithScalar: a scope writes a container, then overwrites it
// with a scalar. Both writes are "absolute" in the sense the overlay plan means, but the
// LATER one has to erase the leaf paths the earlier one established -- so an overlay
// keyed by leaf path cannot just keep the latest entry per path, it has to know that a
// write at an ancestor kills every descendant entry beneath it.
func TestScope_OverwriteContainerWithScalar(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	scope := "s1"

	scalingCommit(t, s, nil, `{keep: 0}`, nil)

	scalingCommit(t, s, &scope, `{a: {x: 1, y: 2}}`, nil)
	showDoc(t, s, &scope, "scoped after a={x:1,y:2}")
	t.Logf("  index paths for the scope now: %v", scopeIndexPaths(s, scope))

	scalingCommit(t, s, &scope, `{a: "x"}`, nil)
	after := showDoc(t, s, &scope, `scoped after a="x"`)
	t.Logf("  index paths for the scope now: %v", scopeIndexPaths(s, scope))

	if strings.Contains(after, "x: 1") || strings.Contains(after, "y: 2") {
		t.Errorf("the scalar overwrite did not erase the container it replaced: %s", after)
	}
}

// commitAt commits a patch rooted at a path rather than at the document root.
func commitAt(t *testing.T, s *Storage, scope *string, path, body string) int64 {
	t.Helper()
	data, err := parse.Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse(%q): %v", body, err)
	}
	txn, err := s.NewTx(1, scope)
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	p, err := txn.NewPatcher(&api.Patch{PathData: api.PathData{Path: path, Data: data}})
	if err != nil {
		t.Fatalf("NewPatcher: %v", err)
	}
	res := p.Commit()
	if !res.Committed {
		t.Fatalf("commit at %q not committed: %v", path, res.Error)
	}
	return res.Commit
}

// scopeIndexPaths lists the paths this scope has segments at.
func scopeIndexPaths(s *Storage, scope string) []string {
	seen := map[string]bool{}
	for _, seg := range s.index.AllSegments() {
		if seg.ScopeID == nil || *seg.ScopeID != scope {
			continue
		}
		p := seg.KindedPath
		if p == "" {
			p = "<root>"
		}
		seen[p] = true
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
