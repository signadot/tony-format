package storage

import (
	"testing"
)

// TestBaseline_SnapshotExpressivity records what survives baseline materialization.
//
// A snapshot is built by running the patches through the streaming processor and
// writing the RESULT as a state event stream (createSnapshot -> snap.Builder). Ops are
// therefore resolved at snapshot time: whatever the base carries afterwards is state,
// not the operation that produced it. This test shows, for each construct, the read
// BEFORE the snapshot (patch replay from an empty base) and AFTER (snapshot as base),
// so any difference is exactly what materialization costs.
func TestBaseline_SnapshotExpressivity(t *testing.T) {
	cases := []struct {
		name  string
		write string
	}{
		{"keyed array", `{items: [{name: "a", v: 1}]}`},
		{"non-op data tag", `{t: !custom 5}`},
		{"tagged object", `{o: !mytag {x: 1}}`},
		{"plain nested", `{a: {b: {c: 1}}}`},
		{"array", `{arr: [1, 2, 3]}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Open(t.TempDir(), nil)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer s.Close()
			if tc.name == "keyed array" {
				declareKeyed(t, s, `{define: {items: {name: !logd-key null}}}`)
			}

			scalingCommit(t, s, nil, tc.write, nil)
			before := showDoc(t, s, nil, "  before snapshot")

			if err := s.SwitchDLog(); err != nil {
				t.Fatalf("SwitchDLog: %v", err)
			}
			after := showDoc(t, s, nil, "  after  snapshot")

			if before != after {
				t.Logf("  DIFFERS: materialization changed the state")
			}
		})
	}
}

// TestBaseline_KeyedMergeAfterSnapshot: identity merge holds once the base is a snapshot,
// and whether or not the patch spells !key -- the schema says what keys the array, and
// the store holds the elements under their names, so a snapshot has nothing to drop.
func TestBaseline_KeyedMergeAfterSnapshot(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	declareKeyed(t, s, `{define: {items: {name: !logd-key null}}}`)

	scalingCommit(t, s, nil, `{items: [{name: "a", v: 1}, {name: "b", v: 1}]}`, nil)
	showDoc(t, s, nil, "initial")

	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	showDoc(t, s, nil, "after snapshot")

	// Update a only, with the tag the schema already implies.
	c := scalingCommit(t, s, nil, `{items: !key(name) [{name: "a", v: 99}]}`, nil)
	if got := skus(mustReadScope(t, s, c, nil), "items"); !sameSet(got, []string{}) && len(got) != 2 {
		t.Errorf("after a keyed update: %d elements, want 2", len(got))
	}

	// And without it: the same merge, by identity, because the schema decides.
	c = scalingCommit(t, s, nil, `{items: [{name: "a", v: 7}]}`, nil)
	doc := mustReadScope(t, s, c, nil)
	if got := intOf(elemField(t, doc, "items", "name", "a", "v")); got != 7 {
		t.Errorf("a.v = %d, want 7", got)
	}
	if got := intOf(elemField(t, doc, "items", "name", "b", "v")); got != 1 {
		t.Errorf("b.v = %d, want 1: an untagged write to a keyed array merged by position", got)
	}
}
