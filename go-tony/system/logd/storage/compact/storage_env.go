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
}
