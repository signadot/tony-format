package server

import (
	"fmt"

	"github.com/signadot/tony-format/tony/ir"
	tony "github.com/signadot/tony-format/tony"
)

// reconstructState reconstructs state for a path at a target commit count.
// It uses snapshots when available to optimize reconstruction.
// Returns the reconstructed state and the actual commit count.
func (s *Server) reconstructState(virtualPath string, targetCommitCount *int64) (*ir.Node, int64, error) {
	// List all diffs for this path
	diffList, err := s.storage.ListDiffs(virtualPath)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list diffs: %w", err)
	}

	// Determine target commit count
	var targetCommit int64
	if targetCommitCount != nil {
		targetCommit = *targetCommitCount
	} else if len(diffList) > 0 {
		// Use latest commit count
		targetCommit = diffList[len(diffList)-1].CommitCount
	} else {
		// No diffs, return null state
		return ir.Null(), 0, nil
	}

	// Try to find a snapshot to start from
	snapshotCommitCount, err := s.storage.FindNearestSnapshot(virtualPath, targetCommit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to find snapshot: %w", err)
	}

	var state *ir.Node
	var startCommitCount int64

	if snapshotCommitCount > 0 {
		// Load snapshot
		snapshot, err := s.storage.ReadSnapshot(virtualPath, snapshotCommitCount)
		if err != nil {
			// If snapshot read fails, fall back to starting from null
			state = ir.Null()
			startCommitCount = 0
		} else {
			state = snapshot.State
			startCommitCount = snapshotCommitCount
		}
	} else {
		// No snapshot, start from null
		state = ir.Null()
		startCommitCount = 0
	}

	// Filter diffs to apply (only those after the snapshot)
	var diffsToApply []struct{ CommitCount, TxSeq int64 }
	for _, diff := range diffList {
		if diff.CommitCount > startCommitCount && diff.CommitCount <= targetCommit {
			diffsToApply = append(diffsToApply, diff)
		}
	}

	// Apply diffs sequentially
	for _, diffInfo := range diffsToApply {
		// Read the diff file
		diffFile, err := s.storage.ReadDiff(virtualPath, diffInfo.CommitCount, diffInfo.TxSeq, false)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to read diff: %w", err)
		}

		// Apply the diff to reconstruct state
		state, err = tony.Patch(state, diffFile.Diff)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to apply diff: %w", err)
		}
	}

	return state, targetCommit, nil
}

// createSnapshot creates a snapshot for a path at a specific commit count.
// It reconstructs the state and writes it as a snapshot.
func (s *Server) createSnapshot(virtualPath string, commitCount int64) error {
	// Reconstruct state at the target commit count
	state, actualCommitCount, err := s.reconstructState(virtualPath, &commitCount)
	if err != nil {
		return fmt.Errorf("failed to reconstruct state: %w", err)
	}

	// Verify we got the right commit count
	if actualCommitCount != commitCount {
		return fmt.Errorf("commit count mismatch: expected %d, got %d", commitCount, actualCommitCount)
	}

	// Write snapshot
	if err := s.storage.WriteSnapshot(virtualPath, commitCount, state); err != nil {
		return fmt.Errorf("failed to write snapshot: %w", err)
	}

	return nil
}
