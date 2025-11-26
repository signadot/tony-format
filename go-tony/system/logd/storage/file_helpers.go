package storage

import (
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

// ListDiffs lists all diff segments for a path, ordered by commit count.
// The index only contains committed diffs, never pending files.
// Returns both point segments (individual diffs) and range segments (compacted diffs).
func (s *Storage) ListDiffs(virtualPath string) ([]*index.LogSegment, error) {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()

	// Use index for fast lookup instead of filesystem
	segments := s.index.LookupRange(virtualPath, nil, nil)

	// Convert to pointers
	result := make([]*index.LogSegment, len(segments))
	for i := range segments {
		result[i] = &segments[i]
	}

	return result, nil
}
