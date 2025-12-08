package storage

import (
	"os"
	"testing"
)

func TestStreamingIndexer(t *testing.T) {
	// Use a testdata file
	sourcePath := "../../../testdata/sb.tony"
	destPath := "/tmp/test_output.tony"
	defer os.Remove(destPath)

	commit := int64(1)
	tx := int64(1)

	// Create indexer
	indexer, err := NewStreamingIndexer(sourcePath, destPath, commit, tx)
	if err != nil {
		t.Fatalf("failed to create indexer: %v", err)
	}
	defer indexer.Close()

	// Process the document
	if err := indexer.Process(); err != nil {
		t.Fatalf("failed to process document: %v", err)
	}

	// Get the built index
	idx := indexer.GetIndex()

	// Use the index for lookups
	from := int64(1)
	to := int64(1)
	segments := idx.LookupRange("", &from, &to)

	t.Logf("Indexed %d paths", len(segments))
	for _, seg := range segments {
		t.Logf("  Path: %s, Offset: %d", seg.KindedPath, seg.LogPosition)
	}

	// Read a few paths
	if len(segments) > 0 {
		pathsToRead := []string{}
		for i := 0; i < len(segments) && i < 5; i++ {
			if segments[i].KindedPath != "" {
				pathsToRead = append(pathsToRead, segments[i].KindedPath)
			}
		}

		t.Logf("\nReading %d paths from file:", len(pathsToRead))
		nodes, err := indexer.ReadRandomPaths(pathsToRead)
		if err != nil {
			t.Fatalf("failed to read paths: %v", err)
		}

		for path, node := range nodes {
			t.Logf("  Path: %s, Node type: %v", path, node.Type)
		}
	}
}
