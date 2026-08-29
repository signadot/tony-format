package storage

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

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
