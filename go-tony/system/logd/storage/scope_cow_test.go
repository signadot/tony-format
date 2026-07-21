package storage

import (
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// mustCommit parses src as a root-rooted patch and commits it to the given scope
// (nil = baseline), returning the commit count.
func mustCommit(t *testing.T, s *Storage, scope *string, src string) int64 {
	t.Helper()
	patch, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	txn, err := s.NewTx(1, scope)
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	p, err := txn.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch}})
	if err != nil {
		t.Fatalf("NewPatcher: %v", err)
	}
	res := p.Commit()
	if !res.Committed {
		t.Fatalf("commit %q: %v", src, res.Error)
	}
	return res.Commit
}

func mustReadScope(t *testing.T, s *Storage, commit int64, scope *string) *ir.Node {
	t.Helper()
	n, err := s.ReadStateAt("", commit, scope)
	if err != nil {
		t.Fatalf("ReadStateAt(scope=%v): %v", scope, err)
	}
	return n
}

// skus returns the sku field of each element of the array at n.<field>.
func skus(n *ir.Node, field string) []string {
	var out []string
	if n == nil || n.Type != ir.ObjectType {
		return out
	}
	var arr *ir.Node
	for i, f := range n.Fields {
		if f.String == field {
			arr = n.Values[i]
			break
		}
	}
	if arr == nil || arr.Type != ir.ArrayType {
		return out
	}
	for _, el := range arr.Values {
		out = append(out, getString(el, "sku"))
	}
	return out
}

func openTestStorage(t *testing.T) *Storage {
	t.Helper()
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// Case 1: a scope write to a leaf is sticky — a LATER baseline write to the same
// leaf is shadowed in the scope. (The original COW isolation bug: scope read
// returned the later baseline value.)
func TestScope_COW_ScopeWinsOverLaterBaseline(t *testing.T) {
	s := openTestStorage(t)
	sc := "s1"

	mustCommit(t, s, nil, `{a: {x: 1}}`)  // baseline a.x=1
	mustCommit(t, s, &sc, `{a: {x: 5}}`)  // scope a.x=5
	c := mustCommit(t, s, nil, `{a: {x: 99}}`) // baseline a.x=99 (LATER than scope)

	if got := getInt(mustReadScope(t, s, c, &sc), "a", "x"); got != 5 {
		t.Errorf("scope a.x = %d, want 5 (scope write must shadow later baseline)", got)
	}
	if got := getInt(mustReadScope(t, s, c, nil), "a", "x"); got != 99 {
		t.Errorf("baseline a.x = %d, want 99", got)
	}
}

// Case 2: for a leaf the scope never wrote, the scope tracks ongoing baseline
// (live overlay, not a frozen branch). The scope's own separate write is kept.
func TestScope_COW_TracksOngoingBaseline(t *testing.T) {
	s := openTestStorage(t)
	sc := "s1"

	mustCommit(t, s, nil, `{a: {x: 1}}`) // baseline a.x=1
	mustCommit(t, s, &sc, `{a: {z: 7}}`) // scope writes a DIFFERENT leaf a.z=7
	c := mustCommit(t, s, nil, `{a: {x: 2}}`) // baseline a.x=2 (later)

	sv := mustReadScope(t, s, c, &sc)
	if got := getInt(sv, "a", "x"); got != 2 {
		t.Errorf("scope a.x = %d, want 2 (ongoing baseline for un-written leaf)", got)
	}
	if got := getInt(sv, "a", "z"); got != 7 {
		t.Errorf("scope a.z = %d, want 7 (scope's own write)", got)
	}
}

// Case 3: a later baseline write at an ANCESTOR merges with the scope's leaf.
func TestScope_COW_AncestorMerge(t *testing.T) {
	s := openTestStorage(t)
	sc := "s1"

	mustCommit(t, s, &sc, `{a: {x: 5}}`)  // scope a.x=5
	c := mustCommit(t, s, nil, `{a: {y: 9}}`) // baseline writes ancestor a={y:9}

	sv := mustReadScope(t, s, c, &sc)
	if got := getInt(sv, "a", "x"); got != 5 {
		t.Errorf("scope a.x = %d, want 5", got)
	}
	if got := getInt(sv, "a", "y"); got != 9 {
		t.Errorf("scope a.y = %d, want 9 (baseline ancestor write flows through)", got)
	}
}

// Case 4: a later baseline write at an ANCESTOR that also sets the scope's leaf is
// still shadowed at that leaf by the scope.
func TestScope_COW_AncestorClobber(t *testing.T) {
	s := openTestStorage(t)
	sc := "s1"

	mustCommit(t, s, &sc, `{a: {x: 5}}`)   // scope a.x=5
	c := mustCommit(t, s, nil, `{a: {x: 99}}`) // baseline ancestor write sets a.x=99

	if got := getInt(mustReadScope(t, s, c, &sc), "a", "x"); got != 5 {
		t.Errorf("scope a.x = %d, want 5 (scope owns the leaf)", got)
	}
}

// Case 5: !key durability. A scope's keyed-array addition survives ongoing baseline
// keyed additions (identity merge), and the scope's addition does not leak to
// baseline. This is why the scope layer must replay ops rather than a materialized
// (resolved) overlay.
func TestScope_COW_KeyDurability(t *testing.T) {
	s := openTestStorage(t)
	sc := "s1"

	mustCommit(t, s, nil, `{items: !key(sku) [{sku: "WIDGET", qty: 1}, {sku: "GADGET", qty: 1}]}`)
	mustCommit(t, s, &sc, `{items: !key(sku) [{sku: "GIZMO", qty: 3}]}`)
	c := mustCommit(t, s, nil, `{items: !key(sku) [{sku: "SPROCKET", qty: 1}]}`)

	scopeSkus := skus(mustReadScope(t, s, c, &sc), "items")
	if !sameSet(scopeSkus, []string{"WIDGET", "GADGET", "SPROCKET", "GIZMO"}) {
		t.Errorf("scope items skus = %v, want {WIDGET,GADGET,SPROCKET,GIZMO} (baseline's new item AND scope's)", scopeSkus)
	}

	baseSkus := skus(mustReadScope(t, s, c, nil), "items")
	if !sameSet(baseSkus, []string{"WIDGET", "GADGET", "SPROCKET"}) {
		t.Errorf("baseline items skus = %v, want {WIDGET,GADGET,SPROCKET} (scope's GIZMO must not leak)", baseSkus)
	}
}

// TestScope_COW_PatchesSurviveCompaction proves the scope's op-log is retained
// through compaction. SwitchDLog auto-creates snapshots (baseline AND scope), and an
// aggressive cutoff drops the underlying patches. Baseline reads survive via the
// baseline snapshot. The scope read, however, deliberately ignores the (unsound)
// scope snapshot and replays the scope's patches — so it can only return the scope's
// write if that patch survived compaction. This is the differential: 5 iff retained.
func TestScope_COW_PatchesSurviveCompaction(t *testing.T) {
	s := openTestStorage(t)
	sc := "s1"

	mustCommit(t, s, nil, `{a: {x: 1}}`)      // baseline a.x=1
	c := mustCommit(t, s, &sc, `{a: {x: 5}}`) // scope a.x=5

	// Move these segments into the inactive log so compaction processes them.
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}

	// Cutoff in the future: every patch is "past cutoff", so the time-based drop
	// would remove ALL patches — baseline and, without retention, scope too.
	cfg := &CompactionConfig{
		Cutoff:       -time.Hour,
		BaseInterval: time.Hour,
		SlotsPerTier: 8,
		Multiplier:   2,
		GracePeriod:  10 * time.Millisecond,
	}
	if err := s.Compact(cfg); err != nil {
		t.Fatalf("Compact: %v", err)
	}

	// Baseline value is preserved by the baseline snapshot (sanity: read still works).
	if got := getInt(mustReadScope(t, s, c, nil), "a", "x"); got != 1 {
		t.Fatalf("baseline a.x = %d, want 1 (from baseline snapshot)", got)
	}
	// The scope's op-log must survive: since the scope read ignores the scope
	// snapshot, returning 5 proves the scope patch was retained through compaction.
	if got := getInt(mustReadScope(t, s, c, &sc), "a", "x"); got != 5 {
		t.Errorf("scope a.x = %d, want 5: scope op-log must survive compaction", got)
	}
}

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, g := range got {
		seen[g]++
	}
	for _, w := range want {
		if seen[w] == 0 {
			return false
		}
		seen[w]--
	}
	return true
}

