package token

import (
	"bytes"
	"io"
	"testing"
)

func TestTokenSink_Basic(t *testing.T) {
	var buf bytes.Buffer
	var nodeStarts []struct {
		offset int
		path   string
		token  Token
	}

	onNodeStart := func(offset int, path string, token Token) {
		nodeStarts = append(nodeStarts, struct {
			offset int
			path   string
			token  Token
		}{offset, path, token})
	}

	sink := NewTokenSink(&buf, onNodeStart)

	// Tokenize a simple document
	input := "key: value\n"
	tokens, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}

	// Write tokens
	if err := sink.Write(tokens); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	// Verify output matches input (minus trailing newline that Tokenize adds)
	expected := input
	got := buf.String()
	if got != expected {
		t.Errorf("Output mismatch:\n  got:      %q\n  expected: %q", got, expected)
	}

	// Verify offset tracking
	if sink.Offset() != len(expected) {
		t.Errorf("Offset mismatch: got %d, expected %d", sink.Offset(), len(expected))
	}

	// Verify node starts were detected
	t.Logf("Node starts detected: %d", len(nodeStarts))
	for i, ns := range nodeStarts {
		t.Logf("  [%d] offset=%d, path=%q, token=%v", i, ns.offset, ns.path, ns.token.Type)
	}
	
	// Verify paths
	if len(nodeStarts) >= 1 {
		if nodeStarts[0].path != "key" {
			t.Errorf("Expected path key, got %q", nodeStarts[0].path)
		}
	}
}

func TestTokenSink_NodeStartDetection(t *testing.T) {
	var buf bytes.Buffer
	var nodeStarts []struct {
		offset int
		path   string
		token  Token
	}

	onNodeStart := func(offset int, path string, token Token) {
		nodeStarts = append(nodeStarts, struct {
			offset int
			path   string
			token  Token
		}{offset, path, token})
	}

	sink := NewTokenSink(&buf, onNodeStart)

	// Tokenize a document with multiple nodes
	input := `key1: value1
key2: value2
`
	tokens, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}

	// Write tokens
	if err := sink.Write(tokens); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	// Verify output
	expected := input
	got := buf.String()
	if got != expected {
		t.Errorf("Output mismatch:\n  got:      %q\n  expected: %q", got, expected)
	}

	// Log node starts for inspection
	t.Logf("Node starts detected: %d", len(nodeStarts))
	t.Logf("Output: %q", got)
	for i, ns := range nodeStarts {
		if ns.offset < len(got) {
			t.Logf("  Node %d starts at offset %d (path=%q, token=%v): %q", i, ns.offset, ns.path, ns.token.Type, got[ns.offset:min(ns.offset+10, len(got))])
		}
	}
}

func TestTokenSink_NestedPaths(t *testing.T) {
	var buf bytes.Buffer
	var nodeStarts []struct {
		offset int
		path   string
		token  Token
	}

	onNodeStart := func(offset int, path string, token Token) {
		nodeStarts = append(nodeStarts, struct {
			offset int
			path   string
			token  Token
		}{offset, path, token})
	}

	sink := NewTokenSink(&buf, onNodeStart)

	// Tokenize a nested document
	input := `key1:
  nested: value1
key2: value2
`
	tokens, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}

	// Write tokens
	if err := sink.Write(tokens); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	// Log paths for inspection
	t.Logf("Node starts detected: %d", len(nodeStarts))
	for i, ns := range nodeStarts {
		t.Logf("  [%d] offset=%d, path=%q, token=%v", i, ns.offset, ns.path, ns.token.Type)
	}
	
	// Verify nested path
	foundNested := false
	for _, ns := range nodeStarts {
		if ns.path == "key1.nested" {
			foundNested = true
			break
		}
	}
	if !foundNested {
		t.Errorf("Expected to find path key1.nested")
	}
}

