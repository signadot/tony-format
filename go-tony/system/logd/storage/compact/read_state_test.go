package compact

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/dfile"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/paths"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/seq"
)

func TestReadState_BasicHappyPath(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	sequence := seq.NewSeq(tmpDir)
	idxL := &sync.Mutex{}
	idx := index.NewIndex("")

	// Create DirCompactor at Level 0
	dc := NewDirCompactor(&Config{Divisor: 2}, 0, tmpDir, "/test", sequence, idxL, idx)

	// Build states and diffs using tony.Diff
	state0 := ir.Null()
	state1 := ir.FromMap(map[string]*ir.Node{"a": ir.FromInt(1)})
	state2 := ir.FromMap(map[string]*ir.Node{"a": ir.FromInt(1), "b": ir.FromInt(2)})

	diff1 := tony.Diff(state0, state1)
	diff2 := tony.Diff(state1, state2)

	// Write Level 0 input segments (granular diffs)
	input1 := index.PointLogSegment(1, 1, "")
	filename1 := paths.FormatLogSegment(input1, 0, false)
	df1 := &dfile.DiffFile{
		Seq:    1,
		Diff:   diff1,
		Inputs: 1,
	}
	if err := dfile.WriteDiffFile(filepath.Join(tmpDir, filename1), df1); err != nil {
		t.Fatalf("failed to write input segment 1: %v", err)
	}

	input2 := index.PointLogSegment(2, 2, "")
	filename2 := paths.FormatLogSegment(input2, 0, false)
	df2 := &dfile.DiffFile{
		Seq:    2,
		Diff:   diff2,
		Inputs: 1,
	}
	if err := dfile.WriteDiffFile(filepath.Join(tmpDir, filename2), df2); err != nil {
		t.Fatalf("failed to write input segment 2: %v", err)
	}

	// Write Level 1 compacted segment (current output)
	// This is the diff from null to state2 (compacted)
	compactedDiff := tony.Diff(state0, state2)
	compacted := &index.LogSegment{
		StartCommit: 1,
		StartTx:     1,
		EndCommit:   2,
		EndTx:       2,
		RelPath:     "",
	}
	filenameCompacted := paths.FormatLogSegment(compacted, 1, false)
	dfCompacted := &dfile.DiffFile{
		Seq:    2,
		Diff:   compactedDiff,
		Inputs: 2,
	}
	if err := dfile.WriteDiffFile(filepath.Join(tmpDir, filenameCompacted), dfCompacted); err != nil {
		t.Fatalf("failed to write compacted segment: %v", err)
	}

	// Read state
	inputSegs, err := dc.ReadState(sequence, idxL, idx)
	if err != nil {
		t.Fatalf("ReadState failed: %v", err)
	}

	// Verify input segments returned (should be empty as they are compacted)
	if len(inputSegs) != 0 {
		t.Errorf("expected 0 input segments, got %d", len(inputSegs))
	}

	// Verify CurSegment was loaded
	if dc.CurSegment == nil {
		t.Fatal("CurSegment should be loaded")
	}
	if dc.CurSegment.EndCommit != 2 { // Changed from 4 to 2 to match test setup
		t.Errorf("expected CurSegment EndCommit 2, got %d", dc.CurSegment.EndCommit)
	}

	// Verify Inputs count (from compacted file)
	if dc.Inputs != 2 {
		t.Errorf("expected Inputs 2, got %d", dc.Inputs)
	}

	// Verify Next is nil (no Level 2 segments)
	if dc.Next != nil {
		t.Error("Next should be nil")
	}

	// Verify Ref matches state2
	if !dc.Ref.DeepEqual(state2) {
		t.Error("Ref does not match expected state2")
	}
}

func createDiffFile(t *testing.T, dir string, seg *index.LogSegment, level int, diff *ir.Node, txSeq int64) {
	filename := paths.FormatLogSegment(seg, level, false)
	df := &dfile.DiffFile{
		Seq:    txSeq,
		Diff:   diff,
		Inputs: 1,
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := dfile.WriteDiffFile(filepath.Join(dir, filename), df); err != nil {
		t.Fatalf("failed to write diff file: %v", err)
	}
}
