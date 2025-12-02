package compact

import (
	"testing"
)

func TestNeverRemove(t *testing.T) {
	strategy := NeverRemove()
	if strategy(1, 0) {
		t.Error("NeverRemove should never return true")
	}
	if strategy(100, 5) {
		t.Error("NeverRemove should never return true")
	}
}

func TestAlwaysRemove(t *testing.T) {
	strategy := AlwaysRemove()
	if !strategy(1, 0) {
		t.Error("AlwaysRemove should always return true")
	}
	if !strategy(100, 5) {
		t.Error("AlwaysRemove should always return true")
	}
}

func TestLevelThreshold(t *testing.T) {
	strategy := LevelThreshold(1)

	// Level 0 should be removed
	if !strategy(1, 0) {
		t.Error("Level 0 should be removed")
	}
	// Level 1 should be removed
	if !strategy(1, 1) {
		t.Error("Level 1 should be removed")
	}
	// Level 2 should NOT be removed
	if strategy(1, 2) {
		t.Error("Level 2 should not be removed")
	}
	// Level 3 should NOT be removed
	if strategy(1, 3) {
		t.Error("Level 3 should not be removed")
	}
}

func TestHeadWindow(t *testing.T) {
	currentCommit := 100
	strategy := HeadWindow(func() int { return currentCommit }, 10)

	// When currentCommit=100, threshold=90, so current > threshold (100 > 90)
	// This means we should remove files (we're past the keep window)
	if !strategy(100, 0) {
		t.Error("at commit 100 with keep=10, should remove files (100 > 90)")
	}

	// When currentCommit=5, threshold=-5, so current > threshold (5 > -5)
	// But we haven't accumulated enough commits yet - wait, that's wrong logic
	// Actually: if keep=10, we need currentCommit > 10 to start removing
	currentCommit = 5
	if strategy(5, 0) {
		t.Error("at commit 5 with keep=10, should NOT remove files yet (5 <= 10)")
	}

	// When currentCommit=11, threshold=1, so current > threshold (11 > 1)
	// This means we should remove files
	currentCommit = 11
	if !strategy(11, 0) {
		t.Error("at commit 11 with keep=10, should remove files (11 > 1)")
	}

	// Test with updated current commit
	currentCommit = 200
	if !strategy(200, 0) {
		t.Error("at commit 200 with keep=10, should remove files (200 > 190)")
	}
}

func TestHeadWindowLevel(t *testing.T) {
	currentCommit := 100
	strategy := HeadWindowLevel(func() int { return currentCommit }, 10, 1)

	// Level 0, at commit 100 - should be removed (current > threshold: 100 > 90)
	if !strategy(100, 0) {
		t.Error("Level 0, at commit 100 with keep=10, should remove files")
	}

	// Level 1, at commit 100 - should be removed
	if !strategy(100, 1) {
		t.Error("Level 1, at commit 100 with keep=10, should remove files")
	}

	// Level 2 - should NOT be removed (level too high)
	if strategy(100, 2) {
		t.Error("Level 2 should not be removed (level threshold)")
	}

	// Level 0, at commit 5 - should NOT be removed (not past keep window yet)
	currentCommit = 5
	if strategy(5, 0) {
		t.Error("Level 0, at commit 5 with keep=10, should NOT remove files yet")
	}

	// Level 2, at commit 100 - should NOT be removed (level threshold)
	currentCommit = 100
	if strategy(100, 2) {
		t.Error("Level 2 should not be removed (level threshold)")
	}
}
