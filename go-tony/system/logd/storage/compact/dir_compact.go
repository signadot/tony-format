package compact

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/dfile"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/paths"
)

type DirCompactor struct {
	// global config
	Config Config

	// static per-dir
	Dir         string
	VirtualPath string
	// per level per dir
	Level int

	// runtime state
	CurSegment *index.LogSegment

	Start, Ref *ir.Node
	Inputs     int

	// dir compactor at next level
	Next *DirCompactor

	// queue management
	incoming chan index.LogSegment

	// lifecycle management
	done chan struct{}
}

func newDirCompactor(cfg *Config, lvl int, dir, vp string, env *storageEnv) *DirCompactor {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	dc := &DirCompactor{
		Config:      *cfg,
		Dir:         dir,
		VirtualPath: vp,
		Level:       lvl,
		incoming:    make(chan index.LogSegment, bufferSize(cfg.Divisor, lvl)),
		Ref:         ir.Null(),
		Start:       ir.Null(),
		done:        make(chan struct{}),
	}
	return dc
}

func NewDirCompactor(cfg *Config, lvl int, dir, vp string, env *storageEnv) *DirCompactor {
	dc := newDirCompactor(cfg, lvl, dir, vp, env)
	go dc.run(env)
	return dc
}

func (dc *DirCompactor) run(env *storageEnv) error {
	if err := dc.recover(nil, env); err != nil {
		return err // Initial recovery failed (e.g., shutdown)
	}
	for {
		var seg *index.LogSegment
		select {
		case s, ok := <-dc.incoming:
			if !ok {
				return nil
			}
			seg = &s
		case <-dc.done:
			return nil
		}
		if err := dc.processSegment(seg, env); err != nil {
			if err := dc.recover(err, env); err != nil {
				return err // Recovery failed (e.g., shutdown)
			}
		}
	}
}

func (dc *DirCompactor) processSegment(seg *index.LogSegment, env *storageEnv) error {
	// since recovery from an error can place us ahead of incoming,
	// be sure to skip segments we've already processed
	if dc.CurSegment != nil {
		if seg.EndCommit <= dc.CurSegment.EndCommit {
			dc.Config.Log.Debug("skipping", "segment", seg)
			return nil
		}
	}
	df, err := dc.readSegment(seg, dc.Level)
	if err != nil {
		return err
	}
	tmp, err := tony.Patch(dc.Ref, df.Diff)
	if err != nil {
		return err
	}
	diff := tony.Diff(dc.Start, tmp)
	if diff == nil {
		return nil
	}
	rotate := dc.addSegment(seg)
	if rotate {
		if err := dc.rotateCompactionWindow(env, diff, tmp); err != nil {
			return err
		}
	}
	dc.Ref = tmp
	return nil
}

// rotateCompactionWindow handles the rotation of a compaction window:
// persists the current segment, resets state, and propagates to next level.
func (dc *DirCompactor) rotateCompactionWindow(env *storageEnv, diff *ir.Node, newState *ir.Node) error {
	if err := dc.persistCurrent(env, diff); err != nil {
		return err
	}
	dc.Inputs = 0
	if dc.Next == nil {
		dc.Next = NewDirCompactor(&dc.Config, dc.Level+1, dc.Dir, dc.VirtualPath, env)
	} else {
		dc.Next.incoming <- *dc.CurSegment
	}
	dc.CurSegment = &index.LogSegment{RelPath: dc.VirtualPath}
	dc.Start = newState
	return nil
}

