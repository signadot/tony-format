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
		{"keyed array", `{items: !key(name) [{name: "a", v: 1}]}`},
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

// TestBaseline_KeyedMergeAfterSnapshot checks whether identity merge still works once
// the base is a snapshot. If the snapshot drops the !key tag, merges keep working only
// because each incoming PATCH carries the tag itself — meaning the tag is a property of
// writes, never of stored state.
func TestBaseline_KeyedMergeAfterSnapshot(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	scalingCommit(t, s, nil, `{items: !key(name) [{name: "a", v: 1}, {name: "b", v: 1}]}`, nil)
	showDoc(t, s, nil, "initial")

	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	showDoc(t, s, nil, "after snapshot")

	// Update key "a" only. With identity merge this updates a in place (2 items);
	// without it, a positional merge overwrites element 0 and may truncate.
	scalingCommit(t, s, nil, `{items: !key(name) [{name: "a", v: 99}]}`, nil)
	showDoc(t, s, nil, "after keyed update WITH !key on the patch")

	// Same update, but the patch does NOT carry the tag: positional merge territory.
	scalingCommit(t, s, nil, `{items: [{name: "a", v: 7}]}`, nil)
	showDoc(t, s, nil, "after update WITHOUT !key on the patch")
}
