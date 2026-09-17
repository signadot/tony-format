package storage

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// Children is checked against the keys of the value a read answers at the same commit
// and path, as the store holds it (unraised: a keyed array is an object of names). Every
// phase below writes something the fold has to get right, and then the sweep asks every
// commit so far at every path (3kgxprskh12krjrmndn0).

// storedChildren is what Children must list: the direct children of the stored value at
// kp as of at, from a read of it.
func storedChildren(t *testing.T, s *Storage, at int64, scope *string, kp string) []Child {
	t.Helper()
	c, err := s.Read(at, scope, kp)
	if err != nil {
		t.Fatalf("read %q@%d: %v", kp, at, err)
	}
	node, err := Collect(c, 1<<30)
	if err != nil {
		t.Fatalf("collect %q@%d: %v", kp, at, err)
	}
	node = ir.Uncomment(node)
	if node == nil {
		return nil
	}
	var out []Child
	switch node.Type {
	case ir.ObjectType:
		sparse := kindOfNode(node) == ChildSparseArray
		for i, f := range node.Fields {
			seg := kpath.Field(f.String).String()
			if sparse {
				seg = kpath.SparseIndex(int(*f.Int64)).String()
			}
			out = append(out, Child{Segment: seg, Kind: kindOfNode(node.Values[i])})
		}
	case ir.ArrayType:
		for i, v := range node.Values {
			out = append(out, Child{Segment: kpath.Index(i).String(), Kind: kindOfNode(v)})
		}
	}
	return out
}

func listChildren(t *testing.T, s *Storage, at int64, scope *string, kp, after string) []Child {
	t.Helper()
	var out []Child
	if err := s.Children(at, scope, kp, after, func(c Child) bool {
		out = append(out, c)
		return true
	}); err != nil {
		t.Fatalf("Children(%q@%d after %q): %v", kp, at, after, err)
	}
	return out
}

func sameChildren(a, b []Child) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sweep checks every commit through head at every path.
func sweep(t *testing.T, s *Storage, scope *string, phase string, paths ...string) {
	t.Helper()
	head, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatal(err)
	}
	for at := int64(1); at <= head; at++ {
		for _, kp := range paths {
			want := storedChildren(t, s, at, scope, kp)
			got := listChildren(t, s, at, scope, kp, "")
			if !sameChildren(got, want) {
				t.Errorf("%s: Children(%q@%d) = %v, want %v", phase, kp, at, got, want)
			}
		}
	}
}

var listingPaths = []string{"", "jobs", "jobs.a1", "list", "sparse", "runs", "leaf", "nope", "jobs.gone"}

func TestChildren_MatchTheStoredValueThroughEveryPhase(t *testing.T) {
	s := openTestStorage(t)
	s.SetPathSnapshotPolicy(-1, 0) // snapshots are taken where the test says
	seed := `{jobs: {a1: {status: done, n: 1}, a2: {status: ready, n: 2}, gone: {x: 1}}, ` +
		`list: [1, {b: 2}, [3]], sparse: !sparsearray {3: x, 7: {deep: 1}}, ` +
		`runs: {"(id=r1)": {id: r1}, "(id=r2)": {id: r2}}, leaf: 1}`
	mustCommit(t, s, nil, seed)
	sweep(t, s, nil, "fresh", listingPaths...)

	// Deletes, additions, a kind change, a merge into a container, a sparse key.
	mustCommit(t, s, nil, `{jobs: {gone: !delete, a0: {status: new}, a1: {n: 5}, a2: "now a string"}, sparse: !sparsearray {5: y}}`)
	sweep(t, s, nil, "edited", listingPaths...)

	// A total cover at the path, and a delete of it.
	mustCommit(t, s, nil, `{jobs: !insert {z1: {status: done}, z2: 3}}`)
	mustCommit(t, s, nil, `{list: [9]}`)
	sweep(t, s, nil, "replaced", listingPaths...)
	mustCommit(t, s, nil, `{jobs: !delete}`)
	sweep(t, s, nil, "deleted", listingPaths...)
	mustCommit(t, s, nil, `{jobs: {back: 1}, leaf: {now: {a: container}}}`)
	sweep(t, s, nil, "returned", listingPaths...)

	// A root snapshot: the base is its table, the tail what follows.
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	sweep(t, s, nil, "snapshot", listingPaths...)
	mustCommit(t, s, nil, `{jobs: {after: {snap: true}, back: !delete}, runs: {"(id=r3)": {id: r3}}}`)
	sweep(t, s, nil, "snapshot+tail", listingPaths...)
	head, _ := s.GetCurrentCommit()
	if seg, ok := s.index.SnapshotAtOrAbove("jobs", head); !ok || seg.KindedPath != "" {
		t.Fatalf("no root snapshot to list from: %+v %v", seg, ok)
	}
	// At the head, with nothing but plain writes since the snapshot, a listing is the
	// table plus the tail: nothing streams.
	before := s.ReadStats()
	listChildren(t, s, head, nil, "jobs", "")
	listChildren(t, s, head, nil, "", "")
	if st := s.ReadStats(); st.ListStream != before.ListStream || st.ListTable != before.ListTable+2 {
		t.Errorf("listing at the head over a snapshot: table %d→%d, stream %d→%d; want 2 from the table",
			before.ListTable, st.ListTable, before.ListStream, st.ListStream)
	}

	// A write ABOVE the path stating it whole: the fold cannot say, and streams.
	mustCommit(t, s, nil, `!insert {jobs: {only: 1}, list: [1, 2], leaf: 2}`)
	sweep(t, s, nil, "blocked above", listingPaths...)
	head, _ = s.GetCurrentCommit()
	before = s.ReadStats()
	listChildren(t, s, head, nil, "jobs", "")
	if st := s.ReadStats(); st.ListStream != before.ListStream+1 {
		t.Errorf("a listing under a write that states the path whole did not stream: %+v", st)
	}

	// Compaction: history before the snapshot is gone, the listing is not.
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	if err := s.Compact(&CompactionConfig{Cutoff: 0, BaseInterval: time.Hour, SlotsPerTier: 8, Multiplier: 2, GracePeriod: 100 * time.Millisecond}); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	sweep(t, s, nil, "compacted", listingPaths...)
}

