package server

import (
	"fmt"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
)

// reconstructState reconstructs state for a path at a target commit count.
// It uses snapshots when available to optimize reconstruction.
// Returns the reconstructed state and the actual commit count.
func (s *Server) reconstructState(virtualPath string, targetCommitCount *int64) (*ir.Node, int64, error) {
	// List all diffs for this path
	diffList, err := s.Config.Storage.ListDiffs(virtualPath)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list diffs: %w", err)
	}

	// Determine target commit count
	var targetCommit int64
	if targetCommitCount != nil {
		targetCommit = *targetCommitCount
	} else if len(diffList) > 0 {
		// Use latest commit count
		targetCommit = diffList[len(diffList)-1].StartCommit
	} else {
		// No diffs, return null state
		return ir.Null(), 0, nil
	}

	var state = ir.Null()
	var startCommitCount int64

	// Apply diffs sequentially up to target commit
	for _, seg := range diffList {
		if seg.StartCommit > startCommitCount && seg.StartCommit <= targetCommit {
			// Read the diff file
			diffFile, err := s.Config.Storage.ReadDiff(virtualPath, seg.StartCommit, seg.StartTx, false)
			if err != nil {
				return nil, 0, fmt.Errorf("failed to read diff: %w", err)
			}

			// Apply the diff to reconstruct state
			state, err = tony.Patch(state, diffFile.Diff)
			if err != nil {
				return nil, 0, fmt.Errorf("failed to apply diff: %w", err)
			}
		}
	}

	return state, targetCommit, nil
}
