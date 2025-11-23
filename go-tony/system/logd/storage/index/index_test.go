package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/signadot/tony-format/go-tony/gomap"
)

func TestIndexAddLookupPointDiff(t *testing.T) {
	idx := NewIndex("")

	segs := []LogSegment{
		*PointLogSegment(10, 100, "foo/bar"),
		*PointLogSegment(11, 101, "foo"),
		*PointLogSegment(12, 102, "foo/bar/baz"),
		*PointLogSegment(13, 103, "qux"),
	}

	// 1. Test Add
	// Add some commits at different paths
	for i := range segs {
		idx.Add(&segs[i])
	}

	// 2. Test LookupRange (Ancestors + Descendants)

	got := idx.LookupRange("foo/bar", nil, nil)

	want := segs[:3]

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("LookupRange mismatch (-want +got):\n%s", diff)
	}
	rootAll := idx.LookupRange("", nil, nil)
	d, err := gomap.ToTony(rootAll)
	if err != nil {
		panic(err)
	}
	t.Logf("rootAll:\n%s", d)
}

func TestIndexRemove(t *testing.T) {
	idx := NewIndex("")

	segs := []LogSegment{
		*PointLogSegment(10, 100, "foo/bar"),
		*PointLogSegment(11, 101, "foo"),
		*PointLogSegment(12, 102, "foo/bar/baz"),
		*PointLogSegment(13, 103, "qux"),
	}

	// 1. Test Add
	// Add some commits at different paths
	for i := range segs {
		idx.Add(&segs[i])
	}

	// 3. Test Replace
	// Replace {10, 100, ""} at "foo/bar" with a compacted range.
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
		RelPath: "foo",
	}

	idx.Add(compactRange)

	gotAfter := idx.LookupRange("", nil, nil)

	wantAfter := []LogSegment{
		*compactRange,
		*PointLogSegment(13, 103, "qux"),
	}

	if diff := cmp.Diff(wantAfter, gotAfter); diff != "" {
		t.Errorf("LookupRange after Replace mismatch (-want +got):\n%s", diff)
	}
}

func TestIndexPersist(t *testing.T) {
	idx := NewIndex("")
	segs := []LogSegment{
		*PointLogSegment(10, 100, "foo/bar"),
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
	got := loadedIdx.LookupRange("foo/bar", nil, nil)
	want := segs // Same as added
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Loaded Index mismatch (-want +got):\n%s", diff)
	}
}

func TestIndexBuild(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files simulating logd structure:
	// - Root level files
	// - Files in "children" subdirectories (logd organizational structure)
	// - Files that should be ignored

	createFile := func(name string) {
		path := filepath.Join(tmpDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("dummy"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	createFile("c10-a100.diff")
	createFile("c11-a101.diff")
	createFile("ignored.txt")
	// Simulate logd structure: paths/children/foo/c12-a102.diff
	createFile("children/foo/c12-a102.diff")

	// Extraction closure - responsible for filtering and path mapping
	extract := func(path string) (*LogSegment, error) {
		base := filepath.Base(path)

		// Ignore non-diff files
		if base == "ignored.txt" {
			return nil, nil
		}

		// Simple extraction for test - in real usage, would use FS.ParseLogSegment
		// and FS.FilesystemToPath to handle the path mapping
		if base == "c10-a100.diff" {
			return PointLogSegment(10, 100, ""), nil
		}
		if base == "c11-a101.diff" {
			return PointLogSegment(11, 101, ""), nil
		}
		if base == "c12-a102.diff" {
			// This file is in children/foo/ which maps to virtual path "/foo"
			return PointLogSegment(12, 102, "foo"), nil
		}
		return nil, nil
	}

	idx, err := Build(tmpDir, extract)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Verify all files were processed correctly
	allSegs := idx.LookupRange("", nil, nil)
	if len(allSegs) != 3 {
		t.Errorf("Expected 3 segments, got %d", len(allSegs))
	}

	// Verify the file in children/foo/ was mapped to "foo" path
	fooSegs := idx.LookupRange("foo", nil, nil)
	if len(fooSegs) != 1 {
		t.Errorf("Expected 1 segment at 'foo', got %d", len(fooSegs))
	}
}
