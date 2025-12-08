package index

import "testing"

func TestIterAtPath(t *testing.T) {
	root := NewIndex("")
	root.Add(PointLogSegment(1, 1, ""))
	root.Add(PointLogSegment(2, 2, "foo"))
	root.Add(PointLogSegment(3, 3, "foo.bar"))

	root.RLock()
	defer root.RUnlock()

	// Test root path
	it := root.IterAtPath("")
	if !it.Valid() {
		t.Error("Iterator should be valid at root")
	}
	if it.Path() != "" {
		t.Errorf("Expected empty path, got %q", it.Path())
	}
	if it.Depth() != 0 {
		t.Errorf("Expected depth 0, got %d", it.Depth())
	}

	// Test existing path
	it = root.IterAtPath("foo")
	if !it.Valid() {
		t.Error("Iterator should be valid for existing path")
	}
	if it.Path() != "foo" {
		t.Errorf("Expected path 'foo', got %q", it.Path())
	}
	if it.Depth() != 1 {
		t.Errorf("Expected depth 1, got %d", it.Depth())
	}

	// Test nested path
	it = root.IterAtPath("foo.bar")
	if !it.Valid() {
		t.Error("Iterator should be valid for nested path")
	}
	if it.Path() != "foo.bar" {
		t.Errorf("Expected path 'foo.bar', got %q", it.Path())
	}
	if it.Depth() != 2 {
		t.Errorf("Expected depth 2, got %d", it.Depth())
	}

	// Test non-existent path
	it = root.IterAtPath("nonexistent")
	if it.Valid() {
		t.Error("Iterator should be invalid for non-existent path")
	}
}

func TestIterAtPathEmpty(t *testing.T) {
	root := NewIndex("")
	root.RLock()
	defer root.RUnlock()

	it := root.IterAtPath("")
	if !it.Valid() {
		t.Error("Iterator should be valid at root even for empty index")
	}
}

func TestIndexIteratorPath(t *testing.T) {
	root := NewIndex("")
	root.Add(PointLogSegment(1, 1, "a.b.c"))

	root.RLock()
	defer root.RUnlock()

	it := root.IterAtPath("a.b.c")
	if !it.Valid() {
		t.Error("Iterator should be valid")
	}
	if it.Path() != "a.b.c" {
		t.Errorf("Expected path 'a.b.c', got %q", it.Path())
	}
	if it.Depth() != 3 {
		t.Errorf("Expected depth 3, got %d", it.Depth())
	}
}

func TestDown(t *testing.T) {
	root := NewIndex("")
	root.Add(PointLogSegment(1, 1, "foo"))
	root.Add(PointLogSegment(2, 2, "foo.bar"))

	root.RLock()
	defer root.RUnlock()

	it := root.IterAtPath("")
	if !it.Valid() {
		t.Error("Iterator should be valid at root")
	}

	// Navigate down to existing child
	if !it.Down("foo") {
		t.Error("Down() should succeed for existing child")
	}
	if it.Path() != "foo" {
		t.Errorf("Expected path 'foo', got %q", it.Path())
	}
	if it.Depth() != 1 {
		t.Errorf("Expected depth 1, got %d", it.Depth())
	}

	// Navigate down to nested child
	if !it.Down("bar") {
		t.Error("Down() should succeed for nested child")
	}
	if it.Path() != "foo.bar" {
		t.Errorf("Expected path 'foo.bar', got %q", it.Path())
	}
	if it.Depth() != 2 {
		t.Errorf("Expected depth 2, got %d", it.Depth())
	}

	// Try to navigate to non-existent child
	if it.Down("nonexistent") {
		t.Error("Down() should fail for non-existent child")
	}
	if it.Path() != "foo.bar" {
		t.Errorf("Path should remain 'foo.bar', got %q", it.Path())
	}
}

