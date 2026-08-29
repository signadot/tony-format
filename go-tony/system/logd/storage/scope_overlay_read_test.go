package storage

import "testing"

// The overlay path is an optimisation of readScopedStateAtReplay, which is the
// definition. Where they disagree, the optimisation is wrong.
//
// It disagreed about the write that follows an overlay: at that write's own commit the
// scoped read did not have it, and it appeared as soon as any later commit landed. The
// entry was in the log, correct and marked; the bounded index lookup the overlay path
// makes for "the scope's own patches since the overlay" dropped the segment. See
// index.commitsUpTo -- a commit range is asked in terms of EndCommit and the segments
// are ordered by StartCommit, so the range is not a contiguous run and the walk stopped
// in the middle of one (tmwq9mh6h12kskmxj9n0).
func TestAScopedReadHasTheWriteThatFollowsAnOverlay(t *testing.T) {
	for _, low := range []bool{false, true} {
		s := openTestStorage(t)
		s.EnableScopeOverlay(true) // the subject; it is not the default
		s.EnableLowering(low)
		const scope = "s1"
		sc := scope
		step := func(scoped bool, path, src string, snapshot bool) int64 {
			t.Helper()
			c, err := applyScopeOp(t, s, scopeOp{scoped: scoped, path: path, src: src}, scope)
			if err != nil {
				t.Fatalf("lowering=%v %s <- %s: %v", low, path, src, err)
			}
			if snapshot {
				if err := s.SwitchDLog(); err != nil {
					t.Fatalf("SwitchDLog: %v", err)
				}
			}
			return c
		}
		// Two snapshots, so an overlay is written and is the newest thing the scope
		// has before the write under test.
		step(true, "a", `!delete`, false)
		step(false, "a", `{k1: 1}`, false)
		step(true, "d", `{k1: 2}`, true)
		step(true, "d", `{k0: 3}`, true)
		c := step(true, "d.e", `{k1: 5}`, false)

		if ov := s.latestOverlay(scope, c); ov == nil {
			t.Fatalf("lowering=%v: no overlay was written, so this proves nothing", low)
		}
		want, err := s.readScopedStateAtReplay(c, &sc)
		if err != nil {
			t.Fatalf("lowering=%v replay: %v", low, err)
		}
		got, err := s.readScopedStateAtOverlay(c, &sc)
		if err != nil {
			t.Fatalf("lowering=%v overlay: %v", low, err)
		}
		if withComments(got) != withComments(want) {
			t.Errorf("lowering=%v: the overlay path disagrees with the definition it optimises\n"+
				"  overlay: %s\n  replay:  %s", low, withComments(got), withComments(want))
		}
		// And the write is actually there, rather than the two agreeing on losing it.
		if doc, err := s.ReadStateAt("", c, &sc); err != nil {
			t.Fatalf("lowering=%v read: %v", low, err)
		} else if v, e := doc.GetKPath("d.e.k1"); e != nil || v == nil {
			t.Errorf("lowering=%v: the scope's own write at d.e is missing at its own commit: %s",
				low, withComments(doc))
		}
	}
}
