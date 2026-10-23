package index

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// The invariant residency rests on (index_residency.md): EVICTION CHANGES A COST, NEVER
// AN ANSWER. One index, filled by a random stream of adds and removes over overlapping
// paths and persisted along the way, is asked every question the store asks -- the
// segments in a range at a path, the seek, whether a path was written, the newest commit,
// everything -- under a ceiling a few regions wide and then unbounded, and the answers
// are the same bytes. With evictions and misses counted, so the ceiling was real.
func TestEvictionChangesACostAndNeverAnAnswer(t *testing.T) {
	paths := []string{"", "a", "a.b", "a.b.c", "a.x", "d", "d.e", "items.\"(sku=A)\""}
	for seed := int64(1); seed <= 8; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			dir := t.TempDir()
			idx, m, _, err := OpenIndex(dir, nil)
			if err != nil || m != nil {
				t.Fatalf("OpenIndex fresh: %v %v", m, err)
			}
			ref := NewIndex("")
			rng := rand.New(rand.NewSource(seed))
			var live []LogSegment
			for commit := int64(1); commit <= 600; commit++ {
				switch rng.Intn(10) {
				case 0: // a root snapshot
					seg := *NewSnapshotSegment(commit, "", "A", commit*10, 0, nil)
					idx.Add(&seg)
					ref.Add(&seg)
					live = append(live, seg)
				case 1: // a snapshot of a path
					p := paths[1+rng.Intn(len(paths)-1)]
					seg := *NewSnapshotSegment(commit, p, "A", commit*10, 0, nil)
					idx.Add(&seg)
					ref.Add(&seg)
					live = append(live, seg)
				case 2: // a removal, the way compaction removes
					if len(live) > 0 {
						k := rng.Intn(len(live))
						seg := live[k]
						live = slices.Delete(live, k, k+1)
						if idx.Remove(&seg) != ref.Remove(&seg) {
							t.Fatalf("commit %d: Remove disagreed", commit)
						}
					}
				default: // a write, indexed at the path and every prefix
					p := paths[rng.Intn(len(paths))]
					var scope *string
					if rng.Intn(4) == 0 {
						s := "s1"
						scope = &s
					}
					seg := LogSegment{StartCommit: commit - 1, EndCommit: commit, StartTx: commit, EndTx: commit,
						LogFile: "A", LogPosition: commit * 10, ScopeID: scope}
					for _, kp := range prefixesOf(p) {
						s := seg
						s.KindedPath = kp
						s.Spine = kp != p
						idx.Add(&s)
						ref.Add(&s)
						live = append(live, s)
					}
				}
				if commit%97 == 0 {
					if err := idx.Persist(map[string]int64{"A": 0}); err != nil {
						t.Fatalf("Persist: %v", err)
					}
				}
			}
			if err := idx.Persist(map[string]int64{"A": 0}); err != nil {
				t.Fatalf("Persist: %v", err)
			}
			idx.Residency().SetCeiling(MinIndexCeiling)

			compare(t, idx, ref, paths)
			st := idx.Residency().Stats()
			if st.Evictions == 0 || st.Misses == 0 {
				t.Fatalf("the ceiling did nothing: %+v", st)
			}
			if st.Ceiling > 0 && st.Resident > st.Ceiling+st.Over {
				t.Errorf("resident %d over ceiling %d by more than the recorded excess %d", st.Resident, st.Ceiling, st.Over)
			}
			t.Logf("resident %d of ceiling %d, %d hits, %d misses, %d evictions, over %d",
				st.Resident, st.Ceiling, st.Hits, st.Misses, st.Evictions, st.Over)

			// And the same index reopened from its files: the skeleton and every header,
			// nothing resident, and the same answers.
			if err := idx.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			again, m, why, err := OpenIndex(dir, func(string) int64 { return 0 })
			if err != nil || m == nil {
				t.Fatalf("OpenIndex again: %v (%s)", err, why)
			}
			again.Residency().SetCeiling(MinIndexCeiling)
			compare(t, again, ref, paths)
			if st := again.Residency().Stats(); st.Misses == 0 {
				t.Errorf("a reopened index answered without paging anything: %+v", st)
			}
			again.Close()
		})
	}
}

func prefixesOf(p string) []string {
	out := []string{""}
	if p == "" {
		return out
	}
	var segs []string
	for rest := p; rest != ""; {
		first, tail := splitFirst(rest)
		segs = append(segs, first)
		out = append(out, joinSegments(segs))
		rest = tail
	}
	return out
}

func splitFirst(kp string) (string, string) {
	segs := kpath.SplitAll(kp)
	if len(segs) == 1 {
		return segs[0], ""
	}
	return segs[0], joinSegments(segs[1:])
}

