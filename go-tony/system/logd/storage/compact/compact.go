package compact

import (
	"fmt"
	"sync"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/paths"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/seq"
)

type Compactor struct {
	Config Config
	*seq.Seq
	Index         *index.Index
	IndexFSLocker sync.Locker

	// store dir compactors indexed by virtual path
	dcMu  sync.Mutex
	dcMap map[string]*DirCompactor
}

func NewCompactor(cfg *Config, seq *seq.Seq, idxL sync.Locker, idx *index.Index) *Compactor {
	return &Compactor{
		Config:        *cfg,
		Seq:           seq,
		Index:         idx,
		IndexFSLocker: idxL,
		dcMap:         map[string]*DirCompactor{},
	}
}

// OnNewSegment triggers compaction for a new index segment.
// OnNewSegment should never be called for a given relative
// path of a segment before any previous call completed.
// Practically speaking, this means it should be called during
// commits while the seq lock is locked.  OnNewSegment will
// have a strong tendency to return very quickly to help accomodate
// the caller.
func (c *Compactor) OnNewSegment(seg *index.LogSegment) error {
	dc := c.getOrInitDC(seg)
	dc.incoming <- *seg
	return nil
}

func (c *Compactor) getOrInitDC(seg *index.LogSegment) *DirCompactor {
	c.dcMu.Lock()
	defer c.dcMu.Unlock()
	dc := c.dcMap[seg.RelPath]
	if dc == nil {
		dir := paths.PathToFilesystem(c.Config.Root, seg.RelPath)
		dc = NewDirCompactor(&c.Config, 0, dir, seg.RelPath, c.Seq, c.IndexFSLocker, c.Index)
		c.dcMap[seg.RelPath] = dc
		fmt.Printf("created dc in dir %q for vp %q\n", dir, seg.RelPath)
	}
	return dc
}
