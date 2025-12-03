package index

import "testing"

func TestDirectionConstants(t *testing.T) {
	if Up != 0 {
		t.Errorf("Expected Up to be 0, got %d", Up)
	}
	if Down != 1 {
		t.Errorf("Expected Down to be 1, got %d", Down)
	}
}

func TestIteratorCreation(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)

	// Create iterator - should not panic
	iter := &Iterator[int]{
		tree:      tree,
		stack:     make([]iteratorFrame[int], 0, 16),
		ascending: true,
		valid:     false,
	}

	if iter.tree != tree {
		t.Error("Iterator tree reference incorrect")
	}
	if iter.ascending != true {
		t.Error("Iterator ascending flag incorrect")
	}
	if iter.valid != false {
		t.Error("Iterator should start invalid")
	}
	if len(iter.stack) != 0 {
		t.Error("Iterator stack should be empty initially")
	}
}

func TestIteratorFrame(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	n := newLeaf[int](less)

	frame := iteratorFrame[int]{
		node:  n,
		index: 0,
	}

	if frame.node != n {
		t.Error("Frame node reference incorrect")
	}
	if frame.index != 0 {
		t.Error("Frame index should be 0")
	}
}

func TestIteratorSeekToFirst(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)

	// Empty tree
	iter := &Iterator[int]{tree: tree}
	iter.seekToFirst()
	if iter.valid {
		t.Error("Iterator should be invalid for empty tree")
	}

	// Single element
	tree.Insert(42)
	iter = &Iterator[int]{tree: tree}
	iter.seekToFirst()
	if !iter.valid {
		t.Error("Iterator should be valid for single element tree")
	}
	if iter.current != 42 {
		t.Errorf("Expected current to be 42, got %d", iter.current)
	}

	// Multiple elements
	tree.Insert(10)
	tree.Insert(30)
	tree.Insert(20)
	iter = &Iterator[int]{tree: tree}
	iter.seekToFirst()
	if !iter.valid {
		t.Error("Iterator should be valid")
	}
	if iter.current != 10 {
		t.Errorf("Expected current to be 10 (minimum), got %d", iter.current)
	}
}

func TestIteratorSeekToLast(t *testing.T) {
	less := func(a, b int) bool { return a < b }

	// Empty tree
	tree := NewTree(less)
	iter := &Iterator[int]{tree: tree}
	iter.seekToLast()
	if iter.valid {
		t.Error("Iterator should be invalid for empty tree")
	}

	// Single element
	tree = NewTree(less)
	tree.Insert(42)
	iter = &Iterator[int]{tree: tree}
	iter.seekToLast()
	if !iter.valid {
		t.Error("Iterator should be valid for single element tree")
	}
	if iter.current != 42 {
		t.Errorf("Expected current to be 42, got %d", iter.current)
	}

	// Multiple elements
	tree = NewTree(less)
	tree.Insert(10)
	tree.Insert(30)
	tree.Insert(20)
	iter = &Iterator[int]{tree: tree}
	iter.seekToLast()
	if !iter.valid {
		t.Error("Iterator should be valid")
	}
	if iter.current != 30 {
		t.Errorf("Expected current to be 30 (maximum), got %d", iter.current)
	}
}

func TestIteratorSeekEmptyTree(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)

	iter := &Iterator[int]{tree: tree}
	iter.seekToFirst()
	if iter.valid {
		t.Error("Empty tree should produce invalid iterator")
	}

	iter.seekToLast()
	if iter.valid {
		t.Error("Empty tree should produce invalid iterator")
	}
}

func TestIteratorSeekSingleElement(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(100)

	iter := &Iterator[int]{tree: tree}
	iter.seekToFirst()
	if !iter.valid || iter.current != 100 {
		t.Errorf("SeekToFirst failed: valid=%v, current=%d", iter.valid, iter.current)
	}

	iter.seekToLast()
	if !iter.valid || iter.current != 100 {
		t.Errorf("SeekToLast failed: valid=%v, current=%d", iter.valid, iter.current)
	}
}

func TestIterAscending(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)

	// Empty tree
	iter := tree.IterAscending()
	if iter.valid {
		t.Error("Iterator should be invalid for empty tree")
	}
	if !iter.ascending {
		t.Error("Iterator should be ascending")
	}

	// Single element
	tree.Insert(42)
	iter = tree.IterAscending()
	if !iter.valid {
		t.Error("Iterator should be valid")
	}
	if iter.current != 42 {
		t.Errorf("Expected current to be 42, got %d", iter.current)
	}

	// Multiple elements
	tree = NewTree(less)
	tree.Insert(10)
	tree.Insert(30)
	tree.Insert(20)
	iter = tree.IterAscending()
	if !iter.valid {
		t.Error("Iterator should be valid")
	}
	if iter.current != 10 {
		t.Errorf("Expected current to be 10 (minimum), got %d", iter.current)
	}
}

