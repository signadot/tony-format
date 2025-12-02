package compact

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/dfile"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/paths"
)

// hasLowerLevelWaiting checks if any level lower than the given level is waiting
// or if a lower level is currently holding the lock
func hasLowerLevelWaiting(env *storageEnv, level int) bool {
	// If a lower level is holding the lock, we must wait
	if env.readStateLevel != -1 && env.readStateLevel < level {
		return true
	}
	// If a lower level is in the waiters list, we must wait
	for _, waiterLevel := range env.readStateWaiters {
		if waiterLevel < level {
			return true
		}
	}
	return false
}

// insertWaiter adds a level to the waiters list in priority order (lower levels first)
func insertWaiter(env *storageEnv, level int) {
	// Insert in sorted order (lower levels first)
	inserted := false
	for i, waiterLevel := range env.readStateWaiters {
		if level < waiterLevel {
			// Insert before this position
			env.readStateWaiters = append(env.readStateWaiters[:i], append([]int{level}, env.readStateWaiters[i:]...)...)
			inserted = true
			break
		}
	}
	if !inserted {
		env.readStateWaiters = append(env.readStateWaiters, level)
	}
}

// removeWaiter removes a level from the waiters list
func removeWaiter(env *storageEnv, level int) {
	for i, waiterLevel := range env.readStateWaiters {
		if waiterLevel == level {
			env.readStateWaiters = append(env.readStateWaiters[:i], env.readStateWaiters[i+1:]...)
			return
		}
	}
}

