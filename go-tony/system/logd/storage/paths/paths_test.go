package paths

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

func TestFormatParseRoundtrip_PointSegment(t *testing.T) {
	seg := index.PointLogSegment(42, 100, "")
	level := 0

	// Format
	filename := FormatLogSegment(seg, level, false)

	// Parse
	parsed, parsedLevel, err := ParseLogSegment(filename)
	if err != nil {
		t.Fatalf("ParseLogSegment failed: %v", err)
	}

	// Verify
	if parsedLevel != level {
		t.Errorf("level = %d, want %d", parsedLevel, level)
	}
	if parsed.StartCommit != 42 {
		t.Errorf("StartCommit = %d, want 42", parsed.StartCommit)
	}
	if parsed.StartTx != 100 {
		t.Errorf("StartTx = %d, want 100", parsed.StartTx)
	}
	if !parsed.IsPoint() {
		t.Error("parsed segment should be a point segment")
	}
}

func TestFormatParseRoundtrip_CompactedSegment(t *testing.T) {
	seg := &index.LogSegment{
		StartCommit: 1,
		StartTx:     10,
		EndCommit:   5,
		EndTx:       50,
		RelPath:     "",
	}
	level := 1

	// Format
	filename := FormatLogSegment(seg, level, false)
	t.Logf("Formatted filename: %s", filename)

	// Parse
	parsed, parsedLevel, err := ParseLogSegment(filename)
	if err != nil {
		t.Fatalf("ParseLogSegment failed: %v", err)
	}

	// Verify level
	if parsedLevel != level {
		t.Errorf("level = %d, want %d", parsedLevel, level)
	}

	// Verify start
	if parsed.StartCommit != 1 {
		t.Errorf("StartCommit = %d, want 1", parsed.StartCommit)
	}
	if parsed.StartTx != 10 {
		t.Errorf("StartTx = %d, want 10", parsed.StartTx)
	}

	// Verify end
	if parsed.EndCommit != 5 {
		t.Errorf("EndCommit = %d, want 5", parsed.EndCommit)
	}
	if parsed.EndTx != 50 {
		t.Errorf("EndTx = %d, want 50", parsed.EndTx)
	}

	// Verify it's not a point
	if parsed.IsPoint() {
		t.Error("parsed segment should not be a point segment")
	}
}

func TestFormatParseRoundtrip_CompactedSegmentLevel2(t *testing.T) {
	seg := &index.LogSegment{
		StartCommit: 100,
		StartTx:     1000,
		EndCommit:   200,
		EndTx:       2000,
		RelPath:     "",
	}
	level := 2

	// Format
	filename := FormatLogSegment(seg, level, false)
	t.Logf("Formatted filename: %s", filename)

	// Parse
	parsed, parsedLevel, err := ParseLogSegment(filename)
	if err != nil {
		t.Fatalf("ParseLogSegment failed: %v", err)
	}

	// Verify level
	if parsedLevel != level {
		t.Errorf("level = %d, want %d", parsedLevel, level)
	}

	// Verify commits
	if parsed.StartCommit != 100 || parsed.EndCommit != 200 {
		t.Errorf("commit range = [%d, %d], want [100, 200]",
			parsed.StartCommit, parsed.EndCommit)
	}
}

func TestFormatLogSegment_CompactedFormat(t *testing.T) {
	seg := &index.LogSegment{
		StartCommit: 1,
		StartTx:     1,
		EndCommit:   2,
		EndTx:       2,
		RelPath:     "",
	}

	filename := FormatLogSegment(seg, 1, false)

	// Should be: a1.a1-a2.a2-1.diff
	expected := "a1.a1-a2.a2-1.diff"
	if filename != expected {
		t.Errorf("filename = %q, want %q", filename, expected)
	}
}

func TestParseLogSegment_CompactedFormat(t *testing.T) {
	// New format with level
	filename := "a1.a1-a2.a2-1.diff"

	seg, level, err := ParseLogSegment(filename)
	if err != nil {
		t.Fatalf("ParseLogSegment failed: %v", err)
	}

	if level != 1 {
		t.Errorf("level = %d, want 1", level)
	}

	if seg.StartCommit != 1 || seg.EndCommit != 2 {
		t.Errorf("commit range = [%d, %d], want [1, 2]",
			seg.StartCommit, seg.EndCommit)
	}

	if seg.StartTx != 1 || seg.EndTx != 2 {
		t.Errorf("tx range = [%d, %d], want [1, 2]",
			seg.StartTx, seg.EndTx)
	}
}

func TestParseLogSegment_CompactedFormatBackwardsCompat(t *testing.T) {
	// Old format without level (should default to 0)
	filename := "a1.a1-a2.a2.diff"

	seg, level, err := ParseLogSegment(filename)
	if err != nil {
		t.Fatalf("ParseLogSegment failed: %v", err)
	}

	if level != 0 {
		t.Errorf("level = %d, want 0 (backwards compat)", level)
	}

	if seg.StartCommit != 1 || seg.EndCommit != 2 {
		t.Errorf("commit range = [%d, %d], want [1, 2]",
			seg.StartCommit, seg.EndCommit)
	}
}