func TestIterDescending(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)

	// Empty tree
	iter := tree.IterDescending()
	if iter.valid {
		t.Error("Iterator should be invalid for empty tree")
	}
	if iter.ascending {
		t.Error("Iterator should be descending")
	}

	// Single element
	tree.Insert(42)
	iter = tree.IterDescending()
	if !iter.valid {
		t.Error("Iterator should be valid")
	}
	if iter.current != 42 {
		t.Errorf("Expected current to be 42, got %d", iter.current)
	}

	// Multiple elements
	tree = NewTree(less)
	tree.Insert(10)
	tree.Insert(30)
	tree.Insert(20)
	iter = tree.IterDescending()
	if !iter.valid {
		t.Error("Iterator should be valid")
	}
	if iter.current != 30 {
		t.Errorf("Expected current to be 30 (maximum), got %d", iter.current)
	}
}

func TestIterAscendingEmpty(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	iter := tree.IterAscending()
	if iter.valid {
		t.Error("Empty tree should produce invalid iterator")
	}
}

func TestIterDescendingEmpty(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	iter := tree.IterDescending()
	if iter.valid {
		t.Error("Empty tree should produce invalid iterator")
	}
}

func TestNextAscending(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)

	// Single element - iterator starts at the element, Next() should return false
	tree.Insert(42)
	iter := tree.IterAscending()
	if iter.current != 42 {
		t.Errorf("Expected current to be 42, got %d", iter.current)
	}
	if iter.Next() {
		t.Error("Next() should return false for single element")
	}
	if iter.valid {
		t.Error("Iterator should be invalid after Next() on single element")
	}

	// Two elements
	tree = NewTree(less)
	tree.Insert(10)
	tree.Insert(20)
	iter = tree.IterAscending()
	if iter.current != 10 {
		t.Errorf("Expected current to be 10, got %d", iter.current)
	}
	if !iter.Next() {
		t.Error("Next() should return true")
	}
	if iter.current != 20 {
		t.Errorf("Expected current to be 20, got %d", iter.current)
	}
	if iter.Next() {
		t.Error("Next() should return false at end")
	}
}

func TestNextAscendingMultipleElements(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	elements := []int{10, 20, 30, 40, 50}
	for _, e := range elements {
		tree.Insert(e)
	}

	iter := tree.IterAscending()
	collected := []int{iter.current}
	for iter.Next() {
		collected = append(collected, iter.current)
	}

	if len(collected) != len(elements) {
		t.Errorf("Expected %d elements, got %d", len(elements), len(collected))
	}
	for i, expected := range elements {
		if collected[i] != expected {
			t.Errorf("Position %d: expected %d, got %d", i, expected, collected[i])
		}
	}
}

func TestNextAscendingEndOfTree(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(10)
	tree.Insert(20)

	iter := tree.IterAscending()
	// Advance to end
	iter.Next() // Now at 20
	if iter.Next() {
		t.Error("Next() should return false at end")
	}
	if iter.valid {
		t.Error("Iterator should be invalid at end")
	}
}

func TestNextAscendingSingleElement(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(42)

	iter := tree.IterAscending()
	if iter.current != 42 {
		t.Errorf("Expected current to be 42, got %d", iter.current)
	}
	if iter.Next() {
		t.Error("Next() should return false for single element")
	}
	if iter.valid {
		t.Error("Iterator should be invalid after Next()")
	}
}

func TestNextDescending(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)

	// Single element
	tree.Insert(42)
	iter := tree.IterDescending()
	if iter.current != 42 {
		t.Errorf("Expected current to be 42, got %d", iter.current)
	}
	if iter.Next() {
		t.Error("Next() should return false for single element")
	}
	if iter.valid {
		t.Error("Iterator should be invalid after Next()")
	}

	// Two elements
	tree = NewTree(less)
	tree.Insert(10)
	tree.Insert(20)
	iter = tree.IterDescending()
	if iter.current != 20 {
		t.Errorf("Expected current to be 20 (max), got %d", iter.current)
	}
	if !iter.Next() {
		t.Error("Next() should return true")
	}
	if iter.current != 10 {
		t.Errorf("Expected current to be 10, got %d", iter.current)
	}
	if iter.Next() {
		t.Error("Next() should return false at end")
	}
}

