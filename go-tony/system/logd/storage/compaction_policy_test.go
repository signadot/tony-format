package storage

import (
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

func TestTierForAge(t *testing.T) {
	config := &CompactionConfig{
		Cutoff:       1 * time.Hour,
		BaseInterval: 1 * time.Hour,
		SlotsPerTier: 4,
		Multiplier:   2,
	}
	now := time.Now()
	policy := newCompactionPolicy(config, now, -1)

	tests := []struct {
		name     string
		age      time.Duration
		wantTier int
	}{
		{"within cutoff", 30 * time.Minute, -1},
		{"at cutoff", 1 * time.Hour, -1},
		{"tier 0 start", 1*time.Hour + 1*time.Minute, 0},
		{"tier 0 end", 1*time.Hour + 59*time.Minute, 0},
		{"tier 1 start", 2*time.Hour + 1*time.Minute, 1},
		{"tier 1 (2h interval)", 3 * time.Hour, 1},
		{"tier 2 start", 4*time.Hour + 1*time.Minute, 2},
		{"tier 2 (4h interval)", 6 * time.Hour, 2},
		{"tier 3 start", 8*time.Hour + 1*time.Minute, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := policy.tierForAge(tt.age)
			if got != tt.wantTier {
				t.Errorf("tierForAge(%v) = %d, want %d", tt.age, got, tt.wantTier)
			}
		})
	}
}

func TestAssignToTiers(t *testing.T) {
	config := &CompactionConfig{
		Cutoff:       1 * time.Hour,
		BaseInterval: 1 * time.Hour,
		SlotsPerTier: 4,
		Multiplier:   2,
	}
	now := time.Now()
	pinCommit := int64(5)
	policy := newCompactionPolicy(config, now, pinCommit)

	groups := []snapshotGroup{
		{commit: 1, time: now.Add(-30 * time.Minute)},  // within cutoff
		{commit: 2, time: now.Add(-90 * time.Minute)},  // tier 0
		{commit: 3, time: now.Add(-3 * time.Hour)},     // tier 1
		{commit: 4, time: now.Add(-6 * time.Hour)},     // tier 2
		{commit: 5, time: now.Add(-10 * time.Hour)},    // pinned (regardless of age)
		{commit: 6, time: now.Add(-20 * time.Minute)},  // within cutoff
	}

	tiers := policy.assignToTiers(groups)

	// Check pinned tier (-2)
	if len(tiers[-2]) != 1 || tiers[-2][0].commit != 5 {
		t.Errorf("expected commit 5 in pinned tier, got %v", tiers[-2])
	}

	// Check within-cutoff tier (-1)
	if len(tiers[-1]) != 2 {
		t.Errorf("expected 2 groups in tier -1 (within cutoff), got %d", len(tiers[-1]))
	}

	// Check tier 0
	if len(tiers[0]) != 1 || tiers[0][0].commit != 2 {
		t.Errorf("expected commit 2 in tier 0, got %v", tiers[0])
	}

	// Check tier 1
	if len(tiers[1]) != 1 || tiers[1][0].commit != 3 {
		t.Errorf("expected commit 3 in tier 1, got %v", tiers[1])
	}

	// Check tier 2
	if len(tiers[2]) != 1 || tiers[2][0].commit != 4 {
		t.Errorf("expected commit 4 in tier 2, got %v", tiers[2])
	}
}

