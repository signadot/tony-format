package storage

import (
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
)

// The overlay proposed in docs/scope_overlay_plan.md is Diff(baseline, scoped) taken at
// compaction time. These check that construction against the COW invariants, using the
// real read path for both states and tony.Diff/Patch for the overlay, so a failure is a
// statement about the design rather than about a prototype.
//
// Each case: build the history, take the overlay at T, then advance BASELINE past T and
// compare "baseline@T2 + overlay" against what the replay layer says at T2. They agree
// except where the freeze is intended -- which is what the cases name.

func overlayAt(t *testing.T, s *Storage, scope string) *ir.Node {
	t.Helper()
	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	base, err := s.ReadStateAt("", commit, nil)
	if err != nil {
		t.Fatalf("baseline read: %v", err)
	}
	scoped, err := s.ReadStateAt("", commit, &scope)
	if err != nil {
		t.Fatalf("scoped read: %v", err)
	}
	if base == nil {
		base = ir.Null()
	}
	if scoped == nil {
		scoped = ir.Null()
	}
	return tony.Diff(base, scoped)
}

// applyOverlaySoft is applyOverlay but returns the apply error instead of failing, so a
// case known to need plan item P3 can record it.
func applyOverlaySoft(t *testing.T, s *Storage, overlay *ir.Node) (string, error) {
	t.Helper()
	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	base, err := s.ReadStateAt("", commit, nil)
	if err != nil {
		t.Fatalf("baseline read: %v", err)
	}
	if base == nil {
		base = ir.Null()
	}
	if overlay == nil {
		return encodeWire(t, base), nil
	}
	got, err := tony.Patch(base, overlay)
	if err != nil {
		return "<apply failed>", err
	}
	if got == nil {
		return "<empty>", nil
	}
	return encodeWire(t, got), nil
}

// applyOverlay is what a read would do after compaction: baseline at the read commit,
// with the overlay applied on top.
func applyOverlay(t *testing.T, s *Storage, overlay *ir.Node) string {
	t.Helper()
	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	base, err := s.ReadStateAt("", commit, nil)
	if err != nil {
		t.Fatalf("baseline read: %v", err)
	}
	if base == nil {
		base = ir.Null()
	}
	if overlay == nil {
		return encodeWire(t, base)
	}
	got, err := tony.Patch(base, overlay)
	if err != nil {
		t.Fatalf("apply overlay: %v", err)
	}
	if got == nil {
		return "<empty>"
	}
	return encodeWire(t, got)
}