// A scope lists what a scoped read holds: its own additions, and nothing baseline wrote
// under a claim of the scope's.
func TestChildren_InAScope(t *testing.T) {
	s := openTestStorage(t)
	s.SetPathSnapshotPolicy(-1, 0)
	scope := "s1"
	mustCommit(t, s, nil, `{jobs: {a1: {n: 1}, a2: {n: 2}}, other: {x: 1}}`)
	mustCommit(t, s, &scope, `{jobs: {s1only: {n: 3}, a1: !delete}}`)
	mustCommit(t, s, nil, `{jobs: {a3: {n: 4}}}`)
	for _, sc := range []*string{nil, &scope} {
		sweep(t, s, sc, fmt.Sprintf("scope %v", sc != nil), "", "jobs", "other")
	}
	// A claim of the scope's at the path shadows what baseline writes there after.
	mustCommit(t, s, &scope, `{other: !insert {claimed: 1}}`)
	mustCommit(t, s, nil, `{other: {y: 2}}`)
	for _, sc := range []*string{nil, &scope} {
		sweep(t, s, sc, fmt.Sprintf("claim, scope %v", sc != nil), "", "jobs", "other")
	}
}

// A listing pages: from a name, and stopping when the caller has enough. Over a table
// with a tail, so both sources are paged through.
func TestChildren_PagesFromANameAndStops(t *testing.T) {
	s := openTestStorage(t)
	s.SetPathSnapshotPolicy(-1, 0)
	var b strings.Builder
	b.WriteString("{jobs: {")
	for i := range 50 {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "j%02d: {n: %d}", i*2, i)
	}
	b.WriteString("}}")
	mustCommit(t, s, nil, b.String())
	if err := s.SwitchDLog(); err != nil {
		t.Fatal(err)
	}
	mustCommit(t, s, nil, `{jobs: {j01: {n: 1}, j51: {n: 51}, j98: !delete, j99: 9}}`) // between, after, removed, and a leaf
	head, _ := s.GetCurrentCommit()

	all := storedChildren(t, s, head, nil, "jobs")
	for _, after := range []string{"", "j00", "j01", "j02", "j50", "j51", "j97", "j98", "j99", "jzz"} {
		var want []Child
		for _, c := range all {
			if after == "" || segmentCompare(c.Segment, after) > 0 {
				want = append(want, c)
			}
		}
		got := listChildren(t, s, head, nil, "jobs", after)
		if !sameChildren(got, want) {
			t.Errorf("after %q: %v, want %v", after, got, want)
		}
	}
	// Stopping early.
	n := 0
	if err := s.Children(head, nil, "jobs", "", func(Child) bool { n++; return n < 7 }); err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Errorf("fn was called %d times after answering false at 7", n)
	}
}

// A listing of a container larger than any node budget costs the page: the decode policy
// snapshots the parent after the first child read, and the listing reads the table.
func TestChildren_ListsWhatCannotBeBuilt(t *testing.T) {
	s := openTestStorage(t)
	head := heavyContainer(t, s, "jobs", 10000)
	if _, _, err := readSubtreeAt(s, "jobs.j000001", head, nil); err != nil {
		t.Fatal(err)
	}
	s.waitPathSnapshots()
	if st := s.ReadStats(); st.PathSnapshots != 1 {
		t.Fatalf("no snapshot of jobs: %+v", st)
	}
	mustCommit(t, s, nil, `{jobs: {j000000: !delete, j999999: {status: late}}}`)
	head, _ = s.GetCurrentCommit()

	started := time.Now()
	n, first := 0, ""
	if err := s.Children(head, nil, "jobs", "j005000", func(c Child) bool {
		if n == 0 {
			first = c.Segment
		}
		n++
		return n < 10
	}); err != nil {
		t.Fatal(err)
	}
	t.Logf("a page of 10 from the middle of 10 000: %v", time.Since(started))
	if n != 10 || first != "j005001" {
		t.Errorf("page from j005000: %d children, first %q", n, first)
	}
	if st := s.ReadStats(); st.ListTable != 1 || st.ListStream != 0 {
		t.Errorf("the page was not answered from the table: table %d, stream %d", st.ListTable, st.ListStream)
	}
	want := storedChildren(t, s, head, nil, "jobs")
	got := listChildren(t, s, head, nil, "jobs", "")
	if !sameChildren(got, want) {
		t.Errorf("the whole listing differs from the stored value: %d vs %d children", len(got), len(want))
	}
}
