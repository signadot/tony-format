package server

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

func intAt(t *testing.T, n *ir.Node, path string) (int64, bool) {
	t.Helper()
	v, err := n.GetPath(path)
	if err != nil || v == nil || v.Int64 == nil {
		return 0, false
	}
	return *v.Int64, true
}

func TestSetAtFields_CreatesAndPreserves(t *testing.T) {
	root := ir.FromMap(map[string]*ir.Node{
		"x": ir.FromMap(map[string]*ir.Node{"v": ir.FromInt(1)}),
	})
	// Place a value at a fresh nested path; the existing sibling must survive.
	out := setAtFields(root, []string{"a", "b"}, ir.FromMap(map[string]*ir.Node{"v": ir.FromInt(7)}))

	if got, ok := intAt(t, out, "$.x.v"); !ok || got != 1 {
		t.Errorf("sibling x.v: got %d ok=%v, want 1", got, ok)
	}
	if got, ok := intAt(t, out, "$.a.b.v"); !ok || got != 7 {
		t.Errorf("new a.b.v: got %d ok=%v, want 7", got, ok)
	}
}

func TestSetAtFields_ReplacesSubtreeWholesale(t *testing.T) {
	root := ir.FromMap(map[string]*ir.Node{
		"c": ir.FromMap(map[string]*ir.Node{"old": ir.FromInt(0)}),
		"k": ir.FromMap(map[string]*ir.Node{"v": ir.FromInt(1)}),
	})
	// Overlaying c replaces the whole subtree — the stale "old" field is gone.
	out := setAtFields(root, []string{"c"}, ir.FromMap(map[string]*ir.Node{"v": ir.FromInt(9)}))

	if got, ok := intAt(t, out, "$.c.v"); !ok || got != 9 {
		t.Errorf("c.v: got %d ok=%v, want 9", got, ok)
	}
	if v, _ := out.GetPath("$.c.old"); v != nil {
		t.Errorf("stale c.old should be gone, got %v", v)
	}
	if got, ok := intAt(t, out, "$.k.v"); !ok || got != 1 {
		t.Errorf("sibling k.v: got %d ok=%v, want 1", got, ok)
	}
}

func TestSetAtFields_NilAndNonObjectRoot(t *testing.T) {
	out := setAtFields(nil, []string{"a", "b"}, ir.FromInt(5))
	if got, ok := intAt(t, out, "$.a.b"); !ok || got != 5 {
		t.Errorf("from nil root: got %d ok=%v, want 5", got, ok)
	}
	// A non-object node at the target position is replaced by a fresh object.
	out = setAtFields(ir.FromInt(3), []string{"a"}, ir.FromInt(5))
	if got, ok := intAt(t, out, "$.a"); !ok || got != 5 {
		t.Errorf("from scalar root: got %d ok=%v, want 5", got, ok)
	}
}

func TestMountsUnder_StrictlyBelowOnly(t *testing.T) {
	reg := NewMountRegistry()
	for _, p := range []string{"a.b", "a.c.d", "a.e", "x.y"} {
		if err := reg.Register(&MountEntry{Path: p}); err != nil {
			t.Fatalf("register %q: %v", p, err)
		}
	}

	got := map[string]bool{}
	for _, e := range reg.MountsUnder("a") {
		got[e.Path] = true
	}
	// Strictly below "a": its descendants, not a sibling tree.
	want := []string{"a.b", "a.c.d", "a.e"}
	for _, w := range want {
		if !got[w] {
			t.Errorf("MountsUnder(a) missing %q", w)
		}
	}
	if got["x.y"] {
		t.Error("MountsUnder(a) must not include sibling x.y")
	}
	if len(got) != len(want) {
		t.Errorf("MountsUnder(a) size = %d, want %d", len(got), len(want))
	}

	if u := reg.MountsUnder("a.c"); len(u) != 1 || u[0].Path != "a.c.d" {
		t.Errorf("MountsUnder(a.c) = %v, want [a.c.d]", u)
	}
	// A mount has nothing under it: mounts are disjoint.
	if u := reg.MountsUnder("a.b"); len(u) != 0 {
		t.Errorf("MountsUnder(a.b) = %v, want none", u)
	}
	if u := reg.MountsUnder("a.c.d"); len(u) != 0 {
		t.Errorf("MountsUnder(a.c.d) = %v, want none", u)
	}
}

// TestSetReaches: a set crosses a mount when a member is at, under or above it, which the
// pattern answers segment by segment -- a field names the mount's segment or not, a field
// or key wildcard may name any, and an index names none, since a mount path is field-only
// (ghjg0j6nh12krjgandn0).
func TestSetReaches(t *testing.T) {
	for _, tc := range []struct {
		mount, pattern string
		reaches        bool
	}{
		{"verse.x", "verse.demo.*", false},
		{"verse.x", "verse.*", true},         // a member is the mount
		{"verse.x.y", "verse.*", true},       // a member holds the mount
		{"verse", "verse.demo.*", true},      // the members are the mount's
		{"a.b", "*", true},                   // a member holds the mount
		{"a.x.c", "a.*.b", false},            // the members are a.K.b, beside a.x.c
		{"a.list.foo", "a.list[*].x", false}, // an index names no field
		{"runs.m", "runs(*).n", true},        // an element is stored under a field
		{"other.x", "jobs.*", false},
		// A descent reaches whatever is at or under its concrete prefix, and nothing
		// beside it.
		{"a.b", "..name", true},
		{"verse.x", "verse..name", true},
		{"verse.x.y", "verse.x..", true},
		{"verse.x", "verse.demo..name", false},
		{"other.x", "jobs..", false},
	} {
		reg := NewMountRegistry()
		if err := reg.Register(&MountEntry{Path: tc.mount}); err != nil {
			t.Fatalf("register %q: %v", tc.mount, err)
		}
		if got := reg.SetReaches(tc.pattern) != nil; got != tc.reaches {
			t.Errorf("SetReaches(%q) with a mount at %q = %v, want %v", tc.pattern, tc.mount, got, tc.reaches)
		}
	}
}