func TestOverlayDiff_Invariants(t *testing.T) {
	scope := "s1"

	type step struct {
		scoped bool
		body   string
		path   string // "" = root
	}
	cases := []struct {
		name       string
		before     []step // history up to the overlay point
		afterBase  string // a BASELINE write after the overlay is taken
		wantSame   bool   // overlay result should equal the replay result
		wantReason string
		needsP3    string // non-empty: known to diverge until Diff is restricted (plan P3)
	}{
		{
			name:      "scope owns a leaf against a later baseline write",
			before:    []step{{false, `{a: {x: 1}}`, ""}, {true, `{a: {x: 5}}`, ""}},
			afterBase: `{a: {x: 99}}`,
			wantSame:  true,
			needsP3: "Diff emits !replace{from,to}, a CHECKED replace: applying it over a " +
				"baseline that has since moved fails the from: check outright. An overlay " +
				"needs an unconditional set.",
		},
		{
			name:       "baseline changes elsewhere still flow through",
			before:     []step{{false, `{a: {x: 1}}`, ""}, {true, `{a: {x: 5}}`, ""}},
			afterBase:  `{a: {y: 9}}`,
			wantSame:   true,
			wantReason: "the overlay is minimal, so it does not own a.y",
		},
		{
			name:      "keyed addition survives a later baseline keyed addition",
			before:    []step{{false, `{items: !key(sku) [{sku: "W", q: 1}]}`, ""}, {true, `{items: !key(sku) [{sku: "G", q: 3}]}`, ""}},
			afterBase: `{items: !key(sku) [{sku: "S", q: 1}]}`,
			wantSame:  true,
			needsP3: "diffArray takes its keyed branch only when BOTH sides carry !key(f), " +
				"and materialized state never does -- so the overlay came out as a " +
				"POSITIONAL !arraydiff and lands the scope's element by index. Diff needs " +
				"an explicit key source.",
		},
		{
			name:       "container replaced by a scalar stays replaced",
			before:     []step{{false, `{keep: 0}`, ""}, {true, `{a: {x: 1, y: 2}}`, ""}, {true, `{a: "scalar"}`, ""}},
			afterBase:  `{a: {z: 3}}`,
			wantSame:   true,
			wantReason: "diffing final states leaves no stale a.x/a.y to resurrect",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Open(t.TempDir(), nil)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer s.Close()

			for _, st := range tc.before {
				var sc *string
				if st.scoped {
					sc = &scope
				}
				if st.path == "" {
					scalingCommit(t, s, sc, st.body, nil)
				} else {
					commitAt(t, s, sc, st.path, st.body)
				}
			}

			overlay := overlayAt(t, s, scope)
			t.Logf("  overlay: %s", nodeOrNil(t, overlay))

			scalingCommit(t, s, nil, tc.afterBase, nil)

			viaOverlay, applyErr := applyOverlaySoft(t, s, overlay)
			viaReplay := showDocQuiet(t, s, &scope)
			t.Logf("  via overlay: %s", viaOverlay)
			t.Logf("  via replay:  %s", viaReplay)

			if tc.needsP3 != "" {
				if applyErr == nil && viaOverlay == viaReplay {
					t.Errorf("case is marked as needing P3 but already agrees -- update the plan")
				} else {
					t.Logf("  KNOWN, needs P3: %s", tc.needsP3)
				}
				return
			}
			if applyErr != nil {
				t.Fatalf("applying the overlay failed: %v", applyErr)
			}
			if tc.wantSame && viaOverlay != viaReplay {
				t.Errorf("overlay and replay disagree (%s)\n  overlay: %s\n  replay:  %s",
					tc.wantReason, viaOverlay, viaReplay)
			}
		})
	}
}

// TestOverlayDiff_CoincidentValueLosesOwnership is the one place the construction is NOT
// equivalent to replay, and it is not exotic: a controller that reconciles by writing the
// value it already sees does it on every pass.
//
// The scope writes the value baseline already holds. Under replay the scope owns that
// path -- its patch applies last -- so a later baseline change is shadowed. Under a
// Diff-derived overlay the two states agree at that path, so nothing is recorded and the
// later baseline change flows through.
//
// The fix is in the plan: the scope's INDEX PATHS are the ownership set, so an owned path
// gets an entry even when the value coincides.
func TestOverlayDiff_CoincidentValueLosesOwnership(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	scope := "s1"

	scalingCommit(t, s, nil, `{a: {x: 5}}`, nil)
	scalingCommit(t, s, &scope, `{a: {x: 5}}`, nil) // same value the baseline holds

	overlay := overlayAt(t, s, scope)
	t.Logf("overlay (scope wrote the value baseline already had): %s", nodeOrNil(t, overlay))

	scalingCommit(t, s, nil, `{a: {x: 99}}`, nil)

	viaOverlay := applyOverlay(t, s, overlay)
	viaReplay := showDocQuiet(t, s, &scope)
	t.Logf("via overlay: %s", viaOverlay)
	t.Logf("via replay:  %s", viaReplay)
	if viaOverlay == viaReplay {
		t.Logf("they agree -- the ownership gap does not appear for this shape")
		return
	}
	t.Logf("DIVERGENCE as predicted: replay keeps the scope's ownership of a.x, the")
	t.Logf("minimal diff does not record it. The overlay needs the index's owned paths.")
}

func nodeOrNil(t *testing.T, n *ir.Node) string {
	t.Helper()
	if n == nil {
		return "<no difference>"
	}
	return encodeWire(t, n)
}
