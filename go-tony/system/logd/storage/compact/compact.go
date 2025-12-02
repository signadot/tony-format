// Package compact implements hierarchical log segment compaction.
//
// Compaction provides fast access to fully reconstructed objects from their
// diff history and reduces storage by combining multiple small segments into
// larger ones. The algorithm works in levels: Level 0 segments are compacted
// into Level 1, Level 1 into Level 2, and so on.
//
// Algorithm:
//
//  1. Segments arrive at Level 0 via OnNewSegment()
//  2. When Divisor segments accumulate, they are compacted:
//     - Compute diff from Start state to current state
//     - Write compacted segment at Level+1
//     - Remove old input segment files if Config.Remove returns true
//     - Reset and start new compaction window
//  3. Compacted segments propagate to the next level for further compaction
//
// Key concepts:
//
//   - Divisor: Number of segments to accumulate before compacting (Config.Divisor)
//   - Compaction window: The range of segments being compacted together
//   - Start: The base state at the beginning of the current compaction window
//   - Ref: The current state after applying all segments in the window
//
// File removal:
//
// After successful compaction, input segment files may be removed based on
// Config.Remove(commit, level). This allows controlling when old segments are
// deleted (e.g., only after multiple levels of compaction, or never). Removal
// failures are logged but do not fail compaction.
//
// Package-level helper functions provide common removal strategies:
//
//   - NeverRemove(): Never remove files
//   - AlwaysRemove(): Always remove files after compaction
//   - LevelThreshold(maxLevel): Remove only files at or below maxLevel
//   - HeadWindow(curCommit, keep): Keep N most recent commits, remove older
//   - HeadWindowLevel(curCommit, keep, maxLevel): Combine HeadWindow with level filtering
//
// Example:
//
//	cfg := &Config{
//	  Remove: HeadWindow(func() int { return currentCommit() }, 100),
//	}
//
// Recovery:
//
// On startup or error, the compactor reads state from disk by scanning segment files
// and reconstructing the current state. This allows compaction to resume after
// restarts or transient failures.
//
// Thread safety:
//
// Compactor is safe for concurrent use. Each virtual path has its own DirCompactor
// running in a separate goroutine. OnNewSegment() returns quickly by enqueueing
// segments asynchronously.
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
	env           *storageEnv

	// store dir compactors indexed by virtual path
	dcMu  sync.Mutex
	dcMap map[string]*DirCompactor
}

func NewCompactor(cfg *Config, seq *seq.Seq, idxL sync.Locker, idx *index.Index) *Compactor {
	if cfg.Divisor <= 1 {
		panic("invalid divisor")
	}
	env := &storageEnv{
		seq:            seq,
		idxL:           idxL,
		idx:            idx,
		readStateLevel: -1,
	}
	env.readStateCond = sync.NewCond(&env.readStateMu)
	return &Compactor{
		Config:        *cfg,
		Seq:           seq,
		Index:         idx,
		IndexFSLocker: idxL,
		env:           env,
		dcMap:         map[string]*DirCompactor{},
	}
}

// OnNewSegment triggers compaction for a new index segment.
// OnNewSegment should never be called for a given relative
// path of a segment before any previous previous call completed.
// Practically speaking, this means it should be called during
// commits while the seq lock is locked.  OnNewSegment will
// have a strong tendency to return very quickly to help accomodate
// the caller.
func (c *Compactor) OnNewSegment(seg *index.LogSegment) error {
	dc := c.getOrInitDC(seg)
	if dc == nil {
		panic("OnNewSegment called after Shutdown")
	}
	// Try to send without blocking. If channel is full, log a warning but don't block commits.
	// The channel buffer is large (divisor^3 for level 0), so this should rarely fail.
	select {
	case dc.incoming <- *seg:
		return nil
	default:
		// Channel is full - this shouldn't happen with large buffers, but if it does,
		// log a warning and return an error so the caller knows compaction is backlogged.
		// Commits should not block waiting for compaction.
		return fmt.Errorf("compaction channel full for path %q - compaction is backlogged", seg.RelPath)
	}
}

func (c *Compactor) getOrInitDC(seg *index.LogSegment) *DirCompactor {
	c.dcMu.Lock()
	defer c.dcMu.Unlock()
	dc := c.dcMap[seg.RelPath]
	if dc == nil {
		// Check if we're shutting down (dcMap is nil after Shutdown)
		if c.dcMap == nil {
			return nil
		}
		dir := paths.PathToFilesystem(c.Config.Root, seg.RelPath)
		dc = NewDirCompactor(&c.Config, 0, dir, seg.RelPath, c.env)
		c.dcMap[seg.RelPath] = dc
		c.Config.Log.Debug("created dir compactor", "dir", dir, "path", seg.RelPath)
	}
	return dc
}

// GetDirCompactor returns the DirCompactor for the given virtual path, or nil if it doesn't exist.
func (c *Compactor) GetDirCompactor(virtualPath string) *DirCompactor {
	c.dcMu.Lock()
	defer c.dcMu.Unlock()
	return c.dcMap[virtualPath]
}

// Shutdown gracefully shuts down all compaction goroutines by closing their incoming channels
// and signalling done channels, then waits for acknowledgment that all goroutines have exited.
// This should be called before test teardown or process exit to prevent goroutine leaks.
func (c *Compactor) Shutdown() {
	c.dcMu.Lock()
	defer c.dcMu.Unlock()
	for _, dc := range c.dcMap {
		close(dc.incoming)
		// Signal shutdown to stop recovery (done channel is closed once, safe to check)
		select {
		case <-dc.done:
			// Already closed
		default:
			close(dc.done)
		}
		// Wait for acknowledgment that the goroutine has exited
		<-dc.doneAck
	}
	// Clear the map so Shutdown can be called multiple times safely
	c.dcMap = map[string]*DirCompactor{}
}
