package compact

import (
	"sync"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/seq"
)

// storageEnv bundles the storage-related dependencies that are passed around together
type storageEnv struct {
	seq  *seq.Seq
	idxL sync.Locker
	idx  *index.Index
	// readStateMu serializes readState() calls across all compaction levels
	// to prevent races where one level's readState sees inconsistent state
	// while another level's persistCurrent is allocating commits.
	// Lower levels have priority to prevent blocking progress.
	readStateMu     sync.Mutex
	readStateCond   *sync.Cond
	readStateLevel  int  // current level holding readState lock, or -1 if none
	readStateWaiters []int // levels waiting for readState lock (lower levels first)
}