// recover performs synchronous recovery by reading state from disk and processing
// any unprocessed segments. It retries on failure with exponential backoff, checking for
// shutdown requests. Returns an error if recovery is cancelled (e.g., shutdown).
func (dc *DirCompactor) recover(e error, env *storageEnv) error {
	if e != nil {
		dc.Config.Log.Warn("starting recovery for", "path", dc.VirtualPath, "error", e)
	}
	backoff := time.Second
	maxBackoff := 5 * time.Minute
	for {
		// Reset state
		dc.Inputs = 0
		dc.CurSegment = nil
		dc.Start = ir.Null()
		dc.Ref = ir.Null()

		// Read state from disk
		inputSegs, err := dc.readState(env)
		if err != nil {
			dc.Config.Log.Warn("error reading state in recover: trying again", "error", err, "backoff", backoff)
			if err := dc.waitForRetry(backoff); err != nil {
				return err // Shutdown requested
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		// Process segments recovered from disk
		for i := range inputSegs {
			if err := dc.processSegment(&inputSegs[i], env); err != nil {
				dc.Config.Log.Warn("error processing segments in recover: trying again", "error", err, "backoff", backoff)
				if err := dc.waitForRetry(backoff); err != nil {
					return err // Shutdown requested
				}
				backoff = min(backoff*2, maxBackoff)
				goto retry
			}
		}

		// Recovery successful
		return nil

	retry:
		// Continue loop to retry
	}
}

// waitForRetry waits for the retry interval or shutdown signal.
// Returns nil to continue retry, or an error if shutdown is requested.
func (dc *DirCompactor) waitForRetry(delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil // Retry now
	case <-dc.done:
		return fmt.Errorf("recovery cancelled: shutdown requested")
	}
}

func (dc *DirCompactor) addSegment(seg *index.LogSegment) bool {
	if dc.CurSegment == nil {
		dc.CurSegment = &index.LogSegment{
			StartCommit: seg.StartCommit,
			StartTx:     seg.StartTx,
			RelPath:     dc.VirtualPath,
		}
	}
	dc.CurSegment.EndCommit = seg.EndCommit
	dc.CurSegment.EndTx = seg.EndTx
	dc.Inputs++
	return dc.Inputs >= dc.Config.Divisor
}

func (dc *DirCompactor) persistCurrent(env *storageEnv, diff *ir.Node) error {
	// 1. Allocate txSeq
	txSeq, err := env.seq.NextTxSeq()
	if err != nil {
		return err
	}

	// 2. Write pending
	seg := &index.LogSegment{
		StartCommit: 0,
		EndCommit:   0,
		StartTx:     dc.CurSegment.StartTx,
		EndTx:       dc.CurSegment.EndTx,
		RelPath:     dc.VirtualPath,
	}

	filename := paths.FormatLogSegment(seg, dc.Level+1, true)
	df := &dfile.DiffFile{
		Seq:     txSeq,
		Diff:    diff,
		Inputs:  dc.Inputs,
		Pending: true,
	}
	err = dfile.WriteDiffFile(filepath.Join(dc.Dir, filename), df)
	if err != nil {
		return err
	}

	// 3. Commit
	env.seq.Lock()
	defer env.seq.Unlock()
	commit, err := env.seq.NextCommitLocked()
	if err != nil {
		return err
	}

	seg.StartCommit = dc.CurSegment.StartCommit
	seg.EndCommit = commit

	if err := dfile.CommitPending(dc.Dir, seg, dc.Level+1, commit); err != nil {
		return err
	}

	// 4. Index
	env.idxL.Lock()
	defer env.idxL.Unlock()
	env.idx.Add(seg)

	// 5. Remove old input segment files if configured to do so
	// Do this BEFORE updating CurSegment, so we have the correct range
	if dc.Config.Remove != nil && dc.Config.Remove(int(commit), dc.Level+1) {
		if err := dc.removeInputSegments(commit); err != nil {
			// Log but don't fail - removal is best effort
			dc.Config.Log.Warn("failed to remove input segments", "error", err, "commit", commit, "level", dc.Level)
		}
	}

	// update current to have start commit if 0
	if dc.CurSegment.StartCommit == 0 {
		dc.CurSegment.StartCommit = commit
	}
	dc.CurSegment.EndCommit = commit
	dc.CurSegment.EndTx = txSeq
	return nil
}

// removeInputSegments removes input segment files that were compacted into the segment
// at the given commit. It scans the directory for segments at dc.Level that fall within
// the range covered by CurSegment and deletes them.
func (dc *DirCompactor) removeInputSegments(commit int64) error {
	if dc.CurSegment == nil {
		return nil // Nothing to remove
	}

	dirEnts, err := os.ReadDir(dc.Dir)
	if err != nil {
		return err
	}

	// Use the transaction range (StartTx/EndTx) to identify which segments to remove,
	// since commit numbers may differ. The CurSegment tracks the range of input segments
	// that were compacted.
	startTx := dc.CurSegment.StartTx
	endTx := dc.CurSegment.EndTx

	for _, de := range dirEnts {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		seg, lvl, err := paths.ParseLogSegment(name)
		if err != nil {
			continue // Skip invalid filenames
		}
		// Only remove segments at our level that fall within the compacted range
		if lvl == dc.Level && seg.RelPath == dc.VirtualPath {
			// Check if segment overlaps the compacted range [startTx, endTx]
			// A segment overlaps if its EndTx is within the range (since segments are ordered)
			if seg.EndTx >= startTx && seg.EndTx <= endTx {
				filePath := filepath.Join(dc.Dir, name)
				if err := os.Remove(filePath); err != nil {
					dc.Config.Log.Warn("failed to remove input segment", "file", name, "error", err)
					// Continue removing other segments even if one fails
				} else {
					dc.Config.Log.Debug("removed input segment", "file", name, "commit", commit, "level", dc.Level)
				}
			}
		}
	}
	return nil
}

func (dc *DirCompactor) readSegment(seg *index.LogSegment, lvl int) (*dfile.DiffFile, error) {
	name := paths.FormatLogSegment(seg, lvl, false)
	p := filepath.Join(dc.Dir, name)
	return dfile.ReadDiffFile(p)
}

func bufferSize(divisor, level int) int {
	const N = 3
	if level >= N {
		return 1
	}
	// pow(divisor, N - level)
	size := 1
	for i := 0; i < N-level; i++ {
		size *= divisor
	}
	return size
}