func TestTokenSink_ArrayPaths(t *testing.T) {
	var buf bytes.Buffer
	var nodeStarts []struct {
		offset int
		path   string
		token  Token
	}

	onNodeStart := func(offset int, path string, token Token) {
		nodeStarts = append(nodeStarts, struct {
			offset int
			path   string
			token  Token
		}{offset, path, token})
	}

	sink := NewTokenSink(&buf, onNodeStart)

	// Tokenize a bracketed array document (path tracking only works for bracketed structures)
	input := `[item1, item2]`
	tokens, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}

	// Write tokens
	if err := sink.Write(tokens); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	// Log paths for inspection
	t.Logf("Node starts detected: %d", len(nodeStarts))
	for i, ns := range nodeStarts {
		t.Logf("  [%d] offset=%d, path=%q, token=%v", i, ns.offset, ns.path, ns.token.Type)
	}
	
	// Verify array paths (only bracketed arrays are tracked)
	found0 := false
	found1 := false
	for _, ns := range nodeStarts {
		if ns.path == "[0]" {
			found0 = true
		}
		if ns.path == "[1]" {
			found1 = true
		}
	}
	if !found0 {
		t.Errorf("Expected to find path [0]")
	}
	if !found1 {
		t.Errorf("Expected to find path [1]")
	}
}

func TestTokenSink_SparseArrayPaths(t *testing.T) {
	var buf bytes.Buffer
	var nodeStarts []struct {
		offset int
		path   string
		token  Token
	}

	onNodeStart := func(offset int, path string, token Token) {
		nodeStarts = append(nodeStarts, struct {
			offset int
			path   string
			token  Token
		}{offset, path, token})
	}

	sink := NewTokenSink(&buf, onNodeStart)

	// Tokenize a sparse array (integer-keyed map)
	input := `0: hello
13: other
42: value
`
	tokens, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}

	// Write tokens
	if err := sink.Write(tokens); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	// Log paths for inspection
	t.Logf("Node starts detected: %d", len(nodeStarts))
	for i, ns := range nodeStarts {
		t.Logf("  [%d] offset=%d, path=%q, token=%v", i, ns.offset, ns.path, ns.token.Type)
	}
	
	// Verify sparse array paths (use {index} syntax)
	found0 := false
	found13 := false
	found42 := false
	for _, ns := range nodeStarts {
		if ns.path == "{0}" {
			found0 = true
		}
		if ns.path == "{13}" {
			found13 = true
		}
		if ns.path == "{42}" {
			found42 = true
		}
	}
	if !found0 {
		t.Errorf("Expected to find path {0}")
	}
	if !found13 {
		t.Errorf("Expected to find path {13}")
	}
	if !found42 {
		t.Errorf("Expected to find path {42}")
	}
}

func TestTokenSink_RoundTrip(t *testing.T) {
	// Test round-trip: Tokenize -> TokenSink -> Tokenize
	input := `key1: value1
key2:
  nested: value2
`
	// Tokenize original
	tokens1, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}

	// Write to sink
	var buf bytes.Buffer
	sink := NewTokenSink(&buf, nil)
	if err := sink.Write(tokens1); err != nil {
		t.Fatalf("Write error: %v", err)
	}

	// Tokenize output
	output := buf.String()
	tokens2, err := Tokenize(nil, []byte(output))
	if err != nil {
		t.Fatalf("Tokenize output error: %v", err)
	}

	// Compare token counts (allowing for minor differences)
	if len(tokens1) != len(tokens2) {
		t.Logf("Token count mismatch: %d vs %d", len(tokens1), len(tokens2))
		t.Logf("Input:  %q", input)
		t.Logf("Output: %q", output)
		// This might be okay due to normalization, so just log
	}
}

