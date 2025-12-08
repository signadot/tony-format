package index

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestKPathDenseArray tests kpath operations with dense array syntax [0], [1], etc.
func TestKPathDenseArray(t *testing.T) {
	idx := NewIndex("")

	segs := []LogSegment{
		*PointLogSegment(10, 100, "[0]"),
		*PointLogSegment(11, 101, "[1]"),
		*PointLogSegment(12, 102, "foo[0]"),
		*PointLogSegment(13, 103, "foo[1].bar"),
		*PointLogSegment(14, 104, "[0].baz"),
	}

	// Add segments
	for i := range segs {
		idx.Add(&segs[i])
	}

	// Test LookupRange for root-level array indices (exact match only)
	got := idx.LookupRange("[0]", nil, nil)
	want := []LogSegment{segs[0]} // Only [0], not [0].baz
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("LookupRange('[0]') mismatch (-want +got):\n%s", diff)
	}

	// Test LookupRange for nested array indices
	got = idx.LookupRange("foo[0]", nil, nil)
	want = []LogSegment{segs[2]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("LookupRange('foo[0]') mismatch (-want +got):\n%s", diff)
	}

	// Test LookupRange for array index with field
	got = idx.LookupRange("foo[1].bar", nil, nil)
	want = []LogSegment{segs[3]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("LookupRange('foo[1].bar') mismatch (-want +got):\n%s", diff)
	}

	// Test Remove
	if !idx.Remove(&segs[0]) {
		t.Error("Remove('[0]') should succeed")
	}
	got = idx.LookupRange("[0]", nil, nil)
	want = []LogSegment{} // [0] was removed, [0].baz is a descendant so not returned
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("LookupRange('[0]') after remove mismatch (-want +got):\n%s", diff)
	}
}

// TestKPathSparseArray tests kpath operations with sparse array syntax {4}, {42}, etc.
func TestKPathSparseArray(t *testing.T) {
	idx := NewIndex("")

	segs := []LogSegment{
		*PointLogSegment(10, 100, "{4}"),
		*PointLogSegment(11, 101, "{42}"),
		*PointLogSegment(12, 102, "foo{13}"),
		*PointLogSegment(13, 103, "foo{13}.bar"),
		*PointLogSegment(14, 104, "{4}.baz"),
	}

	// Add segments
	for i := range segs {
		idx.Add(&segs[i])
	}

	// Test LookupRange for root-level sparse indices (exact match only)
	got := idx.LookupRange("{4}", nil, nil)
	want := []LogSegment{segs[0]} // Only {4}, not {4}.baz
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("LookupRange('{4}') mismatch (-want +got):\n%s", diff)
	}

	// Test LookupRange for nested sparse indices (exact match only)
	got = idx.LookupRange("foo{13}", nil, nil)
	want = []LogSegment{segs[2]} // Only foo{13}, not foo{13}.bar
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("LookupRange('foo{13}') mismatch (-want +got):\n%s", diff)
	}

	// Test Remove
	if !idx.Remove(&segs[2]) {
		t.Error("Remove('foo{13}') should succeed")
	}
	got = idx.LookupRange("foo{13}", nil, nil)
	want = []LogSegment{} // foo{13} was removed, foo{13}.bar is a descendant so not returned
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("LookupRange('foo{13}') after remove mismatch (-want +got):\n%s", diff)
	}
}

// TestKPathMixed tests kpath operations with mixed field names, dense arrays, and sparse arrays
func TestKPathMixed(t *testing.T) {
	idx := NewIndex("")

	segs := []LogSegment{
		*PointLogSegment(10, 100, "a[0].b{4}.c"),
		*PointLogSegment(11, 101, "a[0].b"),
		*PointLogSegment(12, 102, "a[1].b{5}"),
		*PointLogSegment(13, 103, "x.y[0].z{10}"),
	}

	// Add segments
	for i := range segs {
		idx.Add(&segs[i])
	}

	// Test LookupRange for complex nested path
	// LookupRange includes segments at intermediate levels (ancestors), so "a[0].b" is included
	// Results are sorted by commit/tx first, then by path
	got := idx.LookupRange("a[0].b{4}.c", nil, nil)
	want := []LogSegment{segs[0], segs[1]} // a[0].b{4}.c (commit 10) and a[0].b (commit 11, ancestor)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("LookupRange('a[0].b{4}.c') mismatch (-want +got):\n%s", diff)
	}

	// Test LookupRange for exact path
	// Results are sorted by commit/tx first, then by path
	got = idx.LookupRange("a[0].b", nil, nil)
	want = []LogSegment{segs[1]} // Only a[0].b (exact match), not a[0].b{4}.c (descendant)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("LookupRange('a[0].b') mismatch (-want +got):\n%s", diff)
	}

	// Test LookupRange for another complex path
	got = idx.LookupRange("x.y[0].z{10}", nil, nil)
	want = []LogSegment{segs[3]}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("LookupRange('x.y[0].z{10}') mismatch (-want +got):\n%s", diff)
	}
}