func TestUp(t *testing.T) {
	root := NewIndex("")
	root.Add(PointLogSegment(1, 1, "foo.bar"))

	root.RLock()
	defer root.RUnlock()

	it := root.IterAtPath("foo.bar")
	if !it.Valid() {
		t.Error("Iterator should be valid")
	}

	// Navigate up to parent
	if !it.Up() {
		t.Error("Up() should succeed")
	}
	if it.Path() != "foo" {
		t.Errorf("Expected path 'foo', got %q", it.Path())
	}
	if it.Depth() != 1 {
		t.Errorf("Expected depth 1, got %d", it.Depth())
	}

	// Navigate up to root
	if !it.Up() {
		t.Error("Up() should succeed to root")
	}
	if it.Path() != "" {
		t.Errorf("Expected empty path at root, got %q", it.Path())
	}
	if it.Depth() != 0 {
		t.Errorf("Expected depth 0 at root, got %d", it.Depth())
	}

	// Try to navigate up from root
	if it.Up() {
		t.Error("Up() should fail at root")
	}
	if it.Path() != "" {
		t.Errorf("Path should remain empty at root, got %q", it.Path())
	}
}

func TestDownUpNavigation(t *testing.T) {
	root := NewIndex("")
	root.Add(PointLogSegment(1, 1, "a.b.c"))

	root.RLock()
	defer root.RUnlock()

	it := root.IterAtPath("")
	if it.Path() != "" {
		t.Errorf("Expected empty path, got %q", it.Path())
	}

	// Navigate down the hierarchy
	if !it.Down("a") {
		t.Error("Down('a') should succeed")
	}
	if it.Path() != "a" {
		t.Errorf("Expected path 'a', got %q", it.Path())
	}

	if !it.Down("b") {
		t.Error("Down('b') should succeed")
	}
	if it.Path() != "a.b" {
		t.Errorf("Expected path 'a.b', got %q", it.Path())
	}

	if !it.Down("c") {
		t.Error("Down('c') should succeed")
	}
	if it.Path() != "a.b.c" {
		t.Errorf("Expected path 'a.b.c', got %q", it.Path())
	}

	// Navigate back up
	if !it.Up() {
		t.Error("Up() should succeed")
	}
	if it.Path() != "a.b" {
		t.Errorf("Expected path 'a.b', got %q", it.Path())
	}

	if !it.Up() {
		t.Error("Up() should succeed")
	}
	if it.Path() != "a" {
		t.Errorf("Expected path 'a', got %q", it.Path())
	}

	if !it.Up() {
		t.Error("Up() should succeed")
	}
	if it.Path() != "" {
		t.Errorf("Expected empty path, got %q", it.Path())
	}
}

func TestToPath(t *testing.T) {
	root := NewIndex("")
	root.Add(PointLogSegment(1, 1, "foo"))
	root.Add(PointLogSegment(2, 2, "foo.bar"))
	root.Add(PointLogSegment(3, 3, "foo.baz"))

	root.RLock()
	defer root.RUnlock()

	it := root.IterAtPath("")

	// Navigate to root
	if !it.ToPath("") {
		t.Error("ToPath('') should succeed")
	}
	if it.Path() != "" {
		t.Errorf("Expected empty path, got %q", it.Path())
	}

	// Navigate to existing path
	if !it.ToPath("foo") {
		t.Error("ToPath('foo') should succeed")
	}
	if it.Path() != "foo" {
		t.Errorf("Expected path 'foo', got %q", it.Path())
	}

	// Navigate to nested path
	if !it.ToPath("foo.bar") {
		t.Error("ToPath('foo.bar') should succeed")
	}
	if it.Path() != "foo.bar" {
		t.Errorf("Expected path 'foo.bar', got %q", it.Path())
	}

	// Navigate to different nested path
	if !it.ToPath("foo.baz") {
		t.Error("ToPath('foo.baz') should succeed")
	}
	if it.Path() != "foo.baz" {
		t.Errorf("Expected path 'foo.baz', got %q", it.Path())
	}

	// Navigate back to root
	if !it.ToPath("") {
		t.Error("ToPath('') should succeed")
	}
	if it.Path() != "" {
		t.Errorf("Expected empty path, got %q", it.Path())
	}

	// Try non-existent path - should fail but iterator remains at current position
	if it.ToPath("nonexistent") {
		t.Error("ToPath('nonexistent') should fail")
	}
	if it.Path() != "" {
		t.Errorf("Path should remain at root '', got %q", it.Path())
	}
	if !it.Valid() {
		t.Error("Iterator should remain valid at root after failed ToPath")
	}
}

