package storage

import (
	"fmt"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// !logd-patch-root, and the four things that have to be true of it.
//
// The marker says WHERE a stored entry is applied from. It is logd's own bookkeeping: not
// an operation, not a label of any value, and never something a client wrote. The
// streaming processor finds what to apply by walking for it, which is the whole of its
// job, and everything below is a way of getting that job wrong.
//
//  1. It goes where the change LANDS, not at the entry's root -- the selectivity of every
//     narrow read is which paths carry one (TestLoweredMarkerLandsOnTheChange).
//  2. It never reaches the DOCUMENT. A merge composes a patch's tag onto the document's,
//     so a fold that does not strip it hands it to the next operation as data
//     (TestAPatchRootMarkerDoesNotReachTheDocument, and the same for the other fold).
//  3. It must not share a tag chain with an OPERATOR, where it stops being metadata: a
//     merge dispatches on that chain (TestARelativeRootWriteDoesNotDestroyTheDocument).
//  4. It moves with a patch that is RE-ROOTED, or the re-rooted patch is not a patch root
//     any more and applies nothing (TestANarrowReadDoesNotDropAWriteMarkedAboveIt).
//
// Each was a defect, three of them silent, and two of them lost a whole document. They are
// together because they are one invariant seen from four sides, and because a fix for any
// one of them is easy to write in a way that breaks another.

// markerPathsIn lists the paths a stored patch's !logd-patch-root markers sit at.
func markerPathsIn(n *ir.Node, at string) []string {
	if n == nil {
		return nil
	}
	var out []string
	if tx.HasPatchRootTag(n) {
		p := at
		if p == "" {
			p = "(root)"
		}
		out = append(out, p)
	}
	u := ir.Uncomment(n)
	if u == nil {
		return out
	}
	for i, f := range u.Fields {
		if i >= len(u.Values) {
			break
		}
		child := f.String
		if at != "" {
			child = at + "." + f.String
		}
		out = append(out, markerPathsIn(u.Values[i], child)...)
	}
	return out
}

func markersAtCommit(t *testing.T, s *Storage, commit int64) []string {
	t.Helper()
	for _, seg := range s.index.AllSegments() {
		if seg.KindedPath != "" || seg.EndCommit != commit {
			continue
		}
		e, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition,
			seg.LogFileGeneration)
		if err != nil || e.Patch == nil {
			continue
		}
		return markerPathsIn(e.Patch, "")
	}
	return nil
}

// A delta is marked where the change lands, and an EMPTY container is a change like
// any other: `{a: {}}` says a is now empty, which is a statement about a.
//
// markDeltaRoots used to require the child to have fields before descending into it,
// so an empty one stopped the descent a level early and the entry was marked at the
// document root -- a patch on the whole document as far as patches.BuildPatchIndex is
// concerned, which is what decides whether a narrow read may skip it.
//
// A delete of a path that does not exist yet produces exactly that shape: applying it
// creates the spine and leaves an empty container behind.
//
// A SCOPE stores the claim rather than the difference, so that one case is marked a
// level deeper -- there is a statement about a.b to mark, where baseline has only the
// empty container the delete left. Where the two store the same thing they mark the
// same place, which is the rest of the table.
func TestLoweredMarkerLandsOnTheChange(t *testing.T) {
	tests := []struct {
		name, seed, path, src string
		want                  string
		scopeWant             string // when the claim differs from the difference
	}{{
		name:      "a delete of a path that is not there yet",
		seed:      `{z: 0}`,
		path:      "a.b",
		src:       `!delete`,
		want:      "a",
		scopeWant: "a.b",
	}, {
		name: "an ordinary write, for contrast",
		seed: `{z: 0}`,
		path: "a.b",
		src:  `{k1: 4}`,
		want: "a.b",
	}, {
		name: "a delete of a path that IS there",
		seed: `{a: {b: {k1: 1}}, z: 0}`,
		path: "a.b",
		src:  `!delete`,
		want: "a.b",
	}}

	for _, test := range tests {
		for _, scoped := range []bool{false, true} {
			name := test.name
			if scoped {
				name += " [scope]"
			}
			want := test.want
			if scoped && test.scopeWant != "" {
				want = test.scopeWant
			}
			t.Run(name, func(t *testing.T) {
				s := openTestStorage(t)
				s.LowerEverything(true)
				mustCommit(t, s, nil, test.seed)

				const scope = "s1"
				c, err := applyScopeOp(t, s, scopeOp{
					scoped: scoped, path: test.path, src: test.src,
				}, scope)
				if err != nil {
					t.Fatalf("write: %v", err)
				}
				got := markersAtCommit(t, s, c)
				if strings.Join(got, ",") != want {
					t.Errorf("marked at %v, want [%s]", got, want)
				}
			})
		}
	}
}

