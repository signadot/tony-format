package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tony-format/tony/encode"
	"github.com/tony-format/tony/ir"
	"github.com/tony-format/tony/parse"
)

// Snapshot represents a snapshot of state at a specific commit count.
type Snapshot struct {
	CommitCount int64
	Path        string
	Timestamp   string
	State       *ir.Node
}

// snapshotToFilesystem converts a virtual path to a snapshot directory path.
// Example: "/proc/processes" -> "/logd/snapshots/proc/processes"
func (s *Storage) snapshotToFilesystem(virtualPath string) string {
	// Remove leading slash if present, then join with snapshots directory
	path := virtualPath
	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}
	return filepath.Join(s.root, "snapshots", path)
}

// snapshotFilename returns the filename for a snapshot at a given commit count.
func snapshotFilename(commitCount int64) string {
	return fmt.Sprintf("%d.snapshot", commitCount)
}

// WriteSnapshot writes a snapshot file atomically.
func (s *Storage) WriteSnapshot(virtualPath string, commitCount int64, state *ir.Node) error {
	snapshotDir := s.snapshotToFilesystem(virtualPath)
	if err := s.mkdirAll(snapshotDir, 0755); err != nil {
		return fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	filename := snapshotFilename(commitCount)
	filePath := filepath.Join(snapshotDir, filename)

	// Create the snapshot file structure
	timestamp := time.Now().UTC().Format(time.RFC3339)
	commitCountNode := &ir.Node{Type: ir.NumberType, Int64: &commitCount, Number: strconv.FormatInt(commitCount, 10)}
	snapshotFile := ir.FromMap(map[string]*ir.Node{
		"commitCount": commitCountNode,
		"timestamp":   &ir.Node{Type: ir.StringType, String: timestamp},
		"path":        &ir.Node{Type: ir.StringType, String: virtualPath},
		"state":       state,
	})

	// Encode to Tony format
	var buf strings.Builder
	if err := encode.Encode(snapshotFile, &buf); err != nil {
		return fmt.Errorf("failed to encode snapshot file: %w", err)
	}

	// Write to temp file first, then rename atomically
	tmpFile := filePath + ".tmp"
	if err := os.WriteFile(tmpFile, []byte(buf.String()), 0644); err != nil {
		return err
	}

	// Atomic rename
	if err := os.Rename(tmpFile, filePath); err != nil {
		os.Remove(tmpFile) // Clean up on error
		return err
	}

	return nil
}

// ReadSnapshot reads a snapshot file from disk.
func (s *Storage) ReadSnapshot(virtualPath string, commitCount int64) (*Snapshot, error) {
	snapshotDir := s.snapshotToFilesystem(virtualPath)
	filename := snapshotFilename(commitCount)
	filePath := filepath.Join(snapshotDir, filename)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Parse Tony document
	node, err := parse.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse snapshot file: %w", err)
	}

	// Extract fields from the snapshot file structure
	if node.Type != ir.ObjectType {
		return nil, fmt.Errorf("expected object, got %v", node.Type)
	}

	var timestamp, path string
	var state *ir.Node
	var snapshotCommitCount int64

	for i, field := range node.Fields {
		if i >= len(node.Values) {
			break
		}
		value := node.Values[i]

		switch field.String {
		case "commitCount":
			if value.Type == ir.NumberType && value.Int64 != nil {
				snapshotCommitCount = *value.Int64
			}
		case "timestamp":
			if value.Type == ir.StringType {
				timestamp = value.String
			}
		case "path":
			if value.Type == ir.StringType {
				path = value.String
			}
		case "state":
			state = value
		}
	}

	if state == nil {
		return nil, fmt.Errorf("missing state field in snapshot file")
	}

	// Verify commit count matches filename
	if snapshotCommitCount != commitCount {
		return nil, fmt.Errorf("snapshot commit count mismatch: expected %d, got %d", commitCount, snapshotCommitCount)
	}

	return &Snapshot{
		CommitCount: snapshotCommitCount,
		Path:        path,
		Timestamp:   timestamp,
		State:       state,
	}, nil
}

// ListSnapshots lists all snapshot commit counts for a path, ordered by commit count.
func (s *Storage) ListSnapshots(virtualPath string) ([]int64, error) {
	snapshotDir := s.snapshotToFilesystem(virtualPath)

	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var commitCounts []int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".snapshot") {
			continue
		}

		// Parse filename: {commitCount}.snapshot
		commitCountStr := strings.TrimSuffix(name, ".snapshot")
		commitCount, err := strconv.ParseInt(commitCountStr, 10, 64)
		if err != nil {
			s.logger.Warn("skipping invalid snapshot filename", "filename", name, "error", err)
			continue
		}

		commitCounts = append(commitCounts, commitCount)
	}

	// Sort by commit count (monotonic)
	sort.Slice(commitCounts, func(i, j int) bool {
		return commitCounts[i] < commitCounts[j]
	})

	return commitCounts, nil
}

// FindNearestSnapshot finds the nearest snapshot with commit count <= targetCommitCount.
// Returns 0 if no snapshot exists.
func (s *Storage) FindNearestSnapshot(virtualPath string, targetCommitCount int64) (int64, error) {
	snapshots, err := s.ListSnapshots(virtualPath)
	if err != nil {
		return 0, err
	}

	if len(snapshots) == 0 {
		return 0, nil
	}

	// Binary search for the nearest snapshot <= targetCommitCount
	// Since snapshots are sorted, we can find the rightmost one <= target
	left := 0
	right := len(snapshots) - 1
	nearest := int64(0)

	for left <= right {
		mid := (left + right) / 2
		if snapshots[mid] <= targetCommitCount {
			// This snapshot is <= target, it's a candidate
			if snapshots[mid] > nearest {
				nearest = snapshots[mid]
			}
			// Check if there's a better one to the right
			left = mid + 1
		} else {
			// This snapshot is > target, look to the left
			right = mid - 1
		}
	}

	return nearest, nil
}

// DeleteSnapshot deletes a snapshot file.
func (s *Storage) DeleteSnapshot(virtualPath string, commitCount int64) error {
	snapshotDir := s.snapshotToFilesystem(virtualPath)
	filename := snapshotFilename(commitCount)
	filePath := filepath.Join(snapshotDir, filename)
	return os.Remove(filePath)
}
