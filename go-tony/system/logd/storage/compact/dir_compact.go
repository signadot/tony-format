package compact

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/dfile"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/paths"
)

var errRecoveryCancelled = errors.New("recovery cancelled: shutdown requested")

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
	done    chan struct{}
	doneAck chan struct{}
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
		doneAck:     make(chan struct{}),
	}
	return dc
}

func NewDirCompactor(cfg *Config, lvl int, dir, vp string, env *storageEnv) *DirCompactor {
	dc := newDirCompactor(cfg, lvl, dir, vp, env)
	go dc.run(env)
	return dc
}

func (dc *DirCompactor) run(env *storageEnv) error {
	defer close(dc.doneAck)
	if err := dc.recover(nil, env); err != nil {
		// If recovery was cancelled due to shutdown, that's expected - return gracefully
		if errors.Is(err, errRecoveryCancelled) {
			return nil
		}
		// Recovery failure means files are missing that logd should have created.
		// This is a bug - panic so the test fails immediately.
		panic(fmt.Sprintf("compaction recovery failed - files missing that logd should have created: path=%q error=%v", dc.VirtualPath, err))
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
			recoverErr := dc.recover(err, env)
			if recoverErr != nil {
				// If recovery was cancelled due to shutdown, that's expected - return gracefully
				if recoverErr.Error() == "recovery cancelled: shutdown requested" {
					return nil
				}
				// Recovery failure means files are missing that logd should have created.
				// This is a bug - panic so the test fails immediately.
				panic(fmt.Sprintf("compaction recovery failed - files missing that logd should have created: path=%q original_error=%v recovery_error=%v", dc.VirtualPath, err, recoverErr))
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
	// Debug: log what we're trying to read
	formatted := paths.FormatLogSegment(seg, dc.Level, false)
	_, expectedName := filepath.Split(formatted)
	if expectedName == "" {
		expectedName = formatted
	}
	expectedPath := filepath.Join(dc.Dir, expectedName)

	df, err := dc.readSegment(seg, dc.Level)
	if err != nil {
		// Compaction should process exactly the files that readState() found.
		// If a file doesn't exist, that's a bug - logd should have created it.
		// The only "unexpected" errors are filesystem-level issues (permissions, disk full, etc.)
		// not missing files that logd should have created.
		if os.IsNotExist(err) {
			// List directory to see what files actually exist
			dirEnts, listErr := os.ReadDir(dc.Dir)
			actualFiles := []string{}
			if listErr == nil {
				for _, de := range dirEnts {
					if !de.IsDir() && (strings.HasSuffix(de.Name(), ".diff") || strings.HasSuffix(de.Name(), ".pending")) {
						actualFiles = append(actualFiles, de.Name())
					}
				}
			}
			return fmt.Errorf("segment file does not exist (logd should have created this): segment=%v dc.Level=%d expectedPath=%q expectedName=%q actualFiles=%v error=%w", seg, dc.Level, expectedPath, expectedName, actualFiles, err)
		}
		// Filesystem errors (permissions, disk full, etc.) are unexpected but not logd bugs
		if strings.Contains(err.Error(), "invalid argument") {
			return fmt.Errorf("filesystem error reading segment: %w", err)
		}
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
	// Reset CurSegment to nil so it gets properly initialized from the next segment
	dc.CurSegment = nil
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
		// Check if shutdown was requested before starting recovery
		select {
		case <-dc.done:
			return errRecoveryCancelled
		default:
		}

		// Reset state
		dc.Inputs = 0
		dc.CurSegment = nil
		dc.Start = ir.Null()
		dc.Ref = ir.Null()

		// Read state from disk
		inputSegs, err := dc.readState(env)
		if err != nil {
			// Check for shutdown before retrying
			select {
			case <-dc.done:
				return errRecoveryCancelled
			default:
			}
			dc.Config.Log.Warn("error reading state in recover: trying again", "error", err, "backoff", backoff)
			if err := dc.waitForRetry(backoff); err != nil {
				return err // Shutdown requested
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}

		// Check for shutdown before processing segments
		select {
		case <-dc.done:
			return errRecoveryCancelled
		default:
		}

		// Process segments recovered from disk
		// readState() should have verified these files exist, so any missing file is a bug
		for i := range inputSegs {
			// Check for shutdown before each segment
			select {
			case <-dc.done:
				return errRecoveryCancelled
			default:
			}
			if err := dc.processSegment(&inputSegs[i], env); err != nil {
				// Compaction should process exactly the files readState() found.
				// If a file is missing, that's a bug in logd's file creation/commit.
				// Don't retry - fail immediately so the bug is visible.
				return fmt.Errorf("recovery failed: file that logd should have created is missing: %w", err)
			}
		}

		// Recovery successful
		return nil
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
		return errRecoveryCancelled
	}
}

func (dc *DirCompactor) addSegment(seg *index.LogSegment) bool {
	if dc.CurSegment == nil {
		dc.CurSegment = &index.LogSegment{
			StartCommit: seg.StartCommit,
			StartTx:     seg.StartTx,
			RelPath:     dc.VirtualPath,
		}
	} else if dc.CurSegment.StartCommit == 0 {
		// CurSegment was reset but not properly initialized - fix it
		dc.CurSegment.StartCommit = seg.StartCommit
		dc.CurSegment.StartTx = seg.StartTx
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
	// Use txSeq for EndTx (the txSeq allocated for this compaction),
	// not dc.CurSegment.EndTx (which is the EndTx from input segments)
	seg := &index.LogSegment{
		StartCommit: 0,
		EndCommit:   0,
		StartTx:     dc.CurSegment.StartTx,
		EndTx:       txSeq,
		RelPath:     dc.VirtualPath,
	}

	// FormatLogSegment includes RelPath in the name, but dc.Dir already points to that directory
	// So we need to extract just the base filename
	formatted := paths.FormatLogSegment(seg, dc.Level+1, true)
	_, filename := filepath.Split(formatted)
	if filename == "" {
		filename = formatted
	}
	df := &dfile.DiffFile{
		Seq:     txSeq,
		Diff:    diff,
		Inputs:  dc.Inputs,
		Pending: true,
	}
	writePath := filepath.Join(dc.Dir, filename)

	// Hypothesis #2: Log filename being written for comparison with rename
	dc.Config.Log.Debug("persistCurrent: writing pending file", "path", writePath, "seg", seg, "level", dc.Level+1)

	err = dfile.WriteDiffFile(writePath, df)
	if err != nil {
		return err
	}

	// Verify file exists immediately after write (hypothesis #1: test teardown)
	if _, statErr := os.Stat(writePath); statErr != nil {
		return fmt.Errorf("file does not exist immediately after WriteDiffFile (possible test teardown?): wrote %q but stat failed: %w", writePath, statErr)
	}

	// 3. Commit
	env.seq.Lock()
	defer env.seq.Unlock()
	commit, err := env.seq.NextCommitLocked()
	if err != nil {
		return err
	}

	// Set StartCommit to the first commit in the compacted range
	// CurSegment.StartCommit should never be 0 for a valid compaction window
	// (it's initialized from the first input segment's StartCommit)
	if dc.CurSegment.StartCommit == 0 {
		// This shouldn't happen - CurSegment should be initialized from input segments
		// Panic to catch the bug
		panic(fmt.Sprintf("BUG: CurSegment.StartCommit == 0 when creating level %d compacted segment: CurSegment=%v commit=%d", dc.Level+1, dc.CurSegment, commit))
	}

	// Ensure the newly allocated commit is greater than CurSegment.EndCommit
	// (the end of the range we've already compacted)
	// This can happen if:
	// 1. CurSegment was set from recovery (reading existing level 1 files)
	// 2. But the commit sequence state is stale or reset
	// 3. So we allocate a commit that's in the past
	//
	// If this happens, it means the commit sequence state doesn't match the files on disk.
	// We should read the current sequence state and ensure it's at least CurSegment.EndCommit + 1.
	if commit <= dc.CurSegment.EndCommit {
		// Read the current sequence state to see what it actually is
		currentState, readErr := env.seq.ReadStateLocked()
		currentCommit := int64(0)
		if readErr == nil {
			currentCommit = currentState.Commit
		}
		panic(fmt.Sprintf("BUG: newly allocated commit %d is not greater than CurSegment.EndCommit %d (commit sequence out of sync?): CurSegment=%v currentSequenceState.Commit=%d (after allocation, should be %d)", commit, dc.CurSegment.EndCommit, dc.CurSegment, currentCommit, commit))
	}

	seg.StartCommit = dc.CurSegment.StartCommit
	seg.EndCommit = commit
	// A level 1+ file should cover a range: StartCommit < EndCommit
	// This should now be guaranteed since commit > CurSegment.EndCommit >= CurSegment.StartCommit
	if seg.StartCommit == seg.EndCommit && dc.Level+1 > 0 {
		panic(fmt.Sprintf("BUG: invalid compacted segment: StartCommit == EndCommit == %d at level %d (should cover a range of commits): CurSegment=%v commit=%d", commit, dc.Level+1, dc.CurSegment, commit))
	}

	// Verify file still exists before rename (hypothesis #1: test teardown)
	if _, statErr := os.Stat(writePath); statErr != nil {
		return fmt.Errorf("file disappeared between WriteDiffFile and CommitPending (possible test teardown?): %q stat failed: %w", writePath, statErr)
	}

	// Hypothesis #2: Log what CommitPending will try to rename
	// Build the expected old filename to compare with what was written
	oldFormatted := paths.FormatLogSegment(seg.AsPending(), dc.Level+1, true)
	_, expectedOldName := filepath.Split(oldFormatted)
	if expectedOldName == "" {
		expectedOldName = oldFormatted
	}
	expectedOldPath := filepath.Join(dc.Dir, expectedOldName)
	dc.Config.Log.Debug("persistCurrent: about to rename", "wrote", writePath, "will rename", expectedOldPath, "seg", seg, "commit", commit)

	if writePath != expectedOldPath {
		return fmt.Errorf("filename mismatch: wrote %q but CommitPending will look for %q (hypothesis #2)", writePath, expectedOldPath)
	}

	if err := dfile.CommitPending(dc.Dir, seg, dc.Level+1, commit); err != nil {
		return err
	}

	// Verify the .diff file exists after rename (ensures file was actually created)
	newFormatted := paths.FormatLogSegment(seg, dc.Level+1, false)
	_, newName := filepath.Split(newFormatted)
	if newName == "" {
		newName = newFormatted
	}
	newPath := filepath.Join(dc.Dir, newName)
	if _, statErr := os.Stat(newPath); statErr != nil {
		return fmt.Errorf("compacted file does not exist after CommitPending (logd should have created this): %q stat failed: %w", newPath, statErr)
	}

	// 4. Index - file must exist on disk before adding to index
	env.idxL.Lock()
	defer env.idxL.Unlock()
	// Double-check file still exists before adding to index (defensive check)
	if _, statErr := os.Stat(newPath); statErr != nil {
		return fmt.Errorf("compacted file disappeared between verification and index add (logd should have created this): %q stat failed: %w", newPath, statErr)
	}
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

	// Notify Storage of compaction completion if callback is set
	if dc.Config.OnCompactionComplete != nil {
		dc.Config.OnCompactionComplete(dc.VirtualPath, dc.Ref, commit)
	}

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
	// FormatLogSegment includes RelPath in the name, but dc.Dir already points to that directory
	// So we need to extract just the base filename
	formatted := paths.FormatLogSegment(seg, lvl, false)
	// Extract just the filename (last component after the path separator)
	// If seg.RelPath is empty or ".", formatted will be just the filename
	// If seg.RelPath is set, formatted will be "RelPath/filename", so we extract just the filename
	_, name := filepath.Split(formatted)
	if name == "" {
		// If Split returned empty name, formatted was already just a filename
		name = formatted
	}
	p := filepath.Join(dc.Dir, name)
	return dfile.ReadDiffFile(p)
}

func bufferSize(divisor, level int) int {
	div := min(divisor, 16)
	const N = 3
	if level >= N {
		return 1
	}
	// pow(divisor, N - level)
	size := 1
	for i := 0; i < N-level; i++ {
		size *= div
	}
	return size
}
