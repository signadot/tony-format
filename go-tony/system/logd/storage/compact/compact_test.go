package compact

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/dfile"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/paths"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/seq"
)

// testSetup creates a complete test environment with temp directory, config, sequence, index, and compactor
func testSetup(t *testing.T, divisor int) (tmpDir string, cfg *Config, c *Compactor, env *storageEnv) {
	t.Helper()
	tmpDir = t.TempDir()
	cfg = &Config{
		Divisor: divisor,
		Root:    tmpDir,
	}

	// Create required directories
	if err := os.MkdirAll(filepath.Join(tmpDir, "meta"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "paths"), 0755); err != nil {
		t.Fatal(err)
	}

	sequence := seq.NewSeq(tmpDir)
	idx := index.NewIndex("")
	idxMu := &sync.Mutex{}
	env = &storageEnv{seq: sequence, idxL: idxMu, idx: idx}

	c = NewCompactor(cfg, sequence, idxMu, idx)
	return tmpDir, cfg, c, env
}

// createSegment creates a segment file on disk and returns the segment.
// If compactor is provided, it also sends the segment to the compactor.
func createSegment(t *testing.T, tmpDir, virtualPath string, level int, txSeq, commitSeq int64, diff *ir.Node, compactor *Compactor) *index.LogSegment {
	t.Helper()
	seg := index.PointLogSegment(commitSeq, txSeq, virtualPath)
	filename := paths.FormatLogSegment(seg, level, false)
	df := &dfile.DiffFile{
		Seq:    txSeq,
		Diff:   diff,
		Inputs: 1,
	}
	dir := paths.PathToFilesystem(tmpDir, virtualPath)
	if err := dfile.WriteDiffFile(filepath.Join(dir, filename), df); err != nil {
		t.Fatalf("failed to write segment: %v", err)
	}
	if compactor != nil {
		if err := compactor.OnNewSegment(seg); err != nil {
			t.Fatalf("OnNewSegment failed: %v", err)
		}
	}
	return seg
}

