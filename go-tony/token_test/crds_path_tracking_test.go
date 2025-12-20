package token_test

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/token"
)

// TestCRDs_PathTracking tests path tracking on the large crds.yaml file.
// It converts the block-style YAML to bracketed format first, then verifies
// that path tracking works correctly throughout the document.
func TestCRDs_PathTracking(t *testing.T) {
	// Read the crds.yaml file
	data, err := os.ReadFile("../testdata/crds.yaml")
	if err != nil {
		t.Fatalf("Failed to read crds.yaml: %v", err)
	}

	// Parse as YAML
	node, err := parse.Parse(data, parse.ParseYAML())
	if err != nil {
		t.Fatalf("Failed to parse YAML: %v", err)
	}

	// Convert to bracketed Tony format
	var bracketedBuf bytes.Buffer
	if err := encode.Encode(node, &bracketedBuf, encode.EncodeBrackets(true)); err != nil {
		t.Fatalf("Failed to encode to bracketed format: %v", err)
	}

	bracketedData := bracketedBuf.Bytes()
	t.Logf("Converted crds.yaml to bracketed format: %d bytes", len(bracketedData))

	// Now test path tracking using TokenSource
	source := token.NewTokenSource(bytes.NewReader(bracketedData))
	
	var allPaths []string
	var pathCounts = make(map[string]int)
	var maxDepth int

	for {
		tokens, err := source.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("TokenSource.Read error: %v", err)
		}

		for range tokens {
			path := source.CurrentPath()
			depth := source.Depth()

			// Track unique paths
			if path != "" && path != "$" {
				allPaths = append(allPaths, path)
				pathCounts[path]++
			}

			// Track max depth
			if depth > maxDepth {
				maxDepth = depth
			}
		}
	}

	// Verify we tracked paths successfully
	if len(allPaths) == 0 {
		t.Error("No paths were tracked")
	}

	t.Logf("Path tracking summary:")
	t.Logf("  Total path updates: %d", len(allPaths))
	t.Logf("  Unique paths: %d", len(pathCounts))
	t.Logf("  Max depth: %d", maxDepth)

	// Verify some expected paths exist (based on YAML structure)
	// The crds.yaml file has items array, which when converted to bracketed format
	// uses kinded path format: "items", "items[0].spec", etc. (no $ prefix)
	hasItems := false
	hasMetadata := false
	hasSpec := false

	for path := range pathCounts {
		// Check for items (kinded path: "items" or "items[...")
		if len(path) >= 5 && path[:5] == "items" {
			hasItems = true
		}
		// Check for metadata anywhere in path
		if len(path) >= 8 && path[len(path)-8:] == "metadata" {
			hasMetadata = true
		}
		// Check for spec anywhere in path
		if len(path) >= 4 && path[len(path)-4:] == "spec" {
			hasSpec = true
		}
	}

	if !hasItems {
		t.Error("Expected to find paths starting with items")
	}
	if !hasMetadata {
		t.Error("Expected to find paths ending with metadata")
	}
	if !hasSpec {
		t.Error("Expected to find paths ending with spec")
	}

	// Verify depth tracking
	if maxDepth < 5 {
		t.Errorf("Expected max depth >= 5, got %d (crds.yaml is deeply nested)", maxDepth)
	}
}

// TestCRDs_TokenSink_PathTracking tests path tracking using TokenSink
// on the converted bracketed format.
func TestCRDs_TokenSink_PathTracking(t *testing.T) {
	// Read the crds.yaml file
	data, err := os.ReadFile("../testdata/crds.yaml")
	if err != nil {
		t.Fatalf("Failed to read crds.yaml: %v", err)
	}

	// Parse as YAML
	node, err := parse.Parse(data, parse.ParseYAML())
	if err != nil {
		t.Fatalf("Failed to parse YAML: %v", err)
	}

	// Convert to bracketed Tony format
	var bracketedBuf bytes.Buffer
	if err := encode.Encode(node, &bracketedBuf, encode.EncodeBrackets(true)); err != nil {
		t.Fatalf("Failed to encode to bracketed format: %v", err)
	}

	bracketedData := bracketedBuf.Bytes()
	t.Logf("Converted crds.yaml to bracketed format: %d bytes", len(bracketedData))

	// Tokenize the bracketed data
	tokens, err := token.Tokenize(nil, bracketedData)
	if err != nil {
		t.Fatalf("Failed to tokenize bracketed data: %v", err)
	}

	// Test path tracking using TokenSink
	var sinkBuf bytes.Buffer
	var nodeStarts []struct {
		offset int
		path   string
		tok    token.Token
	}

	onNodeStart := func(offset int, path string, tok token.Token) {
		nodeStarts = append(nodeStarts, struct {
			offset int
			path   string
			tok    token.Token
		}{offset, path, tok})
	}

	sink := token.NewTokenSink(&sinkBuf, onNodeStart)

	if err := sink.Write(tokens); err != nil {
		t.Fatalf("TokenSink.Write error: %v", err)
	}

	// Collect unique paths
	pathCounts := make(map[string]int)
	var maxDepth int

	for _, ns := range nodeStarts {
		if ns.path != "" && ns.path != "$" {
			pathCounts[ns.path]++
		}
		// Track depth from path (estimate from path depth)
		depth := estimateDepthFromPath(ns.path)
		if depth > maxDepth {
			maxDepth = depth
		}
	}

	t.Logf("TokenSink path tracking summary:")
	t.Logf("  Node starts detected: %d", len(nodeStarts))
	t.Logf("  Unique paths: %d", len(pathCounts))
	t.Logf("  Max depth: %d", maxDepth)

	// Verify we tracked paths successfully
	if len(pathCounts) == 0 {
		t.Error("No paths were tracked by TokenSink")
	}

	// Verify some expected paths exist (kinded path format, no $ prefix)
	hasItems := false
	hasMetadata := false
	hasSpec := false

	// Log some sample paths for debugging
	samplePaths := make([]string, 0, 10)
	for path := range pathCounts {
		if len(samplePaths) < 10 {
			samplePaths = append(samplePaths, path)
		}
		// Check for items (kinded path: "items" or "items[...")
		if len(path) >= 5 && path[:5] == "items" {
			hasItems = true
		}
		// Check for metadata anywhere in path
		if len(path) >= 8 && path[len(path)-8:] == "metadata" {
			hasMetadata = true
		}
		// Check for spec anywhere in path
		if len(path) >= 4 && path[len(path)-4:] == "spec" {
			hasSpec = true
		}
	}
	t.Logf("Sample paths: %v", samplePaths)

	if !hasItems {
		t.Error("Expected to find paths starting with items")
	}
	if !hasMetadata {
		t.Error("Expected to find paths ending with metadata")
	}
	if !hasSpec {
		t.Error("Expected to find paths ending with spec")
	}

	// Verify depth tracking
	if maxDepth < 5 {
		t.Errorf("Expected max depth >= 5, got %d (crds.yaml is deeply nested)", maxDepth)
	}
}


// estimateDepthFromPath estimates nesting depth from a JSONPath string
func estimateDepthFromPath(path string) int {
	if path == "" || path == "$" {
		return 0
	}
	depth := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' || path[i] == '[' {
			depth++
		}
	}
	return depth
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