// TestScope_ReadAbsentPathIsEmpty verifies that reading a path with no index node is
// empty (null), not an "invalid index iterator" error. The scoped read layers the
// scope's patches over the baseline, and an absent baseline must be an empty base.
// Regression: erroring here failed scoped watch init on a not-yet-populated subtree
// (a multi-file connect on verse.local.dir-status-<scope>).
func TestScope_ReadAbsentPathIsEmpty(t *testing.T) {
	s := openTestStorage(t)
	sc := "sc1"
	mustCommit(t, s, nil, `{base: {y: 2}}`)       // baseline data elsewhere
	c := mustCommit(t, s, &sc, `{other: {x: 1}}`) // scope data elsewhere

	// ReadStateAt returns the whole reconstructed doc (the caller narrows); the fix is
	// that an absent path no longer errors, and it resolves to nothing.
	for _, tc := range []struct {
		name  string
		scope *string
	}{{"baseline", nil}, {"scoped", &sc}} {
		got, err := s.ReadStateAt("verse.local.status", c, tc.scope)
		if err != nil {
			t.Errorf("%s: absent-path read errored: %v", tc.name, err)
		}
		if v := getInt(got, "verse", "local", "status", "f0", "v"); v != -1 {
			t.Errorf("%s: absent path unexpectedly resolved to %d", tc.name, v)
		}
	}

	// Once the scope populates the path, the scoped read returns its data — the empty
	// baseline is layered under the scope's patches, not lost.
	c2 := mustCommit(t, s, &sc, `{verse: {local: {status: {f0: {v: 0}}}}}`)
	got, err := s.ReadStateAt("verse.local.status", c2, &sc)
	if err != nil {
		t.Fatalf("scoped read after populate: %v", err)
	}
	if v := getInt(got, "verse", "local", "status", "f0", "v"); v != 0 {
		t.Errorf("scoped read after populate f0.v = %d, want 0", v)
	}
}