// waitForSegmentCount waits for the index to have a specific number of segments for a path.
func waitForSegmentCount(t *testing.T, idx *index.Index, virtualPath string, expectedCount int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		idx.RLock()
		segs := idx.LookupRange(virtualPath, nil, nil)
		idx.RUnlock()
		
		if len(segs) >= expectedCount {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	idx.RLock()
	segs := idx.LookupRange(virtualPath, nil, nil)
	idx.RUnlock()
	t.Fatalf("expected %d segments for path %q, got %d within %v", expectedCount, virtualPath, len(segs), timeout)
}

// TestCompactionLogic tests that compaction works correctly:
// - Sending segments triggers compaction when divisor is reached
// - Compacted segments appear in the index
// - Compacted segments contain the correct state
func TestCompactionLogic(t *testing.T) {
	// Setup
	tmpDir, _, c, env := testSetup(t, 2)
	virtualPath := ""

	// 1. Send first segment
	// State: {a: 1}
	diff1 := ir.FromMap(map[string]*ir.Node{"a": ir.FromInt(1)})
	seg1 := createSegment(t, tmpDir, virtualPath, 0, 1, 1, diff1, c)

	// Verify no compaction yet - index should be empty (input segments aren't indexed)
	// We'll verify compaction by checking the index after sending the second segment
	env.idx.RLock()
	segsBefore := env.idx.LookupRange(virtualPath, nil, nil)
	env.idx.RUnlock()
	if len(segsBefore) > 0 {
		t.Logf("Note: found %d segments before compaction (may be from previous test runs)", len(segsBefore))
	}

	// 2. Send second segment (should trigger compaction)
	// State: {a: 1, b: 2}
	diff2 := ir.FromMap(map[string]*ir.Node{"b": ir.FromInt(2)})
	seg2 := createSegment(t, tmpDir, virtualPath, 0, 2, 2, diff2, c)

	// Wait for compaction to complete - a segment should appear in the index
	waitForSegmentCount(t, env.idx, virtualPath, 1, 5*time.Second)

	// Wait for the segment to be committed (EndCommit should be set)
	var compactedSeg *index.LogSegment
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		env.idx.RLock()
		segs := env.idx.LookupRange(virtualPath, nil, nil)
		env.idx.RUnlock()

		if len(segs) == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// Find a segment that covers our transaction range and is committed
		// The commit numbers may differ from tx numbers, so we check tx range
		// A segment is committed if EndCommit > 0 (pending segments have EndCommit=0)
		for i := range segs {
			seg := &segs[i]
			if seg.RelPath == virtualPath && 
			   seg.StartTx <= seg1.StartTx && 
			   seg.EndTx >= seg2.EndTx &&
			   seg.EndCommit > 0 { // Committed (pending segments have EndCommit=0)
				compactedSeg = seg
				break
			}
		}
		
		if compactedSeg != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if compactedSeg == nil {
		env.idx.RLock()
		segs := env.idx.LookupRange(virtualPath, nil, nil)
		env.idx.RUnlock()
		t.Fatalf("no committed compacted segment found covering range [%d-%d]. Found segments: %+v", seg1.StartCommit, seg2.EndCommit, segs)
	}

	// Verify compacted segment content by reading from disk
	// Use the segment metadata to construct the filename
	filename := paths.FormatLogSegment(compactedSeg, 1, false) // Level 1 compaction
	compactedPath := filepath.Join(paths.PathToFilesystem(tmpDir, virtualPath), filename)
	dfCompacted, err := dfile.ReadDiffFile(compactedPath)
	if err != nil {
		t.Fatalf("failed to read compacted file %s: %v", filename, err)
	}

	// Verify compacted diff content - should represent {a: 1, b: 2}
	expected := ir.FromMap(map[string]*ir.Node{"a": ir.FromInt(1), "b": ir.FromInt(2)})
	reconstructed, err := tony.Patch(ir.Null(), dfCompacted.Diff)
	if err != nil {
		t.Fatalf("failed to patch compacted diff: %v", err)
	}
	if !expected.DeepEqual(reconstructed) {
		t.Errorf("compacted state mismatch: expected %v, got %v", expected, reconstructed)
	}

	// Verify segment metadata
	if dfCompacted.Inputs != 2 {
		t.Errorf("expected Inputs=2 in compacted segment, got %d", dfCompacted.Inputs)
	}
}

// TestFileRemoval tests that input segment files are removed after compaction
// when Config.Remove returns true.
func TestFileRemoval(t *testing.T) {
	// Setup with Remove function that removes Level 1 segments
	tmpDir, _, c, _ := testSetup(t, 2)
	virtualPath := ""
	
	// Configure Remove to return true for Level 1 compactions
	c.Config.Remove = func(commit, level int) bool {
		return level == 1 // Remove files when creating Level 1 segments
	}

	// Create two segments that will trigger compaction
	diff1 := ir.FromMap(map[string]*ir.Node{"a": ir.FromInt(1)})
	createSegment(t, tmpDir, virtualPath, 0, 1, 1, diff1, c)
	
	diff2 := ir.FromMap(map[string]*ir.Node{"b": ir.FromInt(2)})
	createSegment(t, tmpDir, virtualPath, 0, 2, 2, diff2, c)

	// Wait for compaction
	waitForSegmentCount(t, c.Index, virtualPath, 1, 5*time.Second)

	// Wait for compaction to fully complete and file removal to happen
	// Need to wait for the segment to be committed (not just indexed)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.Index.RLock()
		segs := c.Index.LookupRange(virtualPath, nil, nil)
		c.Index.RUnlock()
		
		// Check if we have a committed segment
		hasCommitted := false
		for i := range segs {
			if segs[i].RelPath == virtualPath && segs[i].EndCommit > 0 {
				hasCommitted = true
				break
			}
		}
		if hasCommitted {
			// Give a bit more time for file removal to complete
			time.Sleep(200 * time.Millisecond)
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify input segment files were removed
	dir := paths.PathToFilesystem(tmpDir, virtualPath)
	dirEnts, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory: %v", err)
	}

	// Count Level 0 segments (should be 0 if removal worked)
	level0Count := 0
	level1Count := 0
	for _, de := range dirEnts {
		if de.IsDir() {
			continue
		}
		seg, lvl, err := paths.ParseLogSegment(de.Name())
		if err != nil {
			continue
		}
		if seg.RelPath == virtualPath {
			if lvl == 0 {
				level0Count++
			} else if lvl == 1 {
				level1Count++
			}
		}
	}

	if level0Count > 0 {
		t.Errorf("expected 0 Level 0 segments after removal, found %d", level0Count)
	}
	if level1Count == 0 {
		t.Error("expected Level 1 compacted segment to exist")
	}
}

// TestFileRemovalDisabled tests that files are NOT removed when Config.Remove returns false.
func TestFileRemovalDisabled(t *testing.T) {
	// Setup with Remove function that never removes
	tmpDir, _, c, _ := testSetup(t, 2)
	virtualPath := ""
	
	// Configure Remove to never remove files (using helper function)
	c.Config.Remove = NeverRemove()

	// Create two segments that will trigger compaction
	diff1 := ir.FromMap(map[string]*ir.Node{"a": ir.FromInt(1)})
	createSegment(t, tmpDir, virtualPath, 0, 1, 1, diff1, c)
	
	diff2 := ir.FromMap(map[string]*ir.Node{"b": ir.FromInt(2)})
	createSegment(t, tmpDir, virtualPath, 0, 2, 2, diff2, c)

	// Wait for compaction
	waitForSegmentCount(t, c.Index, virtualPath, 1, 5*time.Second)

	// Wait a bit to ensure removal would have happened if enabled
	time.Sleep(100 * time.Millisecond)

	// Verify input segment files were NOT removed
	dir := paths.PathToFilesystem(tmpDir, virtualPath)
	dirEnts, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read directory: %v", err)
	}

	// Count Level 0 segments (should be 2 if removal didn't happen)
	level0Count := 0
	level1Count := 0
	for _, de := range dirEnts {
		if de.IsDir() {
			continue
		}
		seg, lvl, err := paths.ParseLogSegment(de.Name())
		if err != nil {
			continue
		}
		if seg.RelPath == virtualPath {
			if lvl == 0 {
				level0Count++
			} else if lvl == 1 {
				level1Count++
			}
		}
	}

	if level0Count != 2 {
		t.Errorf("expected 2 Level 0 segments when removal disabled, found %d", level0Count)
	}
	if level1Count == 0 {
		t.Error("expected Level 1 compacted segment to exist")
	}
}