func TestToPathFromAnywhere(t *testing.T) {
	root := NewIndex("")
	root.Add(PointLogSegment(1, 1, "a.b.c"))
	root.Add(PointLogSegment(2, 2, "x.y.z"))

	root.RLock()
	defer root.RUnlock()

	// Start at one path
	it := root.IterAtPath("a.b.c")
	if it.Path() != "a.b.c" {
		t.Errorf("Expected path 'a.b.c', got %q", it.Path())
	}

	// Jump to completely different path
	if !it.ToPath("x.y.z") {
		t.Error("ToPath('x.y.z') should succeed")
	}
	if it.Path() != "x.y.z" {
		t.Errorf("Expected path 'x.y.z', got %q", it.Path())
	}

	// Jump back to original path
	if !it.ToPath("a.b.c") {
		t.Error("ToPath('a.b.c') should succeed")
	}
	if it.Path() != "a.b.c" {
		t.Errorf("Expected path 'a.b.c', got %q", it.Path())
	}
}

func TestIndexIteratorCommitsAscending(t *testing.T) {
	root := NewIndex("")
	root.Add(PointLogSegment(10, 1, ""))
	root.Add(PointLogSegment(20, 2, ""))
	root.Add(PointLogSegment(30, 3, ""))

	root.RLock()
	defer root.RUnlock()

	it := root.IterAtPath("")
	collected := []LogSegment{}
	for seg := range it.Commits(Up) {
		collected = append(collected, seg)
	}

	if len(collected) != 3 {
		t.Errorf("Expected 3 segments, got %d", len(collected))
	}
	// With new semantics: StartCommit = LastCommit, EndCommit = Commit
	// PointLogSegment(10, 1, "") creates StartCommit=9, EndCommit=10
	if len(collected) > 0 && collected[0].EndCommit != 10 {
		t.Errorf("Expected first commit 10, got %d", collected[0].EndCommit)
	}
	if len(collected) > 1 && collected[1].EndCommit != 20 {
		t.Errorf("Expected second commit 20, got %d", collected[1].EndCommit)
	}
	if len(collected) > 2 && collected[2].EndCommit != 30 {
		t.Errorf("Expected third commit 30, got %d", collected[2].EndCommit)
	}
}

func TestIndexIteratorCommitsDescending(t *testing.T) {
	root := NewIndex("")
	root.Add(PointLogSegment(10, 1, ""))
	root.Add(PointLogSegment(20, 2, ""))
	root.Add(PointLogSegment(30, 3, ""))

	root.RLock()
	defer root.RUnlock()

	it := root.IterAtPath("")
	collected := []LogSegment{}
	for seg := range it.Commits(Down) {
		collected = append(collected, seg)
	}

	if len(collected) != 3 {
		t.Errorf("Expected 3 segments, got %d", len(collected))
	}
	// With new semantics: StartCommit = LastCommit, EndCommit = Commit
	if len(collected) > 0 && collected[0].EndCommit != 30 {
		t.Errorf("Expected first commit 30, got %d", collected[0].EndCommit)
	}
	if len(collected) > 1 && collected[1].EndCommit != 20 {
		t.Errorf("Expected second commit 20, got %d", collected[1].EndCommit)
	}
	if len(collected) > 2 && collected[2].EndCommit != 10 {
		t.Errorf("Expected third commit 10, got %d", collected[2].EndCommit)
	}
}