// readState reads the runtime state from disk
// Must be serialized across all compaction levels to prevent races where
// one level's readState sees inconsistent state while another level's
// persistCurrent is allocating commits from the shared sequencer.
// Lower levels have priority to prevent blocking compaction progress.
func (dc *DirCompactor) readState(env *storageEnv) ([]index.LogSegment, error) {
	// Acquire lock with priority: lower levels (smaller Level values) get priority
	env.readStateMu.Lock()
	// Wait while a lower level is holding the lock or waiting
	for hasLowerLevelWaiting(env, dc.Level) {
		// Add ourselves to waiters list (maintained in priority order)
		insertWaiter(env, dc.Level)
		env.readStateCond.Wait()
		removeWaiter(env, dc.Level)
	}
	env.readStateLevel = dc.Level
	// Keep lock held for entire readState operation to prevent files from being deleted
	// between ReadDir and reading files
	defer func() {
		env.readStateLevel = -1
		env.readStateCond.Broadcast() // Wake all waiters, priority logic will select the right one
		env.readStateMu.Unlock()
	}()
	
	dirEnts, err := os.ReadDir(dc.Dir)
	if err != nil {
		// If directory doesn't exist, return empty segments (directory will be created on first write)
		// This handles the race where commits are creating directories concurrently with recovery
		if os.IsNotExist(err) {
			return []index.LogSegment{}, nil
		}
		return nil, err
	}

	inputSegs := []index.LogSegment{}
	curSegs := []index.LogSegment{}
	nextLevelExists := false
	parsedFilenames := []string{} // Debug: track what we parsed
	for _, de := range dirEnts {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		// Skip temporary files (.tmp) and pending files
		if strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".pending") {
			continue
		}
		seg, lvl, err := paths.ParseLogSegment(name)
		if err != nil {
			continue
		}
		parsedFilenames = append(parsedFilenames, name) // Debug
		
		// Set RelPath to the virtual path for this directory, since ParseLogSegment
		// only gets the filename and can't determine the virtual path
		seg.RelPath = dc.VirtualPath
		
		switch lvl {
		case dc.Level + 2:
			nextLevelExists = true
		case dc.Level + 1:
			curSegs = append(curSegs, *seg)
		case dc.Level:
			inputSegs = append(inputSegs, *seg)
		default:
		}
	}
	slices.SortFunc(inputSegs, index.LogSegCompare)
	slices.SortFunc(curSegs, index.LogSegCompare)

	// Store parsed filenames for debugging (used in error messages below)
	parsedFilenamesForDebug := parsedFilenames

	// Set CurSegment to the one with the highest EndCommit (most recent).
	// Since segments are non-overlapping and sorted by StartCommit, the last
	// segment in the sorted list should have the highest EndCommit.
	var curSeg *index.LogSegment
	if len(curSegs) > 0 {
		curSeg = &curSegs[len(curSegs)-1]
	}

	// Filter input segments that are already covered by ANY level+1 segment
	filteredInputs := []index.LogSegment{}
	for i := range inputSegs {
		seg := &inputSegs[i]
		// Check if this segment is covered by any level+1 segment
		covered := false
		for j := range curSegs {
			curSeg := &curSegs[j]
			if index.WithinCommitRange(seg, curSeg) {
				covered = true
				break
			}
		}
		if !covered {
			filteredInputs = append(filteredInputs, *seg)
		}
	}

	last := ir.Null()
	for i := range curSegs {
		seg := &curSegs[i]
		// Check for shutdown before reading each file
		select {
		case <-dc.done:
			return nil, fmt.Errorf("readState cancelled: shutdown requested")
		default:
		}
		formatted := paths.FormatLogSegment(seg, dc.Level+1, false)
		_, expectedName := filepath.Split(formatted)
		if expectedName == "" {
			expectedName = formatted
		}
		expectedPath := filepath.Join(dc.Dir, expectedName)
		
		// Check if file exists before trying to read
		if _, statErr := os.Stat(expectedPath); statErr != nil {
			// List directory to see what files actually exist
			dirEnts, listErr := os.ReadDir(dc.Dir)
			actualFiles := []string{}
			if listErr == nil {
				for _, de := range dirEnts {
					if !de.IsDir() && strings.HasSuffix(de.Name(), ".diff") {
						actualFiles = append(actualFiles, de.Name())
					}
				}
			}
			return nil, fmt.Errorf("recovery: level+1 segment file does not exist (logd should have created this): segment=%v expectedPath=%q expectedName=%q parsedFilenames=%v actualFiles=%v error=%w", seg, expectedPath, expectedName, parsedFilenamesForDebug, actualFiles, statErr)
		}
		
		// Read the file. If it doesn't exist, that's a bug - logd should have created it.
		df, err := dc.readSegment(seg, dc.Level+1)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("recovery: level+1 segment file does not exist (logd should have created this): segment=%v expectedPath=%q error=%w", seg, expectedPath, err)
			}
			return nil, err
		}
		tmp, err := tony.Patch(last, df.Diff)
		if err != nil {
			return nil, err
		}
		last = tmp
	}

	// Update state
	// Validate that level 1+ segments cover a range (StartCommit < EndCommit)
	if curSeg != nil && dc.Level+1 > 0 && curSeg.StartCommit == curSeg.EndCommit {
		return nil, fmt.Errorf("recovery: invalid level %d segment has StartCommit == EndCommit == %d (should cover a range): segment=%v", dc.Level+1, curSeg.StartCommit, curSeg)
	}
	dc.CurSegment = curSeg
	dc.Ref = last
	dc.Start = last
	// get inputs from CurSegment
	if curSeg != nil {
		// FormatLogSegment includes RelPath in the name, but dc.Dir already points to that directory
		// So we need to extract just the base filename
		formatted := paths.FormatLogSegment(curSeg, dc.Level+1, false)
		_, name := filepath.Split(formatted)
		p := filepath.Join(dc.Dir, name)
		df, err := dfile.ReadDiffFile(p)
		if err != nil {
			return nil, err
		}
		dc.Inputs = df.Inputs

		// Notify Storage of recovered compacted state if callback is set
		if dc.Config.OnCompactionComplete != nil {
			dc.Config.OnCompactionComplete(dc.VirtualPath, dc.Ref, curSeg.EndCommit)
		}
	}

	// Initialize Next compactor if Level+2 segments exist
	// but don't re-create it if we're in a recovery that has
	// already started it
	if dc.Next == nil && nextLevelExists {
		dc.Next = NewDirCompactor(&dc.Config, dc.Level+1, dc.Dir, dc.VirtualPath, env)
	}

	return filteredInputs, nil
}