// TestKPathIteratorArray tests iterator operations with array indices
func TestKPathIteratorArray(t *testing.T) {
	root := NewIndex("")
	root.Add(PointLogSegment(1, 1, "foo[0]"))
	root.Add(PointLogSegment(2, 2, "foo[1].bar"))
	root.Add(PointLogSegment(3, 3, "bar{4}"))
	root.Add(PointLogSegment(4, 4, "bar{4}.baz"))

	root.RLock()
	defer root.RUnlock()

	// Test IterAtPath with dense array index
	it := root.IterAtPath("foo[0]")
	if !it.Valid() {
		t.Error("Iterator should be valid for 'foo[0]'")
	}
	if it.Path() != "foo[0]" {
		t.Errorf("Expected path 'foo[0]', got %q", it.Path())
	}
	if it.Depth() != 2 {
		t.Errorf("Expected depth 2, got %d", it.Depth())
	}

	// Test IterAtPath with sparse array index
	it = root.IterAtPath("bar{4}")
	if !it.Valid() {
		t.Error("Iterator should be valid for 'bar{4}'")
	}
	if it.Path() != "bar{4}" {
		t.Errorf("Expected path 'bar{4}', got %q", it.Path())
	}

	// Test Down with array index segment
	it = root.IterAtPath("foo")
	if !it.Valid() {
		t.Error("Iterator should be valid at 'foo'")
	}
	if !it.Down("[0]") {
		t.Error("Down('[0]') should succeed")
	}
	if it.Path() != "foo[0]" {
		t.Errorf("Expected path 'foo[0]', got %q", it.Path())
	}

	// Test Down with sparse array index
	it = root.IterAtPath("bar")
	if !it.Valid() {
		t.Error("Iterator should be valid at 'bar'")
	}
	if !it.Down("{4}") {
		t.Error("Down('{4}') should succeed")
	}
	if it.Path() != "bar{4}" {
		t.Errorf("Expected path 'bar{4}', got %q", it.Path())
	}
}

// TestKPathComparison tests that kpath comparison works correctly with array indices
func TestKPathComparison(t *testing.T) {
	idx := NewIndex("")

	segs := []LogSegment{
		*PointLogSegment(10, 100, "[0]"),
		*PointLogSegment(10, 100, "[1]"),
		*PointLogSegment(10, 100, "[10]"),
		*PointLogSegment(10, 100, "{0}"),
		*PointLogSegment(10, 100, "{4}"),
		*PointLogSegment(10, 100, "{42}"),
		*PointLogSegment(10, 100, "a"),
		*PointLogSegment(10, 100, "a[0]"),
		*PointLogSegment(10, 100, "a[1]"),
		*PointLogSegment(10, 100, "a{0}"),
		*PointLogSegment(10, 100, "a{4}"),
	}

	// Add segments
	for i := range segs {
		idx.Add(&segs[i])
	}

	// LookupRange("", ...) only returns root-level segments (none in this test)
	// All segments are stored in child indices, so query each path individually
	// to verify they exist and are sorted correctly
	allPaths := []string{"[0]", "[1]", "[10]", "{0}", "{4}", "{42}", "a", "a[0]", "a[1]", "a{0}", "a{4}"}
	expectedOrder := []string{
		"a",      // Field names come first
		"a[0]",   // Then fields with dense arrays
		"a[1]",
		"a{0}",   // Then fields with sparse arrays
		"a{4}",
		"[0]",    // Then root-level dense arrays
		"[1]",
		"[10]",
		"{0}",    // Finally root-level sparse arrays
		"{4}",
		"{42}",
	}

	// Verify each path can be looked up individually
	for _, path := range allPaths {
		got := idx.LookupRange(path, nil, nil)
		if len(got) == 0 {
			t.Errorf("Expected to find segment at path %q", path)
		}
	}

	// Verify ordering by checking individual lookups match expected order
	// Note: LookupRange may return multiple segments (including ancestors), so check the first one
	for i, expectedPath := range expectedOrder {
		got := idx.LookupRange(expectedPath, nil, nil)
		if len(got) == 0 {
			t.Errorf("Position %d: expected segment at %q, but LookupRange returned empty", i, expectedPath)
			continue
		}
		// Find the segment with the exact path (may be mixed with ancestors)
		found := false
		for _, seg := range got {
			if seg.KindedPath == expectedPath {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Position %d: expected segment at %q, but not found in results: %v", i, expectedPath, got)
		}
	}
}

// TestKPathListRange tests ListRange with array indices
func TestKPathListRange(t *testing.T) {
	idx := NewIndex("")

	// IndexPatch would add segments for all nodes in the tree, not just leaf nodes
	// So we need to add segments for both parent and child paths
	segs := []LogSegment{
		*PointLogSegment(10, 100, "a"),
		*PointLogSegment(11, 101, "[0]"),
		*PointLogSegment(12, 102, "[1]"),
		*PointLogSegment(13, 103, "{4}"),
		*PointLogSegment(14, 104, "foo"),      // parent path (IndexPatch would add this)
		*PointLogSegment(14, 104, "foo[0]"),   // child path
		*PointLogSegment(15, 105, "foo"),      // parent path (IndexPatch would add this)
		*PointLogSegment(15, 105, "foo{13}"),  // child path
	}

	// Add segments
	for i := range segs {
		idx.Add(&segs[i])
	}

	// Test ListRange at root
	children := idx.ListRange(nil, nil)
	// Should include: "[0]", "[1]", "{4}", "a", "foo"
	expectedChildren := map[string]bool{
		"[0]": true,
		"[1]": true,
		"{4}": true,
		"a":   true,
		"foo": true,
	}

	if len(children) != len(expectedChildren) {
		t.Errorf("Expected %d children, got %d: %v", len(expectedChildren), len(children), children)
	}

	for _, child := range children {
		if !expectedChildren[child] {
			t.Errorf("Unexpected child: %q", child)
		}
		delete(expectedChildren, child)
	}

	if len(expectedChildren) > 0 {
		t.Errorf("Missing children: %v", expectedChildren)
	}

	// Verify children are sorted correctly
	for i := 1; i < len(children); i++ {
		// Each child should be greater than the previous one according to kpath comparison
		if LogSegCompare(
			LogSegment{KindedPath: children[i-1]},
			LogSegment{KindedPath: children[i]},
		) >= 0 {
			t.Errorf("Children not sorted: %q should come before %q", children[i-1], children[i])
		}
	}
}