func TestIndexIteratorCommitsEmpty(t *testing.T) {
	root := NewIndex("")

	root.RLock()
	defer root.RUnlock()

	it := root.IterAtPath("")
	collected := []LogSegment{}
	for seg := range it.Commits(Up) {
		collected = append(collected, seg)
	}

	if len(collected) != 0 {
		t.Errorf("Expected 0 segments, got %d", len(collected))
	}
}

func TestIndexIteratorCommitsAtNestedPath(t *testing.T) {
	root := NewIndex("")
	root.Add(PointLogSegment(10, 1, "foo"))
	root.Add(PointLogSegment(20, 2, "foo"))
	root.Add(PointLogSegment(30, 3, "bar"))

	root.RLock()
	defer root.RUnlock()

	it := root.IterAtPath("foo")
	collected := []LogSegment{}
	for seg := range it.Commits(Up) {
		collected = append(collected, seg)
	}

	if len(collected) != 2 {
		t.Errorf("Expected 2 segments at 'foo', got %d", len(collected))
	}
	// With new semantics: StartCommit = LastCommit, EndCommit = Commit
	if len(collected) > 0 && collected[0].EndCommit != 10 {
		t.Errorf("Expected first commit 10, got %d", collected[0].EndCommit)
	}
	if len(collected) > 1 && collected[1].EndCommit != 20 {
		t.Errorf("Expected second commit 20, got %d", collected[1].EndCommit)
	}
}

func TestIndexIteratorCommitsAt(t *testing.T) {
	root := NewIndex("")
	root.Add(PointLogSegment(10, 1, ""))
	root.Add(PointLogSegment(20, 2, ""))
	root.Add(PointLogSegment(30, 3, ""))
	root.Add(PointLogSegment(40, 4, ""))

	root.RLock()
	defer root.RUnlock()

	it := root.IterAtPath("")

	// Test CommitsAt with commit 30, descending - should start at 30 and go down
	collected := []LogSegment{}
	for seg := range it.CommitsAt(30, Down) {
		collected = append(collected, seg)
	}

	if len(collected) != 3 {
		t.Errorf("Expected 3 segments (30, 20, 10), got %d", len(collected))
	}
	// With new semantics: StartCommit = LastCommit, EndCommit = Commit
	if len(collected) > 0 && collected[0].EndCommit != 30 {
		t.Errorf("Expected first commit 30, got %d", collected[0].EndCommit)
	}
	if len(collected) > 1 && collected[1].EndCommit != 20 {
		t.Errorf("Expected second commit 20, got %d", collected[1].EndCommit)
	}
	if len(collected) > 2 && collected[2].EndCommit != 10 {
		t.Errorf("Expected third commit 10, got %d", collected[2].EndCommit)
	}

	// Test CommitsAt with commit 25, descending - should start at 20 (first <= 25)
	collected = []LogSegment{}
	for seg := range it.CommitsAt(25, Down) {
		collected = append(collected, seg)
	}

	if len(collected) != 2 {
		t.Errorf("Expected 2 segments (20, 10), got %d", len(collected))
	}
	// With new semantics: StartCommit = LastCommit, EndCommit = Commit
	if len(collected) > 0 && collected[0].EndCommit != 20 {
		t.Errorf("Expected first commit 20, got %d", collected[0].EndCommit)
	}
	if len(collected) > 1 && collected[1].EndCommit != 10 {
		t.Errorf("Expected second commit 10, got %d", collected[1].EndCommit)
	}

	// Test CommitsAt with commit 30, ascending
	// With new semantics: segment at commit 30 has StartCommit=29, EndCommit=30
	// CommitsAt(30, Up) checks StartCommit >= 30, so it won't find the segment at commit 30
	// It will find the segment at commit 40 (StartCommit=39, EndCommit=40) if 39 >= 30, but that's false too
	// Actually, CommitsAt seeks to StartCommit >= commit, so it should find segments where StartCommit >= 30
	// Segment at commit 30: StartCommit=29, EndCommit=30 (29 >= 30 is false, so skipped)
	// Segment at commit 40: StartCommit=39, EndCommit=40 (39 >= 30 is true, so included)
	// So we should query for commit 29 to find the segment at commit 30
	collected = []LogSegment{}
	for seg := range it.CommitsAt(29, Up) {
		collected = append(collected, seg)
	}

	if len(collected) != 2 {
		t.Errorf("Expected 2 segments (30, 40), got %d", len(collected))
	}
	// With new semantics: StartCommit = LastCommit, EndCommit = Commit
	if len(collected) > 0 && collected[0].EndCommit != 30 {
		t.Errorf("Expected first commit 30, got %d", collected[0].EndCommit)
	}
	if len(collected) > 1 && collected[1].EndCommit != 40 {
		t.Errorf("Expected second commit 40, got %d", collected[1].EndCommit)
	}

	// Test CommitsAt with commit 5, descending - should return empty (no segments <= 5)
	collected = []LogSegment{}
	for seg := range it.CommitsAt(5, Down) {
		collected = append(collected, seg)
	}

	if len(collected) != 0 {
		t.Errorf("Expected 0 segments, got %d", len(collected))
	}
}

