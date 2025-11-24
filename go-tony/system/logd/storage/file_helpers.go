package storage

// ListDiffs lists all committed diff files for a path, ordered by commit count.
// Only returns .diff files, not .pending files.
// Returns a slice of (commitCount, txSeq) pairs.
func (s *Storage) ListDiffs(virtualPath string) ([]struct{ CommitCount, TxSeq int64 }, error) {
	s.indexMu.RLock()
	defer s.indexMu.RUnlock()

	// Use index for fast lookup instead of filesystem
	segments := s.index.LookupRange(virtualPath, nil, nil)

	// Convert []*LogSegment to old format
	var diffs []struct{ CommitCount, TxSeq int64 }
	for _, seg := range segments {
		if seg.IsPoint() && seg.StartCommit != 0 { // Skip pending
			diffs = append(diffs, struct{ CommitCount, TxSeq int64 }{seg.StartCommit, seg.StartTx})
		}
	}
	return diffs, nil
}
