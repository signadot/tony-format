package storage

import (
	"sort"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// Can a scoped view be STEPPED? head.go says no, and says why: a scope's writes apply
// last and shadow stickily, so folding a baseline patch into a materialized scoped
// document lets baseline overwrite a leaf the scope owns. Plan steps 7-8 claim an
// explicit ownership set fixes that. This is that claim against the replay oracle.
//
// The rule under test is apply-then-REASSERT, not mask-then-apply. Masking the baseline
// patch is wrong for a case that actually arises: if the scope owns a.x and baseline
// REPLACES a wholesale, replay wipes a's other fields and keeps the scope's x. Dropping
// baseline's patch would keep the other fields too.
//
//	on a baseline commit:  stepped = Patch(stepped, p) ; stepped = Patch(stepped, overlay)
//	on a scope commit:     stepped = Patch(stepped, p)
//	after either:          overlay = unconditional(Diff(base, stepped))
//
// Nothing here is proportional to the number of accumulated writes, which is the claim.

type stepState struct {
	base    *ir.Node // stepped baseline document
	scoped  *ir.Node // stepped scoped document
	overlay *ir.Node // the scope's ownership vs baseline, unconditional
	keys    map[string]string

	// owned maps each kpath the scope has written to the value it wrote there, captured
	// at that write. ownedOrder keeps insertion order so the assertion is deterministic.
	owned      map[string]*ir.Node
	ownedOrder []string

	// ownedKeyed is ownership inside a keyed list, per ELEMENT: array path -> key value
	// -> the element. Owning the array path instead makes the scope re-assert the whole
	// list and wipe every element baseline has added since -- the same failure the old
	// materialized scope snapshots had. The index already records the finer path
	// (items("G")); this mirrors it.
	ownedKeyed map[string]map[string]*ir.Node
}

// refresh recomputes the overlay. Both states are key-annotated first (plan R2/3.5):
// stored state is op-free, so without this diffArray goes positional and re-asserting a
// positional diff every event DUPLICATES the scope's elements rather than merging them.
func (st *stepState) refresh(t *testing.T) {
	t.Helper()
	st.overlay = unconditional(tony.Diff(
		annotateKeys(st.base.Clone(), st.keys),
		annotateKeys(st.scoped.Clone(), st.keys),
	))
}

// ownedAssert re-states the scope's value at every path it has written, which a minimal
// diff omits when the value coincides with baseline's (plan R3). The paths come from what
// the scope wrote -- in production, from the index.
//
// The values are the ones captured WHEN THE SCOPE WROTE THEM, not re-read from the live
// document. Re-reading is wrong twice over, and both showed up here: after a baseline
// delta has been folded in, the live value at an owned path is baseline's, so the
// assertion would hand baseline the path it was meant to defend; and a path the scope
// DELETED reads back as whatever baseline has put there since, so the assertion would
// undo its own tombstone.
func (st *stepState) ownedAssert(t *testing.T) *ir.Node {
	t.Helper()
	var out *ir.Node
	for _, p := range st.ownedOrder {
		v := st.owned[p]
		if v == nil {
			continue // owned but absent: the overlay's tombstone is what holds it
		}
		rooted, err := tx.RootPatchAt(p, v.Clone())
		if err != nil {
			t.Fatalf("RootPatchAt(%q): %v", p, err)
		}
		if out == nil {
			out = rooted
			continue
		}
		if out, err = tony.Patch(out, rooted); err != nil {
			t.Fatalf("merge owned assertion: %v", err)
		}
	}
	return out
}

// leafPaths lists the object leaf paths a patch touches, which is what indexPatchRec
// records for it. A keyed array is skipped: its ownership is per element, recorded
// separately (see ownedKeyed).
func leafPaths(n *ir.Node, prefix string, keys map[string]string, dst []string) []string {
	if n == nil {
		return dst
	}
	if _, keyed := keys[prefix]; keyed && n.Type == ir.ArrayType {
		return dst
	}
	if n.Type != ir.ObjectType || len(n.Fields) == 0 {
		if prefix != "" {
			dst = append(dst, prefix)
		}
		return dst
	}
	for i, f := range n.Fields {
		p := f.String
		if prefix != "" {
			p = prefix + "." + f.String
		}
		dst = leafPaths(n.Values[i], p, keys, dst)
	}
	return dst
}

// recordKeyed captures, per keyed array the patch touches, the elements the scope now
// holds for the keys that patch named.
func (st *stepState) recordKeyed(t *testing.T, patch *ir.Node) {
	t.Helper()
	for path, field := range st.keys {
		pv, err := patch.GetPath("$." + path)
		if err != nil || pv == nil || pv.Type != ir.ArrayType {
			continue
		}
		live, err := st.scoped.GetPath("$." + path)
		if err != nil || live == nil || live.Type != ir.ArrayType {
			continue
		}
		for _, elem := range pv.Values {
			key, ok := ir.ElemKey(elem, field)
			if !ok {
				continue
			}
			for _, cur := range live.Values {
				if k, ok := ir.ElemKey(cur, field); ok && k == key {
					if st.ownedKeyed == nil {
						st.ownedKeyed = map[string]map[string]*ir.Node{}
					}
					if st.ownedKeyed[path] == nil {
						st.ownedKeyed[path] = map[string]*ir.Node{}
					}
					st.ownedKeyed[path][key] = cur.Clone()
					break
				}
			}
		}
	}
}

// keyedAssert re-states the scope's OWN elements, by key, leaving every other element of
// the list to baseline.
func (st *stepState) keyedAssert(t *testing.T) *ir.Node {
	t.Helper()
	var out *ir.Node
	for path, byKey := range st.ownedKeyed {
		field := st.keys[path]
		elems := make([]*ir.Node, 0, len(byKey))
		for _, k := range sortedKeys(byKey) {
			elems = append(elems, byKey[k].Clone())
		}
		list := ir.FromSlice(elems)
		list.Tag = ir.TagCompose("!key", []string{field}, "")
		rooted, err := tx.RootPatchAt(path, list)
		if err != nil {
			t.Fatalf("RootPatchAt(%q): %v", path, err)
		}
		if out == nil {
			out = rooted
			continue
		}
		if out, err = tony.Patch(out, rooted); err != nil {
			t.Fatalf("merge keyed assertion: %v", err)
		}
	}
	return out
}

func sortedKeys(m map[string]*ir.Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (st *stepState) stepBaseline(t *testing.T, patch *ir.Node) {
	t.Helper()
	var err error
	if st.base, err = tony.Patch(st.base, patch.Clone()); err != nil {
		t.Fatalf("step base: %v", err)
	}
	if st.base == nil {
		st.base = ir.Null()
	}
	if st.scoped, err = tony.Patch(st.scoped, patch.Clone()); err != nil {
		t.Fatalf("fold baseline delta into scoped: %v", err)
	}
	if st.scoped == nil {
		st.scoped = ir.Null()
	}
	// Re-assert what the scope owns over whatever the baseline delta just did: the
	// overlay carries structure and tombstones, the owned assertions carry values the
	// minimal diff left out.
	for _, layer := range []*ir.Node{st.overlay, st.ownedAssert(t), st.keyedAssert(t)} {
		if layer == nil {
			continue
		}
		if st.scoped, err = tony.Patch(st.scoped, layer.Clone()); err != nil {
			t.Fatalf("reassert: %v", err)
		}
		if st.scoped == nil {
			st.scoped = ir.Null()
		}
	}
	st.refresh(t)
}

func (st *stepState) stepScope(t *testing.T, patch *ir.Node) {
	t.Helper()
	var err error
	paths := leafPaths(patch, "", st.keys, nil)
	if st.scoped, err = tony.Patch(st.scoped, patch.Clone()); err != nil {
		t.Fatalf("step scoped: %v", err)
	}
	if st.scoped == nil {
		st.scoped = ir.Null()
	}
	st.recordKeyed(t, patch)
	// Capture what the scope now holds at each path this write touched.
	for _, p := range paths {
		if st.owned == nil {
			st.owned = map[string]*ir.Node{}
		}
		if _, seen := st.owned[p]; !seen {
			st.ownedOrder = append(st.ownedOrder, p)
		}
		v, err := st.scoped.GetPath("$." + p)
		if err != nil {
			v = nil
		}
		st.owned[p] = v
	}
	st.refresh(t)
}

type wr struct {
	scoped bool
	body   string
	note   string
	path   string // patch rooted here rather than at the document root
}

// runStepping commits each write, steps alongside, and compares against the read path at
// every commit. Returns the number of commits that matched.
func runStepping(t *testing.T, writes []wr) { runSteppingKeyed(t, writes, nil) }

func runSteppingKeyed(t *testing.T, writes []wr, keys map[string]string) {
	t.Helper()
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	scope := "s1"

	st := &stepState{base: ir.Null(), scoped: ir.Null(), keys: keys}

	for i, w := range writes {
		var sc *string
		if w.scoped {
			sc = &scope
		}
		var patch *ir.Node
		if w.path == "" {
			scalingCommit(t, s, sc, w.body, nil)
			patch = mustParseBody(t, w.body)
		} else {
			commitAt(t, s, sc, w.path, w.body)
			rooted, err := tx.RootPatchAt(w.path, mustParseBody(t, w.body))
			if err != nil {
				t.Fatalf("RootPatchAt(%q): %v", w.path, err)
			}
			patch = rooted
		}

		if w.scoped {
			st.stepScope(t, patch)
		} else {
			st.stepBaseline(t, patch)
		}

		commit, err := s.GetCurrentCommit()
		if err != nil {
			t.Fatalf("GetCurrentCommit: %v", err)
		}
		want, err := s.ReadStateAt("", commit, &scope)
		if err != nil {
			t.Fatalf("oracle read: %v", err)
		}
		if want == nil {
			want = ir.Null()
		}

		gotS, wantS := encodeWire(t, st.scoped), encodeWire(t, want)
		kind := "baseline"
		if w.scoped {
			kind = "scope   "
		}
		if gotS != wantS {
			// Before calling it a divergence, ask whether the two differ only in where
			// keyed elements sit. Replay appends the scope's elements LAST, because scope
			// patches apply after every baseline patch; stepping appends them when they
			// were written. A keyed list is identified by key, not position, and Diff
			// under the same annotation reports no difference for a reordering -- so this
			// is a real behavioural note, not an equality failure.
			if keys != nil && tony.Diff(annotateKeys(st.scoped.Clone(), keys),
				annotateKeys(want.Clone(), keys)) == nil {
				t.Logf("  %2d %s %-46s -> ORDER ONLY\n      stepped: %s\n      replay:  %s",
					i, kind, w.body, gotS, wantS)
				continue
			}
			t.Errorf("step %d (%s %s) DIVERGED%s\n  stepped: %s\n  replay:  %s\n  overlay: %s",
				i, kind, w.body, noteOf(w), gotS, wantS, nodeOrNil(t, st.overlay))
			return
		}
		label := w.body
		if w.path != "" {
			label = w.path + " := " + w.body
		}
		t.Logf("  %2d %s %-46s -> %s", i, kind, label, gotS)
	}
}

func noteOf(w wr) string {
	if w.note == "" {
		return ""
	}
	return "  [" + w.note + "]"
}

func mustParseBody(t *testing.T, body string) *ir.Node {
	t.Helper()
	n, err := parseBody(body)
	if err != nil {
		t.Fatalf("parse %q: %v", body, err)
	}
	return n
}

// TestScopedStepping_Plain covers the shapes head.go's objection is about.
func TestScopedStepping_Plain(t *testing.T) {
	runStepping(t, []wr{
		{scoped: false, body: `{a: {x: 1, y: 2}, keep: 0}`, note: "baseline seeds"},
		{scoped: true, body: `{a: {x: 5}}`, note: "scope takes a.x"},
		{scoped: false, body: `{a: {y: 9}}`, note: "baseline elsewhere -- must flow through"},
		{scoped: false, body: `{a: {x: 99}}`, note: "baseline at an OWNED leaf -- must be shadowed"},
		{scoped: true, body: `{b: "scope only"}`, note: "scope adds a new path"},
		{scoped: false, body: `{b: "baseline tries"}`, note: "baseline at a scope-only path"},
		{scoped: false, body: `{c: 1}`, note: "untouched path"},
		{scoped: true, body: `{a: {y: 77}}`, note: "scope takes a.y too"},
		{scoped: false, body: `{a: {y: 100}}`, note: "baseline at newly owned leaf"},
	})
}

// TestScopedStepping_AncestorReplace is the case that rules out masking: baseline
// replaces an ancestor of a path the scope owns.
func TestScopedStepping_AncestorReplace(t *testing.T) {
	runStepping(t, []wr{
		{scoped: false, body: `{a: {x: 1, y: 2}, keep: 0}`, note: ""},
		{scoped: true, body: `{a: {x: 5}}`, note: "scope owns a.x"},
		{scoped: false, body: `{a: "scalar"}`, note: "baseline REPLACES a -- y goes, x is the scope's"},
		{scoped: false, body: `{a: {z: 3}}`, note: "baseline rebuilds a"},
	})
}

func parseBody(body string) (*ir.Node, error) {
	return parse.Parse([]byte(body))
}

// TestScopedStepping_Deletes: a tombstone is ownership too. The scope removes a key and
// baseline writes it back; and baseline removes a key the scope holds.
func TestScopedStepping_Deletes(t *testing.T) {
	runStepping(t, []wr{
		{scoped: false, body: `{a: 1, b: 2, c: 3}`, note: "baseline seeds"},
		{scoped: true, body: `!delete`, path: "b", note: "scope deletes b"},
		{scoped: false, body: `{b: 4}`, note: "baseline writes b back -- delete is sticky"},
		{scoped: true, body: `{d: 7}`, note: "scope adds d"},
		{scoped: false, body: `!delete`, path: "c", note: "baseline deletes c -- flows through"},
	})
}

// TestScopedStepping_Keyed: identity-merged lists on both sides. The overlay is built by
// Diff over op-free state, so without the annotation of plan section 3.5 this is where a
// positional diff would show.
func TestScopedStepping_Keyed(t *testing.T) {
	runSteppingKeyed(t, []wr{
		{scoped: false, body: `{items: !key(sku) [{sku: "W", q: 1}]}`, note: "baseline seeds"},
		{scoped: true, body: `{items: !key(sku) [{sku: "G", q: 3}]}`, note: "scope adds G"},
		{scoped: false, body: `{items: !key(sku) [{sku: "S", q: 1}]}`, note: "baseline adds S"},
		{scoped: false, body: `{items: !key(sku) [{sku: "W", q: 9}]}`, note: "baseline updates W"},
	}, map[string]string{"items": "sku"})
}

// TestScopedStepping_CoincidentValue: the scope writes what baseline already holds, so a
// minimal Diff records no ownership (plan R3). Stepping inherits the same gap.
func TestScopedStepping_CoincidentValue(t *testing.T) {
	runStepping(t, []wr{
		{scoped: false, body: `{a: {x: 5}}`, note: "baseline seeds"},
		{scoped: true, body: `{a: {x: 5}}`, note: "scope writes the SAME value -- still owns it"},
		{scoped: false, body: `{a: {x: 99}}`, note: "baseline moves it -- scope must keep 5"},
	})
}
