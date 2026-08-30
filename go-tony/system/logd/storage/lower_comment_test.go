package storage

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
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
// The lowered delta carries the comment as a WRAPPER at its own path, and
// markDeltaRoots used to descend past it to mark the change beneath. The marker says
// where a patch is applied FROM, so a marker below the wrapper leaves the wrapper
// outside the subtree that gets applied: the comment was simply not there on the way
// back in.
//
//	stored:  d: e: # note  k2: !logd-patch-root nested: 2
//	head:    d: e: # note  k2: nested: 2  k0: 1
//	replay:  d: e:         k2: nested: 2  k0: 1
//
// Every value matched, so it took comparing the way api.SameState does to see it at
// all. A client's write cannot reach this: it is rooted at the path it names, which is
// the path the comment is on (xqpvk3ehh12ks89mj5n0).
//
// It needs the snapshot. Without one the empty-base branch folds the patches directly
// and the marker is not consulted, so the same write agrees.
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
					s.LowerEverything(true)
				}
				seedHead(t, s)
				mustCommit(t, s, nil, test.seed)
				// The snapshot is what makes the marker load-bearing.
				if err := s.SwitchDLog(); err != nil {
					t.Fatalf("SwitchDLog: %v", err)
				}
				c, err := applyOp(t, s, genOp{path: test.path, src: test.src})
				if err != nil {
					t.Fatalf("write: %v", err)
				}
				head, hc := headOf(s)
				if hc != c {
					t.Fatalf("the head is at %d, want %d", hc, c)
				}
				replay, err := s.replayBaselineAt(c)
				if err != nil {
					t.Fatalf("replay: %v", err)
				}
				if withComments(head) != withComments(replay) {
					t.Errorf("the head and a full read disagree\n head:   %s\n replay: %s",
						withComments(head), withComments(replay))
				}
			})
		}
	}
}