// Rule 2. A document carrying the marker hands it to the next operation as though it were
// data, and an operation which asks the document for its tag then refuses:
//
//	delete patching "!logd-patch-root\n{\n  k1: 4\n}"
//	gave doc tag !bracket.logd-patch-root at $ didn't match bracket
//
// The empty-base branch folds each patch into an accumulating document and stripped the
// marker once, after the last fold -- so every patch after the first was applied to a
// document still wearing the previous patch's marker. Four baseline writes at the root
// reach it, with no scope, no snapshot and nothing keyed (2w62pyyah12ksqh0jdn0).
//
// It takes a delta marked at the document ROOT, which is what markDeltaRoots produces
// when it cannot descend to a deeper container, so lowering is what makes it reachable.
// Both arms are kept because the difference between them is the point: the same four
// writes are clean when only the writes that need lowering are lowered.
func TestAPatchRootMarkerDoesNotReachTheDocument(t *testing.T) {
	markerDoesNotReachTheDocument(t, false)
}

// The same, with a SNAPSHOT under the writes, which is a different fold: with base events
// to seek through, the streaming processor collects the node at a patch root and folds
// that path's patches into it (applyPatchesToNode) rather than folding whole patches from
// null. Both folds had the same defect and neither covered the other.
//
// This one is caught by an operation that CHECKS the document's tag rather than by a type
// mismatch: `!delete(bracket)` against a document wearing `!bracket.logd-patch-root` is
// asked to match `bracket`, and does not.
func TestAPatchRootMarkerDoesNotReachTheDocumentAcrossASnapshot(t *testing.T) {
	markerDoesNotReachTheDocument(t, true)
}

func markerDoesNotReachTheDocument(t *testing.T, snapshot bool) {
	for _, lowerAll := range []bool{false, true} {
		name := "lowering the writes that need it"
		if lowerAll {
			name = "lowering every write"
		}
		t.Run(name, func(t *testing.T) {
			s := openTestStorage(t)
			if lowerAll {
				s.LowerEverything(true)
			}
			for i, src := range []string{
				"# note\n{k0: 0}",
				`!rename [{from: "k0", to: "k0"}]`,
				`{k1: 4}`,
				`!delete`,
			} {
				op := scopeOp{path: "", src: src, snapshot: snapshot && i == 0}
				if _, err := applyScopeOp(t, s, op, "s1"); err != nil {
					t.Fatalf("%s: %v", src, err)
				}
				if op.snapshot {
					if err := s.SwitchDLog(); err != nil {
						t.Fatalf("SwitchDLog: %v", err)
					}
				}
			}
			c, err := s.GetCurrentCommit()
			if err != nil {
				t.Fatal(err)
			}
			doc, err := s.ReadStateAt("", c, nil)
			if err != nil {
				t.Fatalf("baseline read: %v", err)
			}
			if doc != nil {
				if got := withComments(doc); strings.Contains(got, "logd-patch-root") {
					t.Errorf("the document carries logd's marker: %s", got)
				}
			}
		})
	}
}

