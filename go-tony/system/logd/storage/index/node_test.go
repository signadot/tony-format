package index

import "testing"

func TestNode(t *testing.T) {
	l := newLeaf[int](func(a, b int) bool { return a < b })
	if l.index(0) != -1 {
		t.Error("not -1")
	}
	added, over := l.leafAdd(1)
	if over {
		t.Error("badover")
	}
	if !added {
		t.Error("not add 1")
	}
	if l.index(1) != 0 {
		t.Error("index != 0")
	}
	if !l.leafRemove(1) {
		t.Error("no rm")
	}
	if l.index(1) != -1 {
		t.Error("not rmd")
	}

	for i := range 32 {
		added, over := l.leafAdd(i)
		if over {
			t.Errorf("over at %d", i)
		}
		if !added {
			t.Errorf("false added")
		}
	}
	added, over = l.leafAdd(32)
	if !over {
		t.Error("no over")
	}
	for i := range 32 {
		j := 31 - i
		index := l.index(j)
		if index == -1 {
			t.Errorf("bad -1 at %d", j)
		}
		if !l.leafRemove(j) {
			t.Errorf("bad remove %d", j)
		}
		index = l.index(j)
		if index != -1 {
			t.Errorf("bad index %d at %d", index, j)
		}
		if l.leafRemove(j) {
			t.Errorf("false remove at %d", j)
		}

	}
}

func TestTree(t *testing.T) {
	tr := NewTree[int](func(a, b int) bool { return a < b })

	n := 10000
	for i := 0; i < n; i++ {
		if !tr.Insert(i) {
			t.Errorf("failed to insert %d", i)
		}
		if tr.Insert(i) {
			t.Errorf("inserted duplicate %d", i)
			// tr.root.Dump(0)
			t.FailNow()
		}
	}

	for i := 0; i < n; i++ {
		idx := tr.Index(i)
		if idx != i {
			t.Errorf("expected index %d for %d, got %d", i, i, idx)
		}
	}

	if tr.Index(n) != -1 {
		t.Error("found non-existent item")
	}

	// Remove half
	for i := 0; i < n; i += 2 {
		if !tr.Remove(i) {
			t.Errorf("failed to remove %d", i)
		}
		if tr.Remove(i) {
			t.Errorf("removed non-existent %d", i)
		}
	}

	// Check remaining
	for i := 0; i < n; i++ {
		idx := tr.Index(i)
		if i%2 == 0 {
			if idx != -1 {
				t.Errorf("found removed item %d", i)
			}
		} else {
			expected := i / 2
			if idx != expected {
				t.Errorf("expected index %d for %d, got %d", expected, i, idx)
			}
		}
	}
}