// TestHeadWindowStrategy tests that HeadWindow removal strategy works correctly
// in a real compaction scenario.
func TestHeadWindowStrategy(t *testing.T) {
	tmpDir, _, c, _ := testSetup(t, 2)
	virtualPath := ""
	
	// Track current commit
	currentCommit := 0
	getCurrentCommit := func() int { return currentCommit }
	
	// Configure HeadWindow to keep only the 2 most recent commits
	c.Config.Remove = HeadWindow(getCurrentCommit, 2)
	
	// Create segments at commits 1, 2, 3, 4
	diff1 := ir.FromMap(map[string]*ir.Node{"a": ir.FromInt(1)})
	createSegment(t, tmpDir, virtualPath, 0, 1, 1, diff1, c)
	currentCommit = 1
	
	diff2 := ir.FromMap(map[string]*ir.Node{"b": ir.FromInt(2)})
	createSegment(t, tmpDir, virtualPath, 0, 2, 2, diff2, c)
	// Set currentCommit to 3 so HeadWindow(keep=2) will return true (3 > 2)
	currentCommit = 3
	
	// Wait for first compaction (commits 1-2)
	waitForSegmentCount(t, c.Index, virtualPath, 1, 5*time.Second)
	
	// Wait for committed segment (EndCommit > 0)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.Index.RLock()
		segs := c.Index.LookupRange(virtualPath, nil, nil)
		c.Index.RUnlock()
		hasCommitted := false
		for i := range segs {
			if segs[i].RelPath == virtualPath && segs[i].EndCommit > 0 {
				hasCommitted = true
				break
			}
		}
		if hasCommitted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	
	// Wait for Level 0 segments to be removed (poll until count is 0)
	dir := paths.PathToFilesystem(tmpDir, virtualPath)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		dirEnts, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("failed to read directory: %v", err)
		}
		level0Count := 0
		for _, de := range dirEnts {
			if de.IsDir() {
				continue
			}
			seg, lvl, err := paths.ParseLogSegment(de.Name())
			if err != nil {
				continue
			}
			if seg.RelPath == virtualPath && lvl == 0 {
				level0Count++
			}
		}
		if level0Count == 0 {
			break // Success - all Level 0 segments removed
		}
		time.Sleep(50 * time.Millisecond)
	}
	
	// Final check - should have 0 Level 0 segments
	dirEnts, _ := os.ReadDir(dir)
	level0Count := 0
	for _, de := range dirEnts {
		if de.IsDir() {
			continue
		}
		seg, lvl, err := paths.ParseLogSegment(de.Name())
		if err != nil {
			continue
		}
		if seg.RelPath == virtualPath && lvl == 0 {
			level0Count++
		}
	}
	if level0Count > 0 {
		t.Errorf("expected 0 Level 0 segments after removal, found %d", level0Count)
	}
	
	// Create more segments
	diff3 := ir.FromMap(map[string]*ir.Node{"c": ir.FromInt(3)})
	createSegment(t, tmpDir, virtualPath, 0, 3, 3, diff3, c)
	currentCommit = 3
	
	diff4 := ir.FromMap(map[string]*ir.Node{"d": ir.FromInt(4)})
	createSegment(t, tmpDir, virtualPath, 0, 4, 4, diff4, c)
	// Keep currentCommit at 4 (or higher) so HeadWindow(keep=2) continues to return true (4 > 2)
	currentCommit = 4
	
	// Wait for second compaction (commits 3-4)
	waitForSegmentCount(t, c.Index, virtualPath, 2, 5*time.Second)
	
	// Wait for second committed segment
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.Index.RLock()
		segs := c.Index.LookupRange(virtualPath, nil, nil)
		c.Index.RUnlock()
		committedCount := 0
		for i := range segs {
			if segs[i].RelPath == virtualPath && segs[i].EndCommit > 0 {
				committedCount++
			}
		}
		if committedCount >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	
	// Wait for Level 0 segments to be removed
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		dirEnts2, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("failed to read directory: %v", err)
		}
		level0Count2 := 0
		for _, de := range dirEnts2 {
			if de.IsDir() {
				continue
			}
			seg, lvl, err := paths.ParseLogSegment(de.Name())
			if err != nil {
				continue
			}
			if seg.RelPath == virtualPath && lvl == 0 {
				level0Count2++
			}
		}
		if level0Count2 == 0 {
			break // Success - all Level 0 segments removed
		}
		time.Sleep(50 * time.Millisecond)
	}
	
	// Final check - should have 0 Level 0 segments
	dirEnts2, _ := os.ReadDir(dir)
	level0Count2 := 0
	for _, de := range dirEnts2 {
		if de.IsDir() {
			continue
		}
		seg, lvl, err := paths.ParseLogSegment(de.Name())
		if err != nil {
			continue
		}
		if seg.RelPath == virtualPath && lvl == 0 {
			level0Count2++
		}
	}
	if level0Count2 > 0 {
		t.Errorf("expected 0 Level 0 segments after second compaction, found %d", level0Count2)
	}
}
