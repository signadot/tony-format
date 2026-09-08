package index

import (
	"slices"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

func indexed(t *testing.T, idx *Index, commit int64, src string) {
	t.Helper()
	n, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	last := commit - 1
	IndexPatch(idx, &dlog.Entry{Commit: commit, LastCommit: &last, Patch: n}, "A", commit*100, commit, 0, n, nil)
}

// Segments rests on one invariant: an entry indexed anywhere under a path is indexed at
// that path. IndexPatch records a segment at every level on the way to what a patch
// writes, so it holds by construction; this is what says so if that construction changes.
func TestEveryEntryIsIndexedAtEveryPrefix(t *testing.T) {
	idx := NewIndex("")
	indexed(t, idx, 1, `{a: {b: {c: 1}, x: 2}, d: [1, {e: 3}]}`)
	indexed(t, idx, 2, `{a: {b: !replace {from: {c: 1}, to: {c: 9}}}}`)
	indexed(t, idx, 3, `{d: {"(id=q)": {id: q}}}`)

	for _, seg := range idx.AllSegments() {
		if seg.KindedPath == "" {
			continue
		}
		segs := kpath.SplitAll(seg.KindedPath)
		for n := 0; n < len(segs); n++ {
			prefix := joinSegments(segs[:n])
			found := false
			for _, at := range slices.Collect(idx.Segments(prefix, nil, nil, nil)) {
				if at.LogPosition == seg.LogPosition && at.EndCommit == seg.EndCommit {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("entry at %q (commit %d) has no segment at its prefix %q", seg.KindedPath, seg.EndCommit, prefix)
			}
		}
	}
}

// At an ancestor only a write that LANDS there counts; at the path itself everything
// does; each entry once; in commit order.
func TestSegmentsSelectWhatCanReachThePath(t *testing.T) {
	idx := NewIndex("")
	indexed(t, idx, 1, `{a: {b: 1, s: 1}}`) // writes a.b and a.s: passes through a
	indexed(t, idx, 2, `{a: {s: 2}}`)       // a sibling of b, through a
	indexed(t, idx, 3, `{a: !delete null}`) // lands at a: reaches a.b
	indexed(t, idx, 4, `{a: {b: {c: 4}}}`)  // below a.b: reaches a.b
	indexed(t, idx, 5, `{other: 5}`)        // elsewhere

	commits := func(kp string) []int64 {
		var out []int64
		for seg := range idx.Segments(kp, nil, nil, nil) {
			out = append(out, seg.EndCommit)
		}
		return out
	}
	if got := commits("a.b"); !slices.Equal(got, []int64{1, 3, 4}) {
		t.Errorf("a.b: %v, want [1 3 4]: the sibling write and the unrelated write are skipped", got)
	}
	if got := commits(""); !slices.Equal(got, []int64{1, 2, 3, 4, 5}) {
		t.Errorf("root: %v, want every commit once", got)
	}
	if got := commits("a.never"); !slices.Equal(got, []int64{3}) {
		t.Errorf("a.never: %v, want [3]: only the write that landed at its ancestor", got)
	}
	from, to := int64(2), int64(4)
	if got := slices.Collect(idx.Segments("a.b", &from, &to, nil)); len(got) != 2 {
		t.Errorf("a.b in [2,4]: %d segments, want 2", len(got))
	}
}
