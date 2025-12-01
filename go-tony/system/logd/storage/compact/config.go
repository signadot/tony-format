package compact

import (
	"log/slog"
)

type Config struct {
	// Configuration
	Root    string
	Divisor int
	Remove  func(int, int) bool // Remove(commit, level) returns true if the segment should be removed
	Log     *slog.Logger
}

// RemoveStrategy is a function that determines whether to remove a segment file
// based on its commit number and compaction level.
type RemoveStrategy func(commit, level int) bool

// NeverRemove returns a strategy that never removes files.
func NeverRemove() RemoveStrategy {
	return func(commit, level int) bool {
		return false
	}
}

// AlwaysRemove returns a strategy that always removes files after compaction.
func AlwaysRemove() RemoveStrategy {
	return func(commit, level int) bool {
		return true
	}
}

// LevelThreshold returns a strategy that removes files only at or below the specified level.
// For example, LevelThreshold(1) removes Level 0 and Level 1 files, but keeps Level 2+.
func LevelThreshold(maxLevel int) RemoveStrategy {
	return func(commit, level int) bool {
		return level <= maxLevel
	}
}

// HeadWindow returns a strategy that keeps the N most recent commits and removes older ones.
// curCommit should return the current commit number. The strategy removes input segments
// when compaction happens, based on whether we're past the keep window.
//
// Note: The commit parameter passed to the strategy is the compacted segment's commit,
// but the decision is based on the current commit state. This allows removal to happen
// when compaction occurs, regardless of what commit number the compacted segment receives.
//
// Example: HeadWindow(func() int { return 100 }, 10) removes files when current >= 100,
// keeping only commits >= 90.
func HeadWindow(curCommit func() int, keep int) RemoveStrategy {
	return func(commit, level int) bool {
		// Ignore the commit parameter - decision is based on current state
		current := curCommit()
		// Start removing files once we have more than 'keep' commits
		// This ensures we always keep at least 'keep' commits
		return current > keep
	}
}

// HeadWindowLevel combines HeadWindow with level filtering. It only removes files
// at or below maxLevel, and only when we're past the keep window.
func HeadWindowLevel(curCommit func() int, keep int, maxLevel int) RemoveStrategy {
	return func(commit, level int) bool {
		if level > maxLevel {
			return false
		}
		// Ignore the commit parameter - decision is based on current state
		current := curCommit()
		// Start removing files once we have more than 'keep' commits
		return current > keep
	}
}
