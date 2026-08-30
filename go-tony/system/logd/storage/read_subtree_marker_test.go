package storage

import (
	"fmt"
	"testing"
)

// A narrow read at a path BELOW a write's patch root used to drop the write and answer
// from the snapshot -- silently, with no error and no fallback (1xnezrpkh12ksavvjdn0).
//
// !logd-patch-root says where a stored entry is applied FROM, and the streaming processor
// finds what to apply by walking for it. A narrow read RE-ROOTS each patch to the read
// path by descending into it (patchAtPath), and descended straight past the marker: the
// projected node was an unmarked patch, the processor found no root in it, and it
// contributed nothing.
//
// Reading AT the marker's path always worked, because the projection is the marked node
// itself; reading ABOVE it worked, because the marker is inside the projected subtree.
// Only below was broken, which is "write an entity, then read one of its fields".
//
// A snapshot is required: with none, ApplyPatches takes its empty-base branch and folds
// every patch whole, where markers do not matter. So each case is run both ways, and the
// no-snapshot arm is what says the base -- not the projection -- used to be the answer.
func TestANarrowReadDoesNotDropAWriteMarkedAboveIt(t *testing.T) {
	for _, tc := range []struct {
		name             string
		writePath, write string
		readPath         string
	}{
		{"write at the root, read a field of it", "", `{a: {b: {k: 99}}, z: 1}`, "a"},
		{"write at the root, single field", "", `{a: {b: {k: 99}}}`, "a"},
		{"write at a, read a.b", "a", `{b: {k: 99}}`, "a.b"},
		{"write at a, read the leaf", "a", `{b: {k: 99}}`, "a.b.k"},
		{"write at a.b, read the leaf", "a.b", `{k: 99}`, "a.b.k"},
		// The cases that always worked, kept so a fix cannot trade one for the other.
		{"write at a, read a", "a", `{b: {k: 99}}`, "a"},
		{"write at a.b, read a above it", "a.b", `{k: 99}`, "a"},
	} {
		for _, snapshot := range []bool{true, false} {
			name := tc.name
			if !snapshot {
				name += " (no snapshot)"
			}
			t.Run(name, func(t *testing.T) {
				s := openTestStorage(t)
				mustCommit(t, s, nil, `{a: {b: {k: 1}}, z: 0}`)
				if snapshot {
					if err := s.SwitchDLog(); err != nil {
						t.Fatalf("SwitchDLog: %v", err)
					}
				}
				c := commitAt(t, s, nil, tc.writePath, tc.write)

				wide, err := s.ReadStateAt("", c, nil)
				if err != nil {
					t.Fatalf("wide read: %v", err)
				}
				want, err := wide.GetKPath(tc.readPath)
				if err != nil {
					t.Fatalf("navigate %q: %v", tc.readPath, err)
				}
				got, narrowed, err := s.ReadSubtreeAt(tc.readPath, c, nil)
				if err != nil {
					t.Fatalf("narrow read %q: %v", tc.readPath, err)
				}
				if !narrowed {
					return // declined, so the caller reads wide and gets the oracle
				}
				if !got.DeepEqual(want) {
					t.Errorf("%q: the narrow read lost the write\n got %s\nwant %s",
						tc.readPath, mustEncode(t, got), mustEncode(t, want))
				}
			})
		}
	}
}

// The shape a client actually produces: entities written under a shared ancestor, a
// snapshot, then one of them updated and read back.
func TestANarrowReadOfAnEntitySeesTheWriteThatUpdatedIt(t *testing.T) {
	s := openTestStorage(t)
	for i := 0; i < 5; i++ {
		commitAt(t, s, nil, fmt.Sprintf("verse.entities.e%d", i), fmt.Sprintf(`{id: e%d, n: 0}`, i))
	}
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	c := commitAt(t, s, nil, "verse.entities.e2", `{id: e2, n: 7}`)

	got, narrowed, err := s.ReadSubtreeRootedAt("verse.entities.e2.n", c, nil)
	if err != nil {
		t.Fatalf("narrow read: %v", err)
	}
	if !narrowed {
		t.Skip("declined; the wide read answers")
	}
	if n := getInt(got, "verse", "entities", "e2", "n"); n != 7 {
		t.Errorf("read the entity back with n = %d, want 7 -- the write that set it was dropped\n%s",
			n, mustEncode(t, got))
	}
}
