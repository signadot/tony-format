package snap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
)

func TestStress_RandomDocumentLookups(t *testing.T) {
	// Set chunk size based on document size expectations
	// For 1-10MB documents, use 64KB chunks to get good coverage
	originalChunkSize := os.Getenv("SNAP_MAX_CHUNK_SIZE")
	defer func() {
		if originalChunkSize == "" {
			os.Unsetenv("SNAP_MAX_CHUNK_SIZE")
		} else {
			os.Setenv("SNAP_MAX_CHUNK_SIZE", originalChunkSize)
		}
	}()
	if originalChunkSize == "" {
		os.Setenv("SNAP_MAX_CHUNK_SIZE", "65536") // 64KB chunks
	}

	config := DefaultRandomDocConfig()
	config.MinSize = 500000           // 500KB - start smaller for debugging
	config.MaxSize = 2000000          // 2MB
	config.Seed = 12345               // Fixed seed for reproducibility
	config.ContainerProbability = 0.2 // Lower probability to reduce nesting complexity

	// Generate random document
	doc, allPaths, err := RandomDocument(config)
	if err != nil {
		t.Fatalf("RandomDocument() error = %v", err)
	}

	t.Logf("Generated random document with %d paths, approximate size: %d-%d bytes",
		len(allPaths), config.MinSize, config.MaxSize)

	// Convert document to events
	events, err := stream.NodeToEvents(doc)
	if err != nil {
		t.Fatalf("NodeToEvents() error = %v", err)
	}

	// Build snapshot
	p, w := newBytesWriteSeeker()
	defer os.RemoveAll(filepath.Dir(p))
	defer w.Close()

	index := &Index{Entries: []IndexEntry{}}
	builder, err := NewBuilder(w, index, nil)
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}

	// Feed events to builder
	eventCount := 0
	for _, ev := range events {
		if err := builder.WriteEvent(&ev); err != nil {
			t.Fatalf("WriteEvent() error = %v", err)
		}
		eventCount++
	}

	if err := builder.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	t.Logf("Built snapshot: %d events, %d index entries", eventCount, len(index.Entries))

	// Open snapshot
	r, err := os.Open(p)
	if err != nil {
		t.Fatalf("Open file error = %v", err)
	}
	defer r.Close()

	snapshot, err := Open(r)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer snapshot.Close()

	t.Logf("Opened snapshot: EventSize=%d bytes, Index entries=%d", snapshot.EventSize, len(snapshot.Index.Entries))

	// Test random lookups
	numQueries := 100
	if len(allPaths) < numQueries {
		numQueries = len(allPaths)
	}

	// Use a deterministic RNG for selecting paths to query
	rng := &testingRand{seed: 54321}
	successCount := 0
	failureCount := 0

	for i := 0; i < numQueries; i++ {
		// Randomly select a path to query
		pathIdx := rng.Intn(len(allPaths))
		requestedPath := allPaths[pathIdx]

		// Read from snapshot
		snapshotNode, err := snapshot.ReadPath(requestedPath)
		if err != nil {
			t.Errorf("ReadPath(%q) error = %v", requestedPath, err)
			failureCount++
			continue
		}

		// Read from in-memory document
		expectedNode, err := doc.GetKPath(requestedPath)
		if err != nil {
			// Path doesn't exist in document - snapshot should also return nil
			if snapshotNode != nil {
				t.Errorf("ReadPath(%q) returned node but path doesn't exist in document", requestedPath)
				failureCount++
			} else {
				successCount++
			}
			continue
		}

		// Compare nodes
		if snapshotNode == nil {
			t.Errorf("ReadPath(%q) returned nil but path exists in document", requestedPath)
			failureCount++
			continue
		}

		if !nodesEqual(snapshotNode, expectedNode) {
			t.Errorf("ReadPath(%q) returned different node than expected", requestedPath)
			failureCount++
			continue
		}

		successCount++
	}

	t.Logf("Query results: %d successful, %d failed out of %d queries", successCount, failureCount, numQueries)

	if failureCount > 0 {
		t.Fatalf("Failed %d out of %d queries", failureCount, numQueries)
	}
}

// nodesEqual compares two nodes for equality (simplified comparison)
func nodesEqual(a, b *ir.Node) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Type != b.Type {
		return false
	}

	switch a.Type {
	case ir.StringType:
		return a.String == b.String

	case ir.NumberType:
		if a.Int64 != nil && b.Int64 != nil {
			return *a.Int64 == *b.Int64
		}
		if a.Float64 != nil && b.Float64 != nil {
			return *a.Float64 == *b.Float64
		}
		return a.Int64 == nil && b.Int64 == nil && a.Float64 == nil && b.Float64 == nil

	case ir.BoolType:
		return a.Bool == b.Bool

	case ir.NullType:
		return true

	case ir.ObjectType:
		if len(a.Fields) != len(b.Fields) || len(a.Values) != len(b.Values) {
			return false
		}
		// Build maps for easier comparison
		aMap := make(map[string]*ir.Node)
		bMap := make(map[string]*ir.Node)
		for i := range a.Fields {
			if a.Fields[i] != nil && a.Fields[i].Type == ir.StringType && i < len(a.Values) {
				aMap[a.Fields[i].String] = a.Values[i]
			}
		}
		for i := range b.Fields {
			if b.Fields[i] != nil && b.Fields[i].Type == ir.StringType && i < len(b.Values) {
				bMap[b.Fields[i].String] = b.Values[i]
			}
		}
		if len(aMap) != len(bMap) {
			return false
		}
		for k, av := range aMap {
			bv, ok := bMap[k]
			if !ok {
				return false
			}
			if !nodesEqual(av, bv) {
				return false
			}
		}
		return true

	case ir.ArrayType:
		if len(a.Values) != len(b.Values) {
			return false
		}
		for i := range a.Values {
			if !nodesEqual(a.Values[i], b.Values[i]) {
				return false
			}
		}
		return true

	default:
		return false
	}
}

// Simple deterministic RNG for testing
type testingRand struct {
	seed  int64
	state int64
}

func (r *testingRand) Intn(n int) int {
	if r.state == 0 {
		r.state = r.seed
	}
	r.state = (r.state*1103515245 + 12345) & 0x7fffffff
	return int(r.state % int64(n))
}
