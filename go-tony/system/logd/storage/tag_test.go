package storage

import (
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
)

func TestSparseArrayTagPreservation(t *testing.T) {
	// Create storage
	tmpDir := t.TempDir()
	s, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a sparse array diff
	diff := ir.FromIntKeysMap(map[uint32]*ir.Node{
		1: ir.FromString("one"),
	})

	// Verify it has the tag initially
	if !HasSparseArrayTag(diff) {
		t.Fatal("Created diff missing sparse array tag")
	}

	// Write it
	timestamp := time.Now().UTC().Format(time.RFC3339)
	commitCount, txSeq, err := s.WriteDiffAtomically("/test/sparse", timestamp, diff, false)
	if err != nil {
		t.Fatal(err)
	}

	// Read it back
	readDiffFile, err := s.ReadDiff("/test/sparse", commitCount, txSeq, false)
	if err != nil {
		t.Fatal(err)
	}

	// Verify tag is preserved
	if !HasSparseArrayTag(readDiffFile.Diff) {
		t.Errorf("Read diff missing sparse array tag. Tag: %q", readDiffFile.Diff.Tag)
	}
}