func TestSelectFromTier(t *testing.T) {
	config := &CompactionConfig{
		SlotsPerTier: 3,
	}
	policy := newCompactionPolicy(config, time.Now(), -1)

	t.Run("negative tier keeps all", func(t *testing.T) {
		groups := make([]snapshotGroup, 10)
		for i := range groups {
			groups[i] = snapshotGroup{commit: int64(i)}
		}
		selected := policy.selectFromTier(-1, groups)
		if len(selected) != 10 {
			t.Errorf("expected 10 groups for tier -1, got %d", len(selected))
		}
	})

	t.Run("fewer than slots keeps all", func(t *testing.T) {
		groups := []snapshotGroup{
			{commit: 1},
			{commit: 2},
		}
		selected := policy.selectFromTier(0, groups)
		if len(selected) != 2 {
			t.Errorf("expected 2 groups, got %d", len(selected))
		}
	})

	t.Run("more than slots keeps most recent", func(t *testing.T) {
		groups := []snapshotGroup{
			{commit: 1},
			{commit: 2},
			{commit: 3},
			{commit: 4},
			{commit: 5},
		}
		selected := policy.selectFromTier(0, groups)
		if len(selected) != 3 {
			t.Errorf("expected 3 groups, got %d", len(selected))
		}
		// Should keep most recent (highest commits)
		commits := make(map[int64]bool)
		for _, g := range selected {
			commits[g.commit] = true
		}
		if !commits[5] || !commits[4] || !commits[3] {
			t.Errorf("expected commits 3,4,5 to be kept, got %v", selected)
		}
	})
}

func TestSelectSurvivors(t *testing.T) {
	config := &CompactionConfig{
		Cutoff:       1 * time.Hour,
		BaseInterval: 1 * time.Hour,
		SlotsPerTier: 2,
		Multiplier:   2,
	}
	now := time.Now()
	policy := newCompactionPolicy(config, now, -1)

	// Create groups with segments
	seg := func(commit int64) index.LogSegment {
		return index.LogSegment{
			StartCommit: commit,
			EndCommit:   commit, // snapshot
			LogFile:     "A",
			LogPosition: commit * 100,
		}
	}

	groups := []snapshotGroup{
		{commit: 1, time: now.Add(-30 * time.Minute), segments: []index.LogSegment{seg(1)}},
		{commit: 2, time: now.Add(-45 * time.Minute), segments: []index.LogSegment{seg(2)}},
		// Tier 0: should keep 2 most recent
		{commit: 3, time: now.Add(-90 * time.Minute), segments: []index.LogSegment{seg(3)}},
		{commit: 4, time: now.Add(-100 * time.Minute), segments: []index.LogSegment{seg(4)}},
		{commit: 5, time: now.Add(-110 * time.Minute), segments: []index.LogSegment{seg(5)}},
	}

	survivors := policy.selectSurvivors(groups)

	// Within cutoff (1,2) should all survive
	// Tier 0 (3,4,5) should keep 2 most recent (4,5)
	expectedCommits := map[int64]bool{1: true, 2: true, 4: true, 5: true}
	survivorCommits := make(map[int64]bool)
	for _, s := range survivors {
		survivorCommits[s.StartCommit] = true
	}

	if len(survivors) != 4 {
		t.Errorf("expected 4 survivors, got %d", len(survivors))
	}

	for commit := range expectedCommits {
		if !survivorCommits[commit] {
			t.Errorf("expected commit %d to survive", commit)
		}
	}
}

func TestGroupSnapshots(t *testing.T) {
	segments := []index.LogSegment{
		// Snapshot at commit 1 (baseline)
		{StartCommit: 1, EndCommit: 1, ScopeID: nil},
		// Patch (should be ignored)
		{StartCommit: 0, EndCommit: 1, ScopeID: nil},
		// Snapshot at commit 2 (baseline + scope)
		{StartCommit: 2, EndCommit: 2, ScopeID: nil},
		{StartCommit: 2, EndCommit: 2, ScopeID: strPtr("scope1")},
		// Snapshot at commit 3
		{StartCommit: 3, EndCommit: 3, ScopeID: nil},
	}

	groups := groupSnapshots(segments)

	if len(groups) != 3 {
		t.Errorf("expected 3 groups, got %d", len(groups))
	}

	// Check groups are sorted by commit
	for i := 1; i < len(groups); i++ {
		if groups[i].commit < groups[i-1].commit {
			t.Errorf("groups not sorted: %d < %d", groups[i].commit, groups[i-1].commit)
		}
	}

	// Check commit 2 has 2 segments
	for _, g := range groups {
		if g.commit == 2 && len(g.segments) != 2 {
			t.Errorf("expected 2 segments for commit 2, got %d", len(g.segments))
		}
	}
}

func strPtr(s string) *string {
	return &s
}