func TestIndexIteratorCommitsEarlyReturn(t *testing.T) {
	root := NewIndex("")
	root.Add(PointLogSegment(10, 1, ""))
	root.Add(PointLogSegment(20, 2, ""))
	root.Add(PointLogSegment(30, 3, ""))

	root.RLock()
	defer root.RUnlock()

	it := root.IterAtPath("")
	collected := []LogSegment{}
	count := 0
	for seg := range it.Commits(Up) {
		collected = append(collected, seg)
		count++
		if count >= 2 {
			break
		}
	}

	if len(collected) != 2 {
		t.Errorf("Expected 2 segments, got %d", len(collected))
	}
}

func TestIndexIteratorFullWorkflow(t *testing.T) {
	root := NewIndex("")
	// Add commits at root
	root.Add(PointLogSegment(10, 1, ""))
	root.Add(PointLogSegment(20, 2, ""))
	// Add commits at nested paths
	root.Add(PointLogSegment(30, 3, "foo"))
	root.Add(PointLogSegment(40, 4, "foo"))
	root.Add(PointLogSegment(50, 5, "foo.bar"))
	root.Add(PointLogSegment(60, 6, "foo.bar"))

	root.RLock()
	defer root.RUnlock()

	// Start at root
	it := root.IterAtPath("")
	if it.Path() != "" {
		t.Errorf("Expected empty path, got %q", it.Path())
	}

	// Iterate commits at root
	rootCommits := []LogSegment{}
	for seg := range it.Commits(Up) {
		rootCommits = append(rootCommits, seg)
	}
	if len(rootCommits) != 2 {
		t.Errorf("Expected 2 commits at root, got %d", len(rootCommits))
	}

	// Navigate down to foo
	if !it.Down("foo") {
		t.Error("Down('foo') should succeed")
	}
	if it.Path() != "foo" {
		t.Errorf("Expected path 'foo', got %q", it.Path())
	}

	// Iterate commits at foo
	fooCommits := []LogSegment{}
	for seg := range it.Commits(Up) {
		fooCommits = append(fooCommits, seg)
	}
	if len(fooCommits) != 2 {
		t.Errorf("Expected 2 commits at 'foo', got %d", len(fooCommits))
	}

	// Navigate down to bar
	if !it.Down("bar") {
		t.Error("Down('bar') should succeed")
	}
	if it.Path() != "foo.bar" {
		t.Errorf("Expected path 'foo.bar', got %q", it.Path())
	}

	// Iterate commits at foo.bar
	barCommits := []LogSegment{}
	for seg := range it.Commits(Up) {
		barCommits = append(barCommits, seg)
	}
	if len(barCommits) != 2 {
		t.Errorf("Expected 2 commits at 'foo.bar', got %d", len(barCommits))
	}

	// Navigate back up
	if !it.Up() {
		t.Error("Up() should succeed")
	}
	if it.Path() != "foo" {
		t.Errorf("Expected path 'foo', got %q", it.Path())
	}

	// Jump to different path using ToPath
	if !it.ToPath("foo.bar") {
		t.Error("ToPath('foo.bar') should succeed")
	}
	if it.Path() != "foo.bar" {
		t.Errorf("Expected path 'foo.bar', got %q", it.Path())
	}
}

