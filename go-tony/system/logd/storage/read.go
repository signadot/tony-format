package storage

import "github.com/signadot/tony-format/go-tony/ir"

// Reading, and which read answers which question.
//
// There is one read of state, Read (cursor.go), and one read of change, Deltas
// (deltas.go). Both are at a path, both are cursors, and the root is a path. What used to
// vary between nine entry points -- whole document or subtree, replayed from a snapshot or
// stepped from a kept document -- is not the caller's to choose: the extent is the path,
// and where to seek from is the store's, answered from the index. A caller reached for one
// and paid for another three times in a month when the axes existed
// (ap8ddvp2h12krd43gdn0, kds4sx3bh12krdrkghn0, ntadpaech12krandgsn0); they do not.
//
// What remains inside the store is the stepped head: the document a WRITE is applied to,
// kept current so the commit path never replays (head.go, x2bn8w56h). It is not an entry
// point. steppedStateAt is its read, and it is the commit path's alone.

// steppedStateAt is the state a write at the next commit is applied to: the kept baseline
// head, or for a scope the same view its own reads see. It must never replay, and it
// answers in the store's own vocabulary.
func (s *Storage) steppedStateAt(commit int64, scopeID *string) (*ir.Node, error) {
	if scopeID != nil {
		return s.steppedScopedAt(commit, scopeID)
	}
	return s.steppedBaselineAt(commit)
}

// replayBaselineAt is the whole baseline document at commit, in the store's vocabulary:
// the root cursor, collected. It is what seeds and checks the head, and the intermediate
// the bound admits for that: the head IS a whole document.
func (s *Storage) replayBaselineAt(commit int64) (*ir.Node, error) {
	c, err := s.Read(commit, nil, "")
	if err != nil {
		return nil, err
	}
	return collectAll(c)
}

// replayScopedAt is the same, in a scope's view.
func (s *Storage) replayScopedAt(commit int64, scopeID *string) (*ir.Node, error) {
	c, err := s.Read(commit, scopeID, "")
	if err != nil {
		return nil, err
	}
	return collectAll(c)
}
