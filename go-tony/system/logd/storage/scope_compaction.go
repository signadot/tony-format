package storage

import (
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// Scope compaction (rebuild_plan.md phase 7, second half; 5hmq80f3h12krh1mbsn0).
//
// A scope's entries are its layer: a scoped read folds baseline to the commit asked for
// and then the scope's entries, all of them, in order, on top. Nothing materialized stands
// in for them -- a scope's view depends on a baseline that keeps moving -- so compaction
// kept every one until DeleteScope, and a scope that rewrote one field a thousand times
// carried a thousand entries into every read of it.
//
// What a stored scope write IS makes most of them removable. It is absolute
// (api.ValidateForStorage; lower.go): a statement of what a value is at a path, not of how
// to change it. So an entry is DOMINATED when a later entry of the same scope states every
// path it stated, or an ancestor of it, in a way that does not depend on what was there --
// and a dominated entry contributes nothing to the fold from that later entry on. Beyond
// the cutoff, where history is already approximate, it goes.
//
// THE COVERS are index.Cover, and the deciding is done at the write: the index keeps,
// per scope, the statements no later statement dominates (index/footprint.go), so the
// pass here is a lookup by entry and reads nothing. An entry beyond the cutoff with no
// statement left live goes.

// dominatedScopeEntries answers, by log position, which of the candidate entries -- scope
// entries of the inactive log that are beyond the cutoff, each with its scope -- a later
// entry of the same scope dominates: the ones with no statement left live in the
// footprint. It reads no entry; the footprint decided at each write.
func (s *Storage) dominatedScopeEntries(inactive dlog.LogFileID, candidates map[int64]*string) (map[int64]bool, error) {
	dominated := map[int64]bool{}
	for pos, sc := range candidates {
		if !s.index.Footprint().EntryLive(*sc, string(inactive), pos) {
			dominated[pos] = true
		}
	}
	return dominated, nil
}