func TestNextDescendingMultipleElements(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	elements := []int{10, 20, 30, 40, 50}
	for _, e := range elements {
		tree.Insert(e)
	}

	iter := tree.IterDescending()
	collected := []int{iter.current}
	for iter.Next() {
		collected = append(collected, iter.current)
	}

	// Should be in reverse order
	expected := []int{50, 40, 30, 20, 10}
	if len(collected) != len(expected) {
		t.Errorf("Expected %d elements, got %d", len(expected), len(collected))
	}
	for i, exp := range expected {
		if collected[i] != exp {
			t.Errorf("Position %d: expected %d, got %d", i, exp, collected[i])
		}
	}
}

func TestNextDescendingEndOfTree(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(10)
	tree.Insert(20)

	iter := tree.IterDescending()
	// Advance to end (beginning)
	iter.Next() // Now at 10
	if iter.Next() {
		t.Error("Next() should return false at end")
	}
	if iter.valid {
		t.Error("Iterator should be invalid at end")
	}
}

func TestNextDescendingSingleElement(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(42)

	iter := tree.IterDescending()
	if iter.current != 42 {
		t.Errorf("Expected current to be 42, got %d", iter.current)
	}
	if iter.Next() {
		t.Error("Next() should return false for single element")
	}
	if iter.valid {
		t.Error("Iterator should be invalid after Next()")
	}
}

func TestValue(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(42)

	iter := tree.IterAscending()
	if iter.Value() != 42 {
		t.Errorf("Expected Value() to return 42, got %d", iter.Value())
	}
}

func TestValuePanicWhenInvalid(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)

	iter := tree.IterAscending()
	// Empty tree, iterator is invalid
	defer func() {
		if r := recover(); r == nil {
			t.Error("Value() should panic when iterator is invalid")
		}
	}()
	iter.Value()
}

func TestValid(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)

	// Empty tree
	iter := tree.IterAscending()
	if iter.Valid() {
		t.Error("Iterator should be invalid for empty tree")
	}

	// Valid tree
	tree.Insert(42)
	iter = tree.IterAscending()
	if !iter.Valid() {
		t.Error("Iterator should be valid")
	}
}

func TestValidAfterNext(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(10)
	tree.Insert(20)

	iter := tree.IterAscending()
	if !iter.Valid() {
		t.Error("Iterator should be valid initially")
	}

	iter.Next() // Move to second element
	if !iter.Valid() {
		t.Error("Iterator should still be valid after Next()")
	}

	iter.Next() // Move past end
	if iter.Valid() {
		t.Error("Iterator should be invalid after Next() at end")
	}
}

func TestSeekAscending(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(10)
	tree.Insert(20)
	tree.Insert(30)
	tree.Insert(40)

	iter := tree.IterAscending()
	// Seek to exact match
	if !iter.Seek(20) {
		t.Error("Seek should succeed for exact match")
	}
	if iter.Value() != 20 {
		t.Errorf("Expected Value() to be 20, got %d", iter.Value())
	}

	// Seek to non-existent value (should find next)
	if !iter.Seek(25) {
		t.Error("Seek should succeed, finding next element")
	}
	if iter.Value() != 30 {
		t.Errorf("Expected Value() to be 30 (next >= 25), got %d", iter.Value())
	}
}

func TestSeekDescending(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(10)
	tree.Insert(20)
	tree.Insert(30)
	tree.Insert(40)

	iter := tree.IterDescending()
	// Seek to exact match
	if !iter.Seek(30) {
		t.Error("Seek should succeed for exact match")
	}
	if iter.Value() != 30 {
		t.Errorf("Expected Value() to be 30, got %d", iter.Value())
	}

	// Seek to non-existent value (should find previous)
	if !iter.Seek(25) {
		t.Error("Seek should succeed, finding previous element")
	}
	if iter.Value() != 20 {
		t.Errorf("Expected Value() to be 20 (last <= 25), got %d", iter.Value())
	}
}

func TestSeekExactMatch(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(10)
	tree.Insert(20)
	tree.Insert(30)

	iter := tree.IterAscending()
	if !iter.Seek(20) {
		t.Error("Seek should find exact match")
	}
	if iter.Value() != 20 {
		t.Errorf("Expected 20, got %d", iter.Value())
	}
}

