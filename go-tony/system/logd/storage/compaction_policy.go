package storage

import (
	"errors"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

// CompactionConfig configures the logarithmic retention policy for compaction.
type CompactionConfig struct {
	// Cutoff is the duration within which all patches are kept for accurate historical reads.
	// Beyond this cutoff, history degrades to snapshot granularity.
	Cutoff time.Duration

	// BaseInterval is the snapshot interval for tier 0 (most recent tier after cutoff).
	BaseInterval time.Duration

	// SlotsPerTier is the number of snapshots to keep in each tier.
	SlotsPerTier int

	// Multiplier is the factor by which each tier's interval increases.
	// Tier N has interval = BaseInterval * Multiplier^N
	Multiplier int

	// GracePeriod is how long to wait for active readers to finish after swap.
	// After this timeout, old file is deleted and lingering readers will error.
	GracePeriod time.Duration
}

// DefaultCompactionConfig returns a default compaction configuration.
func DefaultCompactionConfig() *CompactionConfig {
	return &CompactionConfig{
		Cutoff:       1 * time.Hour,
		BaseInterval: 1 * time.Hour,
		SlotsPerTier: 8,
		Multiplier:   2,
		GracePeriod:  5 * time.Second,
	}
}

// Validate checks that the compaction configuration is valid.
// Returns an error if any field has an invalid value that could cause
// incorrect behavior (e.g., infinite loops in tier calculation).
func (c *CompactionConfig) Validate() error {
	if c.BaseInterval <= 0 {
		return errors.New("compaction config: BaseInterval must be positive")
	}
	if c.Multiplier < 2 {
		return errors.New("compaction config: Multiplier must be at least 2")
	}
	if c.SlotsPerTier <= 0 {
		return errors.New("compaction config: SlotsPerTier must be positive")
	}
	if c.GracePeriod < 0 {
		return errors.New("compaction config: GracePeriod cannot be negative")
	}
	return nil
}

// compactionPolicy implements the logarithmic retention algorithm.
type compactionPolicy struct {
	config    *CompactionConfig
	now       time.Time
	pinCommit int64 // commit of pinned snapshot (active schema), or -1 if none
}

// newCompactionPolicy creates a policy for selecting which snapshots survive compaction.
func newCompactionPolicy(config *CompactionConfig, now time.Time, pinCommit int64) *compactionPolicy {
	return &compactionPolicy{
		config:    config,
		now:       now,
		pinCommit: pinCommit,
	}
}

// snapshotGroup represents a baseline snapshot and its associated scope snapshots.
// They are kept or deleted together as a unit.
type snapshotGroup struct {
	commit   int64
	time     time.Time
	segments []index.LogSegment // baseline + scope snapshots at this commit
}

// selectSurvivors determines which snapshot groups survive compaction.
// Returns the segments to keep.
func (p *compactionPolicy) selectSurvivors(groups []snapshotGroup) []index.LogSegment {
	var survivors []index.LogSegment

	// Assign each group to a tier based on age
	tiers := p.assignToTiers(groups)

	// For each tier, select up to SlotsPerTier snapshots
	for tierNum, tierGroups := range tiers {
		selected := p.selectFromTier(tierNum, tierGroups)
		for _, group := range selected {
			survivors = append(survivors, group.segments...)
		}
	}

	return survivors
}

// assignToTiers buckets snapshot groups into tiers based on age.
// Tier -1 is within cutoff (all kept).
// Tier 0+ follows logarithmic spacing.
func (p *compactionPolicy) assignToTiers(groups []snapshotGroup) map[int][]snapshotGroup {
	tiers := make(map[int][]snapshotGroup)
	cutoffTime := p.now.Add(-p.config.Cutoff)

	for _, group := range groups {
		// Pinned snapshot always survives (tier -2 = always keep)
		if group.commit == p.pinCommit {
			tiers[-2] = append(tiers[-2], group)
			continue
		}

		// Within cutoff - all kept (tier -1)
		if group.time.After(cutoffTime) {
			tiers[-1] = append(tiers[-1], group)
			continue
		}

		// Beyond cutoff - assign to logarithmic tier
		age := p.now.Sub(group.time)
		tierNum := p.tierForAge(age)
		tiers[tierNum] = append(tiers[tierNum], group)
	}

	return tiers
}

// tierForAge returns the tier number for a given age beyond cutoff.
// Tier 0: cutoff to cutoff + baseInterval
// Tier 1: cutoff + baseInterval to cutoff + baseInterval * multiplier
// Tier N: interval = baseInterval * multiplier^N
func (p *compactionPolicy) tierForAge(age time.Duration) int {
	// Age beyond cutoff
	beyondCutoff := age - p.config.Cutoff
	if beyondCutoff <= 0 {
		return -1 // within cutoff
	}

	// Find tier: which power of multiplier contains this age?
	// Tier N covers: baseInterval * (multiplier^N - 1) / (multiplier - 1) to baseInterval * (multiplier^(N+1) - 1) / (multiplier - 1)
	// Simplified: tier N starts at baseInterval * multiplier^N
	interval := p.config.BaseInterval
	tier := 0
	accumulated := time.Duration(0)

	for accumulated+interval < beyondCutoff {
		accumulated += interval
		interval *= time.Duration(p.config.Multiplier)
		tier++
	}

	return tier
}

// selectFromTier selects up to SlotsPerTier snapshots from a tier.
// For tier -2 (pinned) and tier -1 (within cutoff), all are kept.
// For tier 0+, selects the most recent snapshots per slot interval.
func (p *compactionPolicy) selectFromTier(tierNum int, groups []snapshotGroup) []snapshotGroup {
	// Pinned and within-cutoff tiers: keep all
	if tierNum < 0 {
		return groups
	}

	// For regular tiers, keep up to SlotsPerTier
	if len(groups) <= p.config.SlotsPerTier {
		return groups
	}

	// Select evenly spaced snapshots, preferring more recent ones
	// Simple approach: sort by time descending, take first SlotsPerTier
	// More sophisticated: divide tier's time range into slots, pick one per slot

	// For now, simple approach: keep most recent SlotsPerTier
	// Groups should already be sorted by commit/time
	selected := make([]snapshotGroup, 0, p.config.SlotsPerTier)
	for i := len(groups) - 1; i >= 0 && len(selected) < p.config.SlotsPerTier; i-- {
		selected = append(selected, groups[i])
	}

	return selected
}

// sortSnapshotGroups sorts groups by commit ascending.
func sortSnapshotGroups(groups []snapshotGroup) {
	for i := 1; i < len(groups); i++ {
		for j := i; j > 0 && groups[j].commit < groups[j-1].commit; j-- {
			groups[j], groups[j-1] = groups[j-1], groups[j]
		}
	}
}
