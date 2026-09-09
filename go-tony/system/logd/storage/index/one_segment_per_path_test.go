package index

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// An entry is indexed ONCE at each path it states something at. An operation over a
// container used to be recorded at its own path twice -- once for the operation and once
// for the operand that sits where it sits -- and the two are equal to the tree, which kept
// whichever came through last on a re-index (eachPatchBelow says what that cost).
func TestAnOperationOverAContainerIsOneSegmentAtItsPath(t *testing.T) {
	for _, tc := range []struct {
		src   string
		paths map[string]bool // path -> spine
	}{
		{`{a: !insert.raw {x: 1, y: {z: 2}}}`, map[string]bool{"": true, "a": false, "a.x": false, "a.y": true, "a.y.z": false}},
		{`{a: !raw {x: 1}}`, map[string]bool{"": true, "a": false, "a.x": false}},
		{`{a: !delete {x: 1}}`, map[string]bool{"": true, "a": false, "a.x": false}},
		{`{a: !replace {from: {p: 1}, to: {q: 2}}}`, map[string]bool{"": true, "a": false, "a.p": false, "a.q": false}},
		{`{a: {x: 1}}`, map[string]bool{"": true, "a": true, "a.x": false}},
	} {
		t.Run(tc.src, func(t *testing.T) {
			n, err := parse.Parse([]byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			last := int64(0)
			e := &dlog.Entry{Commit: 1, LastCommit: &last, Patch: n}
			seen := map[string]int{}
			spine := map[string]bool{}
			EachSegment(e, "A", 0, 1, func(seg *LogSegment) {
				seen[seg.KindedPath]++
				spine[seg.KindedPath] = seg.Spine
			})
			for p, c := range seen {
				if c != 1 {
					t.Errorf("%q indexed %d times", p, c)
				}
			}
			for p, want := range tc.paths {
				if _, ok := seen[p]; !ok {
					t.Errorf("%q not indexed; got %v", p, seen)
				} else if spine[p] != want {
					t.Errorf("%q spine=%v, want %v", p, spine[p], want)
				}
			}
			if len(seen) != len(tc.paths) {
				t.Errorf("indexed at %v, want exactly the %d paths %v", seen, len(tc.paths), tc.paths)
			}
		})
	}
}