func TestSeekNoMatch(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(10)
	tree.Insert(30)

	iter := tree.IterAscending()
	// Seek to value between elements
	if !iter.Seek(20) {
		t.Error("Seek should find next element")
	}
	if iter.Value() != 30 {
		t.Errorf("Expected 30 (next >= 20), got %d", iter.Value())
	}
}

func TestSeekBeforeAll(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(10)
	tree.Insert(20)

	iter := tree.IterAscending()
	if !iter.Seek(5) {
		t.Error("Seek should find first element")
	}
	if iter.Value() != 10 {
		t.Errorf("Expected 10 (first >= 5), got %d", iter.Value())
	}
}

func TestSeekAfterAll(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(10)
	tree.Insert(20)

	iter := tree.IterAscending()
	if iter.Seek(100) {
		t.Error("Seek should fail when target > all elements")
	}
	if iter.Valid() {
		t.Error("Iterator should be invalid")
	}
}

func TestIterSeek(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(10)
	tree.Insert(20)
	tree.Insert(30)

	// Ascending seek
	iter := tree.IterSeek(20, true)
	if !iter.Valid() {
		t.Error("Iterator should be valid")
	}
	if iter.Value() != 20 {
		t.Errorf("Expected 20, got %d", iter.Value())
	}
	if !iter.ascending {
		t.Error("Iterator should be ascending")
	}

	// Descending seek
	iter = tree.IterSeek(20, false)
	if !iter.Valid() {
		t.Error("Iterator should be valid")
	}
	if iter.Value() != 20 {
		t.Errorf("Expected 20, got %d", iter.Value())
	}
	if iter.ascending {
		t.Error("Iterator should be descending")
	}
}

func TestIterRange(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(10)
	tree.Insert(20)
	tree.Insert(30)
	tree.Insert(40)

	// Range function: returns 0 for values between 20 and 30 (inclusive)
	rangeFunc := func(v int) int {
		if v < 20 {
			return -1
		}
		if v > 30 {
			return 1
		}
		return 0
	}

	// Ascending range
	iter := tree.IterRange(rangeFunc, true)
	if !iter.Valid() {
		t.Error("Iterator should be valid")
	}
	if iter.Value() != 20 {
		t.Errorf("Expected 20 (first in range), got %d", iter.Value())
	}

	// Descending range
	iter = tree.IterRange(rangeFunc, false)
	if !iter.Valid() {
		t.Error("Iterator should be valid")
	}
	if iter.Value() != 30 {
		t.Errorf("Expected 30 (last in range), got %d", iter.Value())
	}
}

func TestIterRangeEmpty(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)

	rangeFunc := func(v int) int {
		return 0 // Match all
	}

	iter := tree.IterRange(rangeFunc, true)
	if iter.Valid() {
		t.Error("Iterator should be invalid for empty tree")
	}
}

func TestIterRangePartial(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(10)
	tree.Insert(20)
	tree.Insert(30)
	tree.Insert(40)

	// Range function: only matches 25 (which doesn't exist)
	rangeFunc := func(v int) int {
		if v < 25 {
			return -1
		}
		if v > 25 {
			return 1
		}
		return 0
	}

	iter := tree.IterRange(rangeFunc, true)
	if iter.Valid() {
		t.Error("Iterator should be invalid when no elements match range")
	}
}

func TestIteratorFullTraversalAscending(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	elements := []int{50, 20, 80, 10, 30, 70, 90, 5, 15, 25, 35, 65, 75, 85, 95}
	for _, e := range elements {
		tree.Insert(e)
	}

	iter := tree.IterAscending()
	collected := []int{iter.Value()}
	count := 1
	for iter.Next() {
		collected = append(collected, iter.Value())
		count++
	}

	if count != len(elements) {
		t.Errorf("Expected %d elements, got %d", len(elements), count)
	}

	// Verify ascending order
	for i := 1; i < len(collected); i++ {
		if collected[i] <= collected[i-1] {
			t.Errorf("Not in ascending order: %d <= %d", collected[i], collected[i-1])
		}
	}
}

func TestIteratorFullTraversalDescending(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	elements := []int{50, 20, 80, 10, 30, 70, 90}
	for _, e := range elements {
		tree.Insert(e)
	}

	iter := tree.IterDescending()
	collected := []int{iter.Value()}
	count := 1
	for iter.Next() {
		collected = append(collected, iter.Value())
		count++
	}

	if count != len(elements) {
		t.Errorf("Expected %d elements, got %d", len(elements), count)
	}

	// Verify descending order
	for i := 1; i < len(collected); i++ {
		if collected[i] >= collected[i-1] {
			t.Errorf("Not in descending order: %d >= %d", collected[i], collected[i-1])
		}
	}
}

