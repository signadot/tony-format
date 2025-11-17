package storage

import (
	"os"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

func TestWriteReadSnapshot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Create a test state
	state := ir.FromMap(map[string]*ir.Node{
		"key1": &ir.Node{Type: ir.StringType, String: "value1"},
		"key2": &ir.Node{Type: ir.NumberType, Int64: intPtr(int64(42)), Number: "42"},
	})

	// Write snapshot
	virtualPath := "/test/path"
	commitCount := int64(100)
	if err := s.WriteSnapshot(virtualPath, commitCount, state); err != nil {
		t.Fatalf("failed to write snapshot: %v", err)
	}

	// Read snapshot
	snapshot, err := s.ReadSnapshot(virtualPath, commitCount)
	if err != nil {
		t.Fatalf("failed to read snapshot: %v", err)
	}

	if snapshot.CommitCount != commitCount {
		t.Errorf("expected commit count %d, got %d", commitCount, snapshot.CommitCount)
	}

	if snapshot.Path != virtualPath {
		t.Errorf("expected path %s, got %s", virtualPath, snapshot.Path)
	}

	if snapshot.Timestamp == "" {
		t.Error("expected timestamp to be set")
	}

	// Verify state
	if snapshot.State == nil {
		t.Fatal("expected state to be set")
	}

	key1 := ir.Get(snapshot.State, "key1")
	if key1 == nil || key1.String != "value1" {
		t.Errorf("expected key1 to be 'value1', got %v", key1)
	}
}

func TestListSnapshots(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	virtualPath := "/test/path"
	state := ir.Null()

	// Write multiple snapshots
	commitCounts := []int64{10, 50, 100, 200}
	for _, commitCount := range commitCounts {
		if err := s.WriteSnapshot(virtualPath, commitCount, state); err != nil {
			t.Fatalf("failed to write snapshot at %d: %v", commitCount, err)
		}
	}

	// List snapshots
	snapshots, err := s.ListSnapshots(virtualPath)
	if err != nil {
		t.Fatalf("failed to list snapshots: %v", err)
	}

	if len(snapshots) != len(commitCounts) {
		t.Errorf("expected %d snapshots, got %d", len(commitCounts), len(snapshots))
	}

	// Verify they're sorted
	for i := 1; i < len(snapshots); i++ {
		if snapshots[i] <= snapshots[i-1] {
			t.Errorf("snapshots not sorted: %d <= %d", snapshots[i], snapshots[i-1])
		}
	}

	// Verify all commit counts are present
	found := make(map[int64]bool)
	for _, commitCount := range snapshots {
		found[commitCount] = true
	}
	for _, commitCount := range commitCounts {
		if !found[commitCount] {
			t.Errorf("snapshot at commit count %d not found", commitCount)
		}
	}
}

func TestFindNearestSnapshot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	virtualPath := "/test/path"
	state := ir.Null()

	// Write snapshots at specific commit counts
	commitCounts := []int64{10, 50, 100, 200}
	for _, commitCount := range commitCounts {
		if err := s.WriteSnapshot(virtualPath, commitCount, state); err != nil {
			t.Fatalf("failed to write snapshot at %d: %v", commitCount, err)
		}
	}

	tests := []struct {
		targetCommitCount int64
		expectedNearest    int64
	}{
		{5, 0},   // Before first snapshot
		{10, 10}, // Exact match
		{25, 10}, // Between 10 and 50
		{50, 50}, // Exact match
		{75, 50}, // Between 50 and 100
		{100, 100}, // Exact match
		{150, 100}, // Between 100 and 200
		{200, 200}, // Exact match
		{300, 200}, // After last snapshot
	}

	for _, test := range tests {
		nearest, err := s.FindNearestSnapshot(virtualPath, test.targetCommitCount)
		if err != nil {
			t.Fatalf("failed to find nearest snapshot for %d: %v", test.targetCommitCount, err)
		}

		if nearest != test.expectedNearest {
			t.Errorf("for target %d, expected nearest %d, got %d", test.targetCommitCount, test.expectedNearest, nearest)
		}
	}
}

func TestFindNearestSnapshot_NoSnapshots(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	virtualPath := "/test/path"
	nearest, err := s.FindNearestSnapshot(virtualPath, 100)
	if err != nil {
		t.Fatalf("failed to find nearest snapshot: %v", err)
	}

	if nearest != 0 {
		t.Errorf("expected 0 for no snapshots, got %d", nearest)
	}
}

func TestDeleteSnapshot(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "logd-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	virtualPath := "/test/path"
	state := ir.Null()
	commitCount := int64(100)

	// Write snapshot
	if err := s.WriteSnapshot(virtualPath, commitCount, state); err != nil {
		t.Fatalf("failed to write snapshot: %v", err)
	}

	// Verify it exists
	_, err = s.ReadSnapshot(virtualPath, commitCount)
	if err != nil {
		t.Fatalf("snapshot should exist: %v", err)
	}

	// Delete snapshot
	if err := s.DeleteSnapshot(virtualPath, commitCount); err != nil {
		t.Fatalf("failed to delete snapshot: %v", err)
	}

	// Verify it's gone
	_, err = s.ReadSnapshot(virtualPath, commitCount)
	if err == nil {
		t.Error("snapshot should not exist after deletion")
	}
}