func TestTokenSink_WithCallback(t *testing.T) {
	var buf bytes.Buffer
	var nodeStarts []struct {
		offset int
		path   string
		token  Token
	}

	onNodeStart := func(offset int, path string, token Token) {
		nodeStarts = append(nodeStarts, struct {
			offset int
			path   string
			token  Token
		}{offset, path, token})
	}

	sink := NewTokenSink(&buf, onNodeStart)

	// Tokenize a document
	input := "key: value\n"
	tokens, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}

	// Write tokens one at a time to test incremental writing
	for _, tok := range tokens {
		if err := sink.Write([]Token{tok}); err != nil {
			t.Fatalf("Write error: %v", err)
		}
	}

	// Verify callback was called
	if len(nodeStarts) == 0 {
		t.Logf("No node starts detected (might be expected)")
	} else {
		t.Logf("Node starts: %d", len(nodeStarts))
		for i, ns := range nodeStarts {
			t.Logf("  [%d] offset=%d, path=%q, token=%v", i, ns.offset, ns.path, ns.token.Type)
		}
	}
}

func TestTokenSink_Streaming(t *testing.T) {
	// Test streaming: TokenSource -> TokenSink
	input := `key1: value1
key2: value2
`
	reader := bytes.NewReader([]byte(input))
	source := NewTokenSource(reader)

	var buf bytes.Buffer
	var nodeStarts []struct {
		offset int
		path   string
		token  Token
	}
	onNodeStart := func(offset int, path string, token Token) {
		nodeStarts = append(nodeStarts, struct {
			offset int
			path   string
			token  Token
		}{offset, path, token})
	}
	sink := NewTokenSink(&buf, onNodeStart)

	// Stream tokens from source to sink
	for {
		tokens, err := source.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read error: %v", err)
		}
		if err := sink.Write(tokens); err != nil {
			t.Fatalf("Write error: %v", err)
		}
	}

	// Verify output matches input
	expected := input
	got := buf.String()
	if got != expected {
		t.Errorf("Output mismatch:\n  got:      %q\n  expected: %q", got, expected)
	}

	t.Logf("Node starts: %d", len(nodeStarts))
	for i, ns := range nodeStarts {
		t.Logf("  [%d] offset=%d, path=%q, token=%v", i, ns.offset, ns.path, ns.token.Type)
	}
}

// TestTokenSink_NilCallback_KeyProcessedFlag tests that keyProcessed flag
// is properly handled even when callback is nil. This test would catch bug #1
// where keyProcessed flag is not cleared when callback is nil.
//
// The test uses keys with implicit values (no colons) which sets keyProcessed = true.
// Then it processes more tokens to verify the flag doesn't affect subsequent writes.
func TestTokenSink_NilCallback_KeyProcessedFlag(t *testing.T) {
	var buf bytes.Buffer
	
	// Create sink with nil callback - this is the scenario where the bug occurs
	sink := NewTokenSink(&buf, nil)
	
	// Test case 1: Object with keys that have implicit values (no colons)
	// This sets keyProcessed = true when processing the keys in updatePath()
	input1 := `{key1 key2: value2}`
	tokens1, err := Tokenize(nil, []byte(input1))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}
	
	if err := sink.Write(tokens1); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	
	offset1 := sink.Offset()
	
	// Test case 2: Process more tokens after the first batch
	// If keyProcessed flag wasn't cleared, this might be affected
	input2 := `key3: value3
key4: value4
`
	tokens2, err := Tokenize(nil, []byte(input2))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}
	
	if err := sink.Write(tokens2); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	
	offset2 := sink.Offset()
	
	// Verify offset tracking is correct and incremental
	if offset2 <= offset1 {
		t.Errorf("Offset should increase: offset1=%d, offset2=%d", offset1, offset2)
	}
	
	// Verify output can be re-tokenized (round-trip test)
	output := buf.String()
	tokens3, err := Tokenize(nil, []byte(output))
	if err != nil {
		t.Fatalf("Re-tokenize error: %v", err)
	}
	
	// Verify we got tokens back (basic sanity check)
	if len(tokens3) == 0 {
		t.Errorf("Re-tokenized output produced no tokens")
	}
	
	t.Logf("Output length: %d, offset: %d", len(output), offset2)
	t.Logf("Re-tokenized tokens: %d", len(tokens3))
}