func TestIteratorSeekAndIterate(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	elements := []int{10, 20, 30, 40, 50, 60, 70, 80, 90}
	for _, e := range elements {
		tree.Insert(e)
	}

	// Seek to middle and iterate forward
	iter := tree.IterAscending()
	if !iter.Seek(45) {
		t.Error("Seek should succeed")
	}
	if iter.Value() != 50 {
		t.Errorf("Expected 50 (first >= 45), got %d", iter.Value())
	}

	// Continue iterating
	remaining := []int{}
	for iter.Next() {
		remaining = append(remaining, iter.Value())
	}

	expected := []int{60, 70, 80, 90}
	if len(remaining) != len(expected) {
		t.Errorf("Expected %d remaining elements, got %d", len(expected), len(remaining))
	}
	for i, exp := range expected {
		if remaining[i] != exp {
			t.Errorf("Position %d: expected %d, got %d", i, exp, remaining[i])
		}
	}
}

func TestIteratorConcurrentAccess(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	elements := []int{10, 20, 30, 40, 50}
	for _, e := range elements {
		tree.Insert(e)
	}

	// Create multiple iterators
	iter1 := tree.IterAscending()
	iter2 := tree.IterDescending()
	iter3 := tree.IterAscending()

	// All should work independently
	if iter1.Value() != 10 {
		t.Errorf("iter1: expected 10, got %d", iter1.Value())
	}
	if iter2.Value() != 50 {
		t.Errorf("iter2: expected 50, got %d", iter2.Value())
	}
	if iter3.Value() != 10 {
		t.Errorf("iter3: expected 10, got %d", iter3.Value())
	}

	// Advance iterators independently
	iter1.Next()
	if iter1.Value() != 20 {
		t.Errorf("iter1 after Next: expected 20, got %d", iter1.Value())
	}
	if iter3.Value() != 10 {
		t.Errorf("iter3 should still be at 10, got %d", iter3.Value())
	}
}

func TestCommitsRangeOverFunc(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	elements := []int{10, 20, 30, 40, 50}
	for _, e := range elements {
		tree.Insert(e)
	}

	// Test range-over-func syntax
	collected := []int{}
	for v := range tree.Commits(Up) {
		collected = append(collected, v)
	}

	if len(collected) != len(elements) {
		t.Errorf("Expected %d elements, got %d", len(elements), len(collected))
	}
	for i, exp := range elements {
		if collected[i] != exp {
			t.Errorf("Position %d: expected %d, got %d", i, exp, collected[i])
		}
	}
}

func TestCommitsAscending(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(30)
	tree.Insert(10)
	tree.Insert(20)

	collected := []int{}
	for v := range tree.Commits(Up) {
		collected = append(collected, v)
	}

	expected := []int{10, 20, 30}
	if len(collected) != len(expected) {
		t.Errorf("Expected %d elements, got %d", len(expected), len(collected))
	}
	for i, exp := range expected {
		if collected[i] != exp {
			t.Errorf("Position %d: expected %d, got %d", i, exp, collected[i])
		}
	}
}

func TestCommitsDescending(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	tree.Insert(30)
	tree.Insert(10)
	tree.Insert(20)

	collected := []int{}
	for v := range tree.Commits(Down) {
		collected = append(collected, v)
	}

	expected := []int{30, 20, 10}
	if len(collected) != len(expected) {
		t.Errorf("Expected %d elements, got %d", len(expected), len(collected))
	}
	for i, exp := range expected {
		if collected[i] != exp {
			t.Errorf("Position %d: expected %d, got %d", i, exp, collected[i])
		}
	}
}

func TestCommitsEarlyReturn(t *testing.T) {
	less := func(a, b int) bool { return a < b }
	tree := NewTree(less)
	elements := []int{10, 20, 30, 40, 50}
	for _, e := range elements {
		tree.Insert(e)
	}

	// Early return after 3 elements
	collected := []int{}
	count := 0
	for v := range tree.Commits(Up) {
		collected = append(collected, v)
		count++
		if count >= 3 {
			break
		}
	}

	if len(collected) != 3 {
		t.Errorf("Expected 3 elements, got %d", len(collected))
	}
	expected := []int{10, 20, 30}
	for i, exp := range expected {
		if collected[i] != exp {
			t.Errorf("Position %d: expected %d, got %d", i, exp, collected[i])
		}
	}
}