// Rule 3, from the path that reaches it: a relative write at the DOCUMENT ROOT destroyed
// the document.
//
// A no-op !rename changes nothing but presentation, so lowering it produces a
// presentation-only delta whose ROOT is a tag op over a null:
//
//	!addtag(bracket) null
//
// markDeltaRoots cannot descend past that null -- the descent wants a single-field
// object -- so it marks that node, and the marker composes into the operation's own tag
// chain. mergeop hands everything after the first registered op to the child, so the null
// that meant "the value did not change" arrived as a TAGGED null and the merge answered
// with it. The store read back null (1hf5pzj6h12ksd40jdn0).
//
// Fixed in mergeop, where a null child says the value did not change unless an OPERATION
// trails the op; mergeop/tagdiff_trailing_test.go holds that half. Lowering is on by
// default, so this was the shipped answer.
//
// The same write one level down was always safe: its delta is `{a: !addtag(bracket) null}`
// and the marker lands on the plain object above the op. Both are kept, because the
// difference between them is what made this hard to see.
func TestARelativeRootWriteDoesNotDestroyTheDocument(t *testing.T) {
	for _, tc := range []struct{ name, path, write string }{
		{"at the document root", "", `!rename [{from: "k1", to: "k1"}]`},
		{"one level down", "a", `!rename [{from: "z", to: "z"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStorage(t)
			mustCommit(t, s, nil, `{k1: 5, k2: 16, a: {z: 1}}`)
			c := commitAt(t, s, nil, tc.path, tc.write)

			doc, err := s.ReadStateAt("", c, nil)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if doc == nil {
				t.Fatalf("the write destroyed the document")
			}
			// A no-op rename says nothing about any value, so every one stands.
			if got := getInt(doc, "k1"); got != 5 {
				t.Errorf("k1 = %d, want 5\n%s", got, mustEncode(t, doc))
			}
			if got := getInt(doc, "k2"); got != 16 {
				t.Errorf("k2 = %d, want 16\n%s", got, mustEncode(t, doc))
			}
			if got := getInt(doc, "a", "z"); got != 1 {
				t.Errorf("a.z = %d, want 1\n%s", got, mustEncode(t, doc))
			}
		})
	}
}

// Rule 4. A narrow read RE-ROOTS each patch to the read path by descending into it
// (patchAtPath), and used to descend straight past the marker: the projection was an
// unmarked patch, the processor found no root in it, and the write contributed nothing.
// The read then answered from the snapshot and reported success -- silently, with no
// error and no fallback (1xnezrpkh12ksavvjdn0).
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

// Rule 4 where the projection lands ON an operator, which is where rules 3 and 4 meet.
//
// A no-op !rename at a lowers to `{a: !addtag(bracket) null}`, marked at the document root
// because markDeltaRoots cannot descend past a null. A narrow read at "a" therefore
// re-roots the marker onto the addtag node itself -- so this read is correct only while a
// merge reads a foreign label in an operator's chain as what it is rather than as the
// operand (mergeop's patchUnderTagDiff; mergeop/tagdiff_trailing_test.go holds that half).
//
// The read path used to DECLINE this case rather than depend on that. It no longer does,
// and this is the test that says the dependency holds: the seeded differential finds it
// too, but only above 200 seeds, which is not a guard.
func TestANarrowReadReRootsAMarkerOntoAnOperator(t *testing.T) {
	s := openTestStorage(t)
	s.LowerEverything(true)

	commitAt(t, s, nil, "a", `{k1: 2}`)
	commitAt(t, s, nil, "a", `{k2: 4}`)
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	c := commitAt(t, s, nil, "a", `!rename [{from: "k1", to: "k1"}]`)

	wide, err := s.ReadStateAt("", c, nil)
	if err != nil {
		t.Fatalf("wide read: %v", err)
	}
	want, err := wide.GetKPath("a")
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	got, narrowed, err := s.ReadSubtreeAt("a", c, nil)
	if err != nil {
		t.Fatalf("narrow read: %v", err)
	}
	if !narrowed {
		t.Skip("declined; the wide read answers")
	}
	if !got.DeepEqual(want) {
		t.Errorf("the narrow read disagrees with the wide one\n got %s\nwant %s",
			mustEncode(t, got), mustEncode(t, want))
	}
}
