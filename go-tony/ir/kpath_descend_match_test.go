package ir

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// TestDescendMatchesWhatListFinds is the cross-check the two halves of the kpath
// grammar owe each other (17hj5ygkh12ks7j7n5n0): for a pattern holding `..`, the
// nodes ListKPath finds are exactly the nodes whose own path the pattern Matches.
// Listing understood `..` and matching refused it, so a caller that asked both
// questions of one pattern got contradictory answers.
// obj builds an object from alternating field names and values, and arr a dense
// array, both with the parent links Node.KPath walks.
func obj(kvs ...any) *Node {
	pairs := make([]KeyVal, 0, len(kvs)/2)
	for i := 0; i+1 < len(kvs); i += 2 {
		pairs = append(pairs, KeyVal{Key: FromString(kvs[i].(string)), Val: kvs[i+1].(*Node)})
	}
	return FromKeyVals(pairs)
}

func arr(elems ...*Node) *Node { return FromSlice(elems) }

func TestDescendMatchesWhatListFinds(t *testing.T) {
	// obj{...} and arr(...) keep parent links, which is what Node.KPath reads.
	doc := obj(
		"verse-dev", obj(
			"drift", obj("finding", obj("abc", obj())),
			"finding", obj("xyz", obj()),
			"runs", arr(
				obj("finding", obj("n", FromInt(1))),
				obj("notes", obj("finding", obj("n", FromInt(2)))),
			),
		),
		"other", obj("finding", obj()),
	)

	for _, pattern := range []string{
		"verse-dev..finding",
		"..finding",
		"verse-dev..finding..n",
		"verse-dev.runs[*]..finding",
		"verse-dev..",
		"..",
		"verse-dev..*",
	} {
		t.Run(pattern, func(t *testing.T) {
			pat, err := kpath.Parse(pattern)
			if err != nil {
				t.Fatalf("parse pattern: %v", err)
			}
			// What listing finds, by path.
			found, err := doc.ListKPath(nil, pattern)
			if err != nil {
				t.Fatalf("ListKPath: %v", err)
			}
			listed := make(map[string]bool, len(found))
			for _, n := range found {
				listed[n.KPath()] = true
			}
			// What matching says, over every path in the document.
			matched := map[string]bool{}
			_ = doc.visitAll(func(n *Node) error {
				p := n.KPath()
				kp, err := kpath.Parse(p)
				if err != nil {
					t.Fatalf("parse node path %q: %v", p, err)
				}
				if pat.Matches(kp) {
					matched[p] = true
				}
				return nil
			})
			for p := range listed {
				if !matched[p] {
					t.Errorf("%q found %q but does not Match it", pattern, p)
				}
			}
			for p := range matched {
				if !listed[p] {
					t.Errorf("%q Matches %q but does not find it", pattern, p)
				}
			}
			if len(listed) == 0 {
				t.Errorf("%q found nothing; the case proves nothing", pattern)
			}
		})
	}
}
