package storage

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// withComments renders a document the way SameState compares one. nodeText and
// encode.MustString do NOT print comments, so a comparison made with either shows two
// documents as identical when the store says they differ -- which is what hid this
// for a while.
func withComments(n *ir.Node) string {
	if n == nil {
		return "<nil>"
	}
	b := &strings.Builder{}
	if err := encode.Encode(n, b, encode.EncodeComments(true)); err != nil {
		return "<encode error: " + err.Error() + ">"
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// A comment on a value that is itself being introduced, replayed across a snapshot.
//
// The lowered delta carries the comment as a WRAPPER at its own path, and the wrapper
// is where the entry is applied from (patches.walkAndCollectPatchRoots): a root beneath
// it would leave the wrapper outside the subtree that gets applied, and the comment
// would simply not be there on the way back in.
//
//	stored:  d: e: # note  k2: nested: 2
//	fold:    d: e: # note  k2: nested: 2  k0: 1
//	replay:  d: e:         k2: nested: 2  k0: 1    <- a root beneath the wrapper
//
// Every value matches either way, so it takes comparing the way api.SameState does to
// see it at all. A client's write cannot reach this: it is rooted at the path it names,
// which is the path the comment is on (xqpvk3ehh12ks89mj5n0).
//
// It needs the snapshot. Without one the empty-base branch folds the patches directly
// and the roots are not consulted, so the same write agrees.
func TestLoweredCommentSurvivesASnapshot(t *testing.T) {
	tests := []struct{ name, seed, path, src string }{
		{"a new subtree with a comment", `{d: {k0: 1}}`, "d.e", "# note\n{k2: {nested: 2}}"},
		{"a new subtree, no comment", `{d: {k0: 1}}`, "d.e", `{k2: {nested: 2}}`},
		{"an existing subtree gains a comment", `{d: {e: {k2: {nested: 2}}}}`, "d.e",
			"# note\n{k2: {nested: 2}}"},
		{"a new leaf with a comment", `{d: {k0: 1}}`, "d.e", "# note\n5"},
		{"a comment deeper than the write path", `{d: {k0: 1}}`, "d",
			"e:\n  # note\n  k2:\n    nested: 2\n"},
	}

	for _, test := range tests {
		for _, lowered := range []bool{false, true} {
			name := test.name
			if lowered {
				name += " [lowered]"
			}
			t.Run(name, func(t *testing.T) {
				s := openTestStorage(t)
				if lowered {
					s.lowerEverything(true)
				}
				mustCommit(t, s, nil, test.seed)
				// The snapshot is what makes the rooting load-bearing.
				if err := s.SwitchDLog(); err != nil {
					t.Fatalf("SwitchDLog: %v", err)
				}
				c, err := applyOp(t, s, genOp{path: test.path, src: test.src})
				if err != nil {
					t.Fatalf("write: %v", err)
				}
				// Two computations of the state at c have to agree, comments counted: the
				// read, and a watcher's fold of the stored delta onto the state before it.
				prev, err := readStateAt(s, "", c-1, nil)
				if err != nil {
					t.Fatalf("read at %d: %v", c-1, err)
				}
				ns, err := readPatchesInRange(s, "", c, c, nil)
				if err != nil || len(ns) != 1 {
					t.Fatalf("delta for %d: %v %v", c, ns, err)
				}
				stepped, err := applyStoredPatch(prev, ns[0].Patch)
				if err != nil {
					t.Fatalf("fold: %v", err)
				}
				read, err := readStateAt(s, "", c, nil)
				if err != nil {
					t.Fatalf("read: %v", err)
				}
				if withComments(stepped) != withComments(read) {
					t.Errorf("the fold and the read disagree\n fold: %s\n read: %s",
						withComments(stepped), withComments(read))
				}
			})
		}
	}
}

// A tag under a head comment is the value's, and the store reads it as such
// (vbtm1dfbh12krbkcq1n0). Read from the wrapper, which wears none:
//
//   - an operation under a comment was taken for an absolute write and stored as
//     it was sent, a !replace in the log, which is what lowering is there to
//     prevent (TestLowering_RelativeWriteIsStoredAsItsResult);
//   - an escape under a comment was walked into, and the write refused for the
//     operators in the data it escaped: a stored rule with a note above it.
func TestCommentedTagIsTheValues(t *testing.T) {
	t.Run("an operation under a comment is lowered", func(t *testing.T) {
		s := openTestStorage(t)
		mustCommit(t, s, nil, `{s: "bob", n: 1}`)
		c, err := applyOp(t, s, genOp{path: "", src: "s:\n  # renamed\n  !replace\n  from: bob\n  to: rob\n"})
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		// A head comment on a field's value prints above the field.
		if state, want := withComments(mustReadScope(t, s, c, nil)), `n: 1 # renamed s: rob`; state != want {
			t.Errorf("state is %s, want %s", state, want)
		}
		if stored := strings.Join(storedPatches(t, s), " | "); strings.Contains(stored, "!replace") {
			t.Errorf("the log holds a !replace: %s", stored)
		}
	})
	// In a scope, where a write is stored as the claim it makes (claimValue): the
	// claim is !insert.raw on the value, under the comment, and it is what the
	// lowered delta is held to.
	t.Run("an escape under a comment is stored, and read back as it was", func(t *testing.T) {
		for _, tc := range []struct{ name, seed, path, src string }{
			{"at its own path", `{rules: {}}`, "rules.spec", "# why\n!insert.raw\nvalue: !and [1, 2]\n"},
			{"beneath the path written", `{rules: {}}`, "rules", "spec:\n  # why\n  !insert.raw\n  value: !and [1, 2]\n"},
			{"beside an operation", `{rules: {n: 1}}`, "rules",
				"n: !replace {from: 1, to: 2}\nspec:\n  # why\n  !insert.raw\n  value: !and [1, 2]\n"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				s := openTestStorage(t)
				mustCommit(t, s, nil, tc.seed)
				scope := "sc"
				n, err := parse.Parse([]byte(tc.src), parse.ParseComments(true))
				if err != nil {
					t.Fatal(err)
				}
				txn, err := s.NewTx(1, &scope)
				if err != nil {
					t.Fatal(err)
				}
				p, err := txn.NewPatcher(&api.Patch{PathData: api.PathData{Path: tc.path, Data: n}})
				if err != nil {
					t.Fatal(err)
				}
				res := p.Commit()
				if !res.Committed {
					t.Fatalf("write: %v", res.Error)
				}
				state := withComments(mustReadScope(t, s, res.Commit, &scope))
				for _, want := range []string{"# why", "!and"} {
					if !strings.Contains(state, want) {
						t.Errorf("state %s does not hold %s", state, want)
					}
				}
			})
		}
	})
}
