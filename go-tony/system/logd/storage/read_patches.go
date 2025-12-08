package storage

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// PatchAtSegment represents a patch extracted from a log segment.
type PatchAtSegment struct {
	Segment *index.LogSegment
	Patch   *ir.Node // Extracted patch at segment.KindedPath
	Parent  *ir.Node // Reconstructed parent patch (if segment.KindedPath is deep)
}

// ReadPatchesAt reads all patches affecting the given kpath at the given commit.
// Returns patches extracted from log entries, including sub-trees and reconstructed parents.
// This is a testing/development helper - it doesn't apply or merge patches.
func (s *Storage) ReadPatchesAt(kpath string, commit int64) ([]PatchAtSegment, error) {
	segments := s.index.LookupWithin(kpath, commit)
	if len(segments) == 0 {
		return nil, nil
	}

	var result []PatchAtSegment
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
			kp, err := ir.ParseKPath(seg.KindedPath)
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

		result = append(result, PatchAtSegment{
			Segment: &seg,
			Patch:   patch,
			Parent:  parent,
		})
	}

	return result, nil
}
