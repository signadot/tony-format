package paths

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

func TestFormatPending_PointSegment(t *testing.T) {
	seg := index.PointLogSegment(0, 100, "")
	filename := FormatLogSegment(seg, 0, true)

	expected := "c100.pending"
	if filename != expected {
		t.Errorf("filename = %q, want %q", filename, expected)
	}
}

func TestFormatPending_CompactedSegment(t *testing.T) {
	seg := &index.LogSegment{
		StartCommit: 0,
		EndCommit:   0,
		StartTx:     10,
		EndTx:       50,
		RelPath:     "",
	}
	filename := FormatLogSegment(seg, 1, true)

	// Should be: startTx-endTx-level.pending
	expected := "b10-b50-1.pending"
	if filename != expected {
		t.Errorf("filename = %q, want %q", filename, expected)
	}
}

func TestParsePending_PointSegment(t *testing.T) {
	filename := "c100.pending"

	seg, level, err := ParseLogSegment(filename)
	if err != nil {
		t.Fatalf("ParseLogSegment failed: %v", err)
	}

	if level != 0 {
		t.Errorf("level = %d, want 0", level)
	}

	if seg.StartTx != 100 || seg.EndTx != 100 {
		t.Errorf("tx = [%d, %d], want [100, 100]", seg.StartTx, seg.EndTx)
	}

	if seg.StartCommit != 0 || seg.EndCommit != 0 {
		t.Errorf("commit = [%d, %d], want [0, 0]", seg.StartCommit, seg.EndCommit)
	}

	if !seg.IsPoint() {
		t.Error("pending point segment should be IsPoint()")
	}
}

func TestParsePending_CompactedSegment(t *testing.T) {
	filename := "b10-b50-1.pending"

	seg, level, err := ParseLogSegment(filename)
	if err != nil {
		t.Fatalf("ParseLogSegment failed: %v", err)
	}

	if level != 1 {
		t.Errorf("level = %d, want 1", level)
	}

	if seg.StartTx != 10 || seg.EndTx != 50 {
		t.Errorf("tx range = [%d, %d], want [10, 50]", seg.StartTx, seg.EndTx)
	}

	if seg.StartCommit != 0 || seg.EndCommit != 0 {
		t.Errorf("commit = [%d, %d], want [0, 0]", seg.StartCommit, seg.EndCommit)
	}

	if seg.IsPoint() {
		t.Error("pending compacted segment should not be IsPoint()")
	}
}

func TestFormatParseRoundtrip_PendingCompacted(t *testing.T) {
	seg := &index.LogSegment{
		StartCommit: 0,
		EndCommit:   0,
		StartTx:     100,
		EndTx:       200,
		RelPath:     "",
	}
	level := 2

	// Format as pending
	filename := FormatLogSegment(seg, level, true)
	t.Logf("Formatted pending filename: %s", filename)

	// Parse
	parsed, parsedLevel, err := ParseLogSegment(filename)
	if err != nil {
		t.Fatalf("ParseLogSegment failed: %v", err)
	}

	// Verify
	if parsedLevel != level {
		t.Errorf("level = %d, want %d", parsedLevel, level)
	}

	if parsed.StartTx != 100 || parsed.EndTx != 200 {
		t.Errorf("tx range = [%d, %d], want [100, 200]", parsed.StartTx, parsed.EndTx)
	}

	if parsed.StartCommit != 0 || parsed.EndCommit != 0 {
		t.Errorf("commit should be [0, 0], got [%d, %d]", parsed.StartCommit, parsed.EndCommit)
	}
}