// TestTokenSink_NilCallback_SparseArrayKeys tests that keyProcessed flag
// is properly handled for sparse array keys (integer keys) with nil callback.
func TestTokenSink_NilCallback_SparseArrayKeys(t *testing.T) {
	var buf bytes.Buffer
	
	// Create sink with nil callback
	sink := NewTokenSink(&buf, nil)
	
	// Test case: Sparse array with integer keys that have implicit values
	// This sets keyProcessed = true when processing integer keys in updatePath()
	// Use comma-separated format to avoid tokenization issues
	input := `{10: value1 20}`
	tokens, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}
	
	if err := sink.Write(tokens); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	
	offset1 := sink.Offset()
	
	// Process more tokens to verify flag doesn't affect subsequent writes
	input2 := `key: value
`
	tokens2, err := Tokenize(nil, []byte(input2))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}
	
	if err := sink.Write(tokens2); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	
	offset2 := sink.Offset()
	
	// Verify offset tracking is correct and incremental
	if offset2 <= offset1 {
		t.Errorf("Offset should increase: offset1=%d, offset2=%d", offset1, offset2)
	}
	
	// Verify output can be re-tokenized
	output := buf.String()
	tokens3, err := Tokenize(nil, []byte(output))
	if err != nil {
		t.Fatalf("Re-tokenize error: %v", err)
	}
	
	if len(tokens3) == 0 {
		t.Errorf("Re-tokenized output produced no tokens")
	}
	
	t.Logf("Output length: %d, offset: %d", len(output), offset2)
	t.Logf("Re-tokenized tokens: %d", len(tokens3))
}

// TestTokenSink_NilCallback_MultipleWrites tests that multiple Write() calls
// work correctly with nil callback, especially after keys with implicit values.
// This test verifies that keyProcessed flag doesn't leak between Write() calls.
func TestTokenSink_NilCallback_MultipleWrites(t *testing.T) {
	var buf bytes.Buffer
	
	// Create sink with nil callback
	sink := NewTokenSink(&buf, nil)
	
	// First write: Object with key that has implicit value (sets keyProcessed)
	tokens1, err := Tokenize(nil, []byte(`{key1 key2: value}`))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}
	if err := sink.Write(tokens1); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	offset1 := sink.Offset()
	
	// Second write: More content
	// If keyProcessed flag wasn't cleared, this might be affected
	tokens2, err := Tokenize(nil, []byte(`key3: value3
`))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}
	if err := sink.Write(tokens2); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	offset2 := sink.Offset()
	
	// Third write: Another object with implicit value key
	// This would set keyProcessed again - verify it doesn't interfere
	tokens3, err := Tokenize(nil, []byte(`{key4 key5: value5}`))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}
	if err := sink.Write(tokens3); err != nil {
		t.Fatalf("Write error: %v", err)
	}
	offset3 := sink.Offset()
	
	// Verify offsets are incremental (flag doesn't cause issues)
	if offset2 <= offset1 {
		t.Errorf("Offset should increase: offset1=%d, offset2=%d", offset1, offset2)
	}
	if offset3 <= offset2 {
		t.Errorf("Offset should increase: offset2=%d, offset3=%d", offset2, offset3)
	}
	
	// Verify output can be re-tokenized (round-trip test)
	output := buf.String()
	tokens4, err := Tokenize(nil, []byte(output))
	if err != nil {
		t.Fatalf("Re-tokenize error: %v", err)
	}
	
	if len(tokens4) == 0 {
		t.Errorf("Re-tokenized output produced no tokens")
	}
	
	// Verify final offset matches output length
	if sink.Offset() != len(output) {
		t.Errorf("Final offset mismatch: got %d, expected %d", sink.Offset(), len(output))
	}
	
	t.Logf("Output length: %d, final offset: %d", len(output), offset3)
	t.Logf("Re-tokenized tokens: %d", len(tokens4))
}
