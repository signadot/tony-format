package storage

import (
	"fmt"
	"slices"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// patchAtSegment represents a patch extracted from a log segment.
type patchAtSegment struct {
	Segment *index.LogSegment
	Patch   *ir.Node // Extracted patch at segment.KindedPath
	Parent  *ir.Node // Reconstructed parent patch (if segment.KindedPath is deep)
}

// readPatchesAt reads all patches affecting the given kpath at the given commit.
// Returns patches extracted from log entries, including sub-trees and reconstructed parents.
// This is a testing/development helper - it doesn't apply or merge patches.
func (s *Storage) readPatchesAt(kp string, commit int64) ([]patchAtSegment, error) {
	segments := s.index.LookupWithin(kp, commit)
	if len(segments) == 0 {
		return nil, nil
	}

	var result []patchAtSegment
	for _, seg := range segments {

		// Read entry from dlog
		logFile := dlog.LogFileID(seg.LogFile)
		entry, err := s.dLog.ReadEntryAt(logFile, seg.LogPosition)
		if err != nil {
			return nil, fmt.Errorf("failed to read entry at %s:%d: %w", seg.LogFile, seg.LogPosition, err)
		}

		if entry.Patch == nil {
			continue
		}

		// Extract patch at segment's KindedPath
		var patch *ir.Node
		if seg.KindedPath == "" {
			// Root patch - clone it
			patch = entry.Patch.Clone()
		} else {
			// Extract sub-tree at this path
			var err error
			patch, err = entry.Patch.GetKPath(seg.KindedPath)
			if err != nil {
				// Path not found in patch - skip this segment
				continue
			}
		}

		// Reconstruct parent patch if this is a deep path
		var parent *ir.Node
		if seg.KindedPath != "" {
			kp, err := kpath.Parse(seg.KindedPath)
			if err == nil && kp != nil {
				parentKp := kp.Parent()
				if parentKp != nil {
					parentPath := parentKp.String()
					parent, err = entry.Patch.GetKPath(parentPath)
					if err != nil {
						// Parent not found - that's ok
						parent = nil
					}
				}
			}
		}

		result = append(result, patchAtSegment{
			Segment: &seg,
			Patch:   patch,
			Parent:  parent,
		})
	}

	return result, nil
}

// ReadPatchesInRange reads all patches affecting the given kpath in the commit range [from, to].
// Returns CommitNotifications sorted by commit number, deduplicated.
// This is used for watch replay - returning patches that affect the watched path.
func (s *Storage) ReadPatchesInRange(kp string, from, to int64) ([]*CommitNotification, error) {
	segments := s.index.LookupRange(kp, &from, &to)
	if len(segments) == 0 {
		return nil, nil
	}

	// Deduplicate by commit - same patch entry may be indexed at multiple paths
	seen := make(map[int64]bool)
	var result []*CommitNotification

	for _, seg := range segments {
		// Skip if we've already processed this commit
		if seen[seg.EndCommit] {
			continue
		}
		seen[seg.EndCommit] = true

		// Read entry from dlog
		logFile := dlog.LogFileID(seg.LogFile)
		entry, err := s.dLog.ReadEntryAt(logFile, seg.LogPosition)
		if err != nil {
			return nil, fmt.Errorf("failed to read entry at %s:%d: %w", seg.LogFile, seg.LogPosition, err)
		}

		if entry.Patch == nil {
			continue
		}

		// Build CommitNotification
		notification := &CommitNotification{
			Commit:    entry.Commit,
			Timestamp: entry.Timestamp,
			Patch:     entry.Patch,
			// KPaths not populated - would need index lookup per entry
		}

		result = append(result, notification)
	}

	// Sort by commit (segments are already sorted, but dedup may have changed order)
	slices.SortFunc(result, func(a, b *CommitNotification) int {
		if a.Commit < b.Commit {
			return -1
		}
		if a.Commit > b.Commit {
			return 1
		}
		return 0
	})

	return result, nil
}