func TestIndexIteratorMultiplePaths(t *testing.T) {
	root := NewIndex("")
	root.Add(PointLogSegment(10, 1, "a"))
	root.Add(PointLogSegment(20, 2, "a/b"))
	root.Add(PointLogSegment(30, 3, "x"))
	root.Add(PointLogSegment(40, 4, "x/y"))

	root.RLock()
	defer root.RUnlock()

	it := root.IterAtPath("")

	// Navigate to a/b
	if !it.ToPath("a/b") {
		t.Error("ToPath('a/b') should succeed")
	}
	commits := []LogSegment{}
	for seg := range it.Commits(Up) {
		commits = append(commits, seg)
	}
	// With new semantics: StartCommit = LastCommit, EndCommit = Commit
	if len(commits) != 1 || commits[0].EndCommit != 20 {
		t.Errorf("Expected 1 commit with EndCommit 20, got %d commits", len(commits))
	}

	// Jump to completely different path
	if !it.ToPath("x/y") {
		t.Error("ToPath('x/y') should succeed")
	}
	commits = []LogSegment{}
	for seg := range it.Commits(Up) {
		commits = append(commits, seg)
	}
	if len(commits) != 1 || commits[0].EndCommit != 40 {
		t.Errorf("Expected 1 commit with EndCommit 40, got %d commits", len(commits))
	}
}

func TestIndexIteratorConcurrentIterators(t *testing.T) {
	root := NewIndex("")
	root.Add(PointLogSegment(10, 1, "foo"))
	root.Add(PointLogSegment(20, 2, "bar"))

	root.RLock()
	defer root.RUnlock()

	// Create multiple iterators
	it1 := root.IterAtPath("foo")
	it2 := root.IterAtPath("bar")
	it3 := root.IterAtPath("")

	// All should work independently
	if it1.Path() != "foo" {
		t.Errorf("it1: expected path 'foo', got %q", it1.Path())
	}
	if it2.Path() != "bar" {
		t.Errorf("it2: expected path 'bar', got %q", it2.Path())
	}
	if it3.Path() != "" {
		t.Errorf("it3: expected empty path, got %q", it3.Path())
	}

	// Iterate with different iterators
	commits1 := []LogSegment{}
	for seg := range it1.Commits(Up) {
		commits1 = append(commits1, seg)
	}
	if len(commits1) != 1 {
		t.Errorf("it1: expected 1 commit, got %d", len(commits1))
	}

	commits2 := []LogSegment{}
	for seg := range it2.Commits(Up) {
		commits2 = append(commits2, seg)
	}
	if len(commits2) != 1 {
		t.Errorf("it2: expected 1 commit, got %d", len(commits2))
	}

	// Navigate it1 independently
	if !it1.Up() {
		t.Error("it1.Up() should succeed")
	}
	if it1.Path() != "" {
		t.Errorf("it1: expected empty path, got %q", it1.Path())
	}
	// it2 should still be at bar
	if it2.Path() != "bar" {
		t.Errorf("it2: expected path 'bar', got %q", it2.Path())
	}
}