// compare asks both indexes every question the store asks, at every path and in windows
// of commits, and wants the same answer.
func compare(t *testing.T, got, want *Index, paths []string) {
	t.Helper()
	scope := "s1"
	views := []*string{nil, &scope}
	for _, kp := range paths {
		for _, view := range views {
			for _, w := range [][2]int64{{0, 700}, {100, 200}, {550, 600}, {300, 300}} {
				from, to := w[0], w[1]
				g := slices.Collect(got.Segments(kp, &from, &to, view))
				r := slices.Collect(want.Segments(kp, &from, &to, view))
				if !sameSegments(g, r) {
					t.Fatalf("Segments(%q, %d, %d, scope %v): %d segments under the ceiling, %d unbounded\n got  %v\n want %v", kp, from, to, view != nil, len(g), len(r), g, r)
				}
			}
		}
		for _, at := range []int64{50, 250, 599, 1000} {
			gs, gok := got.SnapshotAtOrAbove(kp, at)
			rs, rok := want.SnapshotAtOrAbove(kp, at)
			if gok != rok || !segmentEqual(gs, rs) {
				t.Fatalf("SnapshotAtOrAbove(%q, %d): %+v %v under the ceiling, %+v %v unbounded", kp, at, gs, gok, rs, rok)
			}
		}
		gd, gok := got.UnwrittenBelow(kp + ".zz")
		rd, rok := want.UnwrittenBelow(kp + ".zz")
		if gd != rd || gok != rok {
			t.Fatalf("UnwrittenBelow(%q.zz): %d %v under the ceiling, %d %v unbounded", kp, gd, gok, rd, rok)
		}
		if g, r := got.LookupRangeAll(kp, nil, nil), want.LookupRangeAll(kp, nil, nil); !sameSegments(g, r) {
			t.Fatalf("LookupRangeAll(%q): %d under the ceiling, %d unbounded", kp, len(g), len(r))
		}
	}
	gc, gt, _ := got.MaxCommit()
	rc, rt, _ := want.MaxCommit()
	if gc != rc || gt != rt {
		t.Fatalf("MaxCommit: %d/%d under the ceiling, %d/%d unbounded", gc, gt, rc, rt)
	}
	if g, r := got.AllSegments(), want.AllSegments(); !sameSegments(g, r) {
		t.Fatalf("AllSegments: %d under the ceiling, %d unbounded", len(g), len(r))
	}
}

// A store whose index files are missing, torn, of another version, or written against
// logs at another generation opens fresh and says why; a store whose files are whole
// opens the skeleton and nothing more.
func TestTheDurableIndexIsTrustedOnlyWhenItCanBe(t *testing.T) {
	dir := t.TempDir()
	idx, _, _, err := OpenIndex(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for c := int64(1); c <= 100; c++ {
		idx.Add(&LogSegment{StartCommit: c - 1, EndCommit: c, KindedPath: "a.b", LogFile: "A", LogPosition: c})
	}
	if err := idx.Persist(map[string]int64{"A": 3}); err != nil {
		t.Fatal(err)
	}
	idx.Close()

	if _, m, why, err := OpenIndex(dir, func(string) int64 { return 3 }); err != nil || m == nil || why != "" {
		t.Fatalf("whole files: manifest %v, why %q, err %v", m != nil, why, err)
	}
	if _, m, why, _ := OpenIndex(dir, func(string) int64 { return 4 }); m != nil || why == "" {
		t.Errorf("a generation the index was not written at was trusted (%q)", why)
	}
	// Torn: the regions file shorter than the manifest says.
	if err := os.Truncate(filepath.Join(dir, regionsFileName), 10); err != nil {
		t.Fatal(err)
	}
	if _, m, why, _ := OpenIndex(dir, func(string) int64 { return 3 }); m != nil || why == "" {
		t.Errorf("a torn regions file was trusted (%q)", why)
	}
	// Missing.
	os.Remove(filepath.Join(dir, manifestFileName))
	if _, m, why, _ := OpenIndex(dir, nil); m != nil || why != "no manifest" {
		t.Errorf("no manifest: %v %q", m != nil, why)
	}
}

// segmentEqual compares two segments by value: the scope is a pointer, and a paged-in
// segment's is not the one the reference holds.
func segmentEqual(a, b LogSegment) bool {
	if (a.ScopeID == nil) != (b.ScopeID == nil) || (a.ScopeID != nil && *a.ScopeID != *b.ScopeID) {
		return false
	}
	a.ScopeID, b.ScopeID = nil, nil
	return a == b
}

func sameSegments(a, b []LogSegment) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !segmentEqual(a[k], b[k]) {
			return false
		}
	}
	return true
}

// Rewrite ran only at Close, with no reader in flight, until the persister began running
// it on an open store (1xa86mrnh12krzswvdn0). A page-in takes its region's record and
// then the file's lock, and a rewrite that comes between moves every record and swaps the
// file: the page-in has to read the record where it is now. Readers page cold regions in
// under a ceiling while the file is rewritten over and over; every answer is the
// unbounded index's, and no page-in fails.
func TestAPageInDuringARewriteReadsTheRecordWhereItIs(t *testing.T) {
	idx, _, _, err := OpenIndex(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	ref := NewIndex("")
	const paths, revs = 64, 40
	for p := 0; p < paths; p++ {
		kp := fmt.Sprintf("verse.entities.e%d", p)
		for r := 0; r < revs; r++ {
			c := int64(p*revs + r)
			seg := LogSegment{StartCommit: c, EndCommit: c, KindedPath: kp, LogFile: "A", LogPosition: c * 64}
			idx.Add(&seg)
			ref.Add(&seg)
		}
	}
	gens := map[string]int64{"A": 0}
	if err := idx.Persist(gens); err != nil {
		t.Fatalf("persist: %v", err)
	}
	idx.res.SetCeiling(MinIndexCeiling)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var mismatches atomic.Int64
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			for k := r; ; k += 7 {
				select {
				case <-stop:
					return
				default:
				}
				kp := fmt.Sprintf("verse.entities.e%d", k%paths)
				if !sameSegments(idx.lookupRange(kp, nil, nil, nil), ref.lookupRange(kp, nil, nil, nil)) {
					mismatches.Add(1)
				}
			}
		}(r)
	}
	for k := 0; k < 25; k++ {
		if err := idx.Rewrite(gens); err != nil {
			t.Fatalf("rewrite %d: %v", k, err)
		}
	}
	close(stop)
	wg.Wait()
	if n := mismatches.Load(); n > 0 {
		t.Fatalf("%d reads during rewrites answered other than the unbounded index", n)
	}
	if st := idx.res.Stats(); st.Misses == 0 {
		t.Fatalf("no region was paged in: %+v", st)
	}
}
