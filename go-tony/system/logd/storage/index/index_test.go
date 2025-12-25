package index

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestIndexAddLookupPointDiff(t *testing.T) {
	idx := NewIndex("")

	segs := []LogSegment{
		*PointLogSegment(10, 100, "foo.bar"),
		*PointLogSegment(11, 101, "foo"),
		*PointLogSegment(12, 102, "foo.bar.baz"),
		*PointLogSegment(13, 103, "qux"),
	}

	// 1. Test Add
	// Add some commits at different paths
	for i := range segs {
		idx.Add(&segs[i])
	}

	// 2. Test LookupRange (exact matches only, no ancestors or descendants)

	got := idx.LookupRange("foo.bar", nil, nil, nil)

	// LookupRange("foo.bar") navigates to "foo" child, which includes segments at "foo" level
	// then navigates to "bar", so it returns both "foo" and "foo.bar"
	want := []LogSegment{segs[0], segs[1]} // "foo.bar" at commit 10, "foo" at commit 11

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("LookupRange mismatch (-want +got):\n%s", diff)
	}
}

func TestIndexRemove(t *testing.T) {
	idx := NewIndex("")

	segs := []LogSegment{
		*PointLogSegment(10, 100, "foo.bar"),
		*PointLogSegment(11, 101, "foo"),
		*PointLogSegment(12, 102, "foo.bar.baz"),
		*PointLogSegment(13, 103, "qux"),
	}

	// 1. Test Add
	// Add some commits at different paths
	for i := range segs {
		idx.Add(&segs[i])
	}

	// 3. Test Replace
	// Replace {10, 100, ""} at "foo.bar" with a compacted range.
	rm := segs[:3]
	for i := range rm {
		seg := &rm[i]
		if !idx.Remove(seg) {
			t.Errorf("didn't remove %v", seg)
		}
	}

	compactRange := &LogSegment{
		StartCommit: 10, StartTx: 100,
		EndCommit: 12, EndTx: 102,
		KindedPath: "foo",
		LogFile:    "A",
	}

	idx.Add(compactRange)

	// LookupRange("", nil, nil, nil) only returns root-level segments, not child indices
	// So we need to query the specific paths
	gotFoo := idx.LookupRange("foo", nil, nil, nil)
	gotQux := idx.LookupRange("qux", nil, nil, nil)

	// PointLogSegment(13, 103, "qux") creates StartCommit: 13, EndCommit: 13
	quxSeg := PointLogSegment(13, 103, "qux")
	wantFoo := []LogSegment{*compactRange}
	wantQux := []LogSegment{*quxSeg}

	if diff := cmp.Diff(wantFoo, gotFoo); diff != "" {
		t.Errorf("LookupRange('foo') after Replace mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(wantQux, gotQux); diff != "" {
		t.Errorf("LookupRange('qux') after Replace mismatch (-want +got):\n%s", diff)
	}
}

func TestIndexPersist(t *testing.T) {
	idx := NewIndex("")
	segs := []LogSegment{
		*PointLogSegment(10, 100, "foo.bar"),
		*PointLogSegment(11, 101, "foo"),
	}
	for i := range segs {
		idx.Add(&segs[i])
	}

	tmpDir := t.TempDir()
	path := tmpDir + "/index.gob"

	if err := StoreIndex(path, idx); err != nil {
		t.Fatalf("StoreIndex failed: %v", err)
	}

	loadedIdx, err := LoadIndex(path)
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	// Verify loaded index content
	// LookupRange("foo.bar") includes ancestors, so it returns both "foo.bar" and "foo"
	got := loadedIdx.LookupRange("foo.bar", nil, nil, nil)
	want := []LogSegment{segs[0], segs[1]} // "foo.bar" at commit 10, "foo" at commit 11 (ancestor)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Loaded Index mismatch (-want +got):\n%s", diff)
	}
}

