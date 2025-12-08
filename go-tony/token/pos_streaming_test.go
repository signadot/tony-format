package token

import (
	"testing"
)

func TestPosDoc_StreamingMode(t *testing.T) {
	// Test that nl() works in streaming mode (empty document)
	posDoc := &PosDoc{
		d: nil, // Empty document (streaming mode)
		n: []int{},
	}

	// Should not panic when calling nl() with empty document
	posDoc.nl(0)
	posDoc.nl(10)
	posDoc.nl(25)

	if len(posDoc.n) != 3 {
		t.Errorf("Expected 3 newline positions, got %d", len(posDoc.n))
	}
	if posDoc.n[0] != 0 || posDoc.n[1] != 10 || posDoc.n[2] != 25 {
		t.Errorf("Expected newline positions [0, 10, 25], got %v", posDoc.n)
	}
}

func TestPosDoc_NonStreamingMode(t *testing.T) {
	// Test that nl() still works in non-streaming mode (with document)
	doc := []byte("line1\nline2\nline3\n")
	posDoc := &PosDoc{
		d: doc,
		n: []int{},
	}

	// Should work normally with document
	posDoc.nl(5)  // First newline
	posDoc.nl(11) // Second newline
	posDoc.nl(17) // Third newline

	if len(posDoc.n) != 3 {
		t.Errorf("Expected 3 newline positions, got %d", len(posDoc.n))
	}

	// Test validation still works - should panic if position doesn't have newline
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic when nl() called with non-newline position")
			}
		}()
		posDoc.nl(0) // Position 0 is 'l', not '\n'
	}()
}

func TestPosDoc_PosWithContext(t *testing.T) {
	posDoc := &PosDoc{
		d: nil, // Streaming mode
		n: []int{},
	}

	// Create a buffer with some content
	buffer := []byte("hello world this is a test")
	bufferLen := len(buffer)
	bufferStartOffset := 100 // Absolute offset where buffer starts

	// Test position in middle of buffer (offset 110 = 'w' in "world")
	// Relative position in buffer: 110 - 100 = 10
	// Context should be 10 bytes before/after: buffer[max(0, 10-10):min(len(buffer), 10+10)]
	// = buffer[0:20] = "hello world this is "
	pos := posDoc.PosWithContext(110, buffer, bufferStartOffset)
	if pos.I != 110 {
		t.Errorf("Expected offset 110, got %d", pos.I)
	}
	if len(pos.Context) == 0 {
		t.Error("Expected context to be extracted")
	}
	// Context should include bytes around position 110
	// Absolute offset 110 = relative offset 10 in buffer
	// Context range: max(0, 110-10) to min(110+10, 100+len(buffer))
	// = max(0, 100) to min(120, 125) = 100 to 120
	// Relative in buffer: 0 to 20
	expectedContext := buffer[0:20] // "hello world this is "
	if string(pos.Context) != string(expectedContext) {
		t.Errorf("Expected context %q, got %q", string(expectedContext), string(pos.Context))
	}

	// Test position at start of buffer
	pos2 := posDoc.PosWithContext(100, buffer, bufferStartOffset)
	if pos2.I != 100 {
		t.Errorf("Expected offset 100, got %d", pos2.I)
	}
	if len(pos2.Context) == 0 {
		t.Error("Expected context to be extracted")
	}
	// Context should start from beginning of buffer
	// Absolute offset 100 = relative offset 0
	// Context range: max(0, 100-10) to min(110, 125) = 90 to 110
	// But buffer starts at 100, so relative: max(0, 90-100) to min(10, 25) = 0 to 10
	expectedContext2 := buffer[0:10] // "hello worl"
	if string(pos2.Context) != string(expectedContext2) {
		t.Errorf("Expected context %q, got %q", string(expectedContext2), string(pos2.Context))
	}

	// Test position near end of buffer
	pos3 := posDoc.PosWithContext(120, buffer, bufferStartOffset)
	if pos3.I != 120 {
		t.Errorf("Expected offset 120, got %d", pos3.I)
	}
	// Context should be truncated at buffer end
	// Absolute offset 120 = relative offset 20 in buffer
	// Context range: max(0, 120-10) to min(130, bufferStartOffset+bufferLen) = 110 to min(130, 100+bufferLen)
	// Relative in buffer: max(0, 110-100) to min(bufferLen, 130-100) = 10 to min(bufferLen, 30)
	relStart := 10
	relEnd := min(bufferLen, 30)
	expectedContext3 := buffer[relStart:relEnd]
	if string(pos3.Context) != string(expectedContext3) {
		t.Errorf("Expected context %q, got %q", string(expectedContext3), string(pos3.Context))
	}

	// Test position beyond buffer (should still work, context might be empty/partial)
	pos4 := posDoc.PosWithContext(200, buffer, bufferStartOffset)
	if pos4.I != 200 {
		t.Errorf("Expected offset 200, got %d", pos4.I)
	}
	// Context might be empty or partial if position is far beyond buffer
}

func TestPos_String_StreamingMode(t *testing.T) {
	posDoc := &PosDoc{
		d: nil, // Streaming mode - no document
		n: []int{},
	}

	// Create Pos with context
	context := []byte("hello world")
	pos := &Pos{
		I:       5,
		D:       posDoc,
		Context: context,
	}

	str := pos.String()
	if str == "" {
		t.Error("Expected non-empty string representation")
	}
	// Should use context, not document
	if len(posDoc.d) > 0 {
		t.Error("PosDoc should not have document in streaming mode")
	}
	// String should contain the context
	if len(str) == 0 || str == "`...?...`" {
		t.Errorf("Expected string to contain context, got %q", str)
	}
}

func TestPos_String_NonStreamingMode(t *testing.T) {
	// Test that Pos.String() still works in non-streaming mode
	doc := []byte("hello world test")
	posDoc := &PosDoc{
		d: doc,
		n: []int{},
	}

	pos := &Pos{
		I:       6, // Position of 'w' in "world"
		D:       posDoc,
		Context: nil, // No context (non-streaming mode)
	}

	str := pos.String()
	if str == "" {
		t.Error("Expected non-empty string representation")
	}
	// Should use document, not context
	if len(pos.Context) > 0 {
		t.Error("Pos should not have context in non-streaming mode")
	}
	// String should contain document snippet
	if len(str) == 0 {
		t.Error("Expected string to contain document snippet")
	}
}

func TestPos_String_Fallback(t *testing.T) {
	// Test fallback when neither context nor document available
	posDoc := &PosDoc{
		d: nil, // No document
		n: []int{},
	}

	pos := &Pos{
		I:       5,
		D:       posDoc,
		Context: nil, // No context either
	}

	str := pos.String()
	// Should still produce a string, even if it shows "?"
	if str == "" {
		t.Error("Expected non-empty string representation")
	}
}

func TestPosDoc_LineCol_StreamingMode(t *testing.T) {
	posDoc := &PosDoc{
		d: nil, // Streaming mode
		n: []int{},
	}

	// Add some newline positions
	posDoc.nl(10)
	posDoc.nl(20)
	posDoc.nl(30)

	// Test LineCol calculation
	line, col := posDoc.LineCol(5)
	if line != 0 || col != 5 {
		t.Errorf("Expected (0, 5), got (%d, %d)", line, col)
	}

	line, col = posDoc.LineCol(15)
	if line != 1 || col != 4 {
		t.Errorf("Expected (1, 4), got (%d, %d)", line, col)
	}

	line, col = posDoc.LineCol(25)
	if line != 2 || col != 4 {
		t.Errorf("Expected (2, 4), got (%d, %d)", line, col)
	}

	line, col = posDoc.LineCol(35)
	if line != 3 || col != 4 {
		t.Errorf("Expected (3, 4), got (%d, %d)", line, col)
	}
}

func TestPosDoc_nl_DuplicatePrevention(t *testing.T) {
	// Test that nl() prevents duplicate newline positions
	posDoc := &PosDoc{
		d: nil,
		n: []int{},
	}

	posDoc.nl(10)
	posDoc.nl(10) // Duplicate - should be ignored
	posDoc.nl(20)

	if len(posDoc.n) != 2 {
		t.Errorf("Expected 2 newline positions (duplicate ignored), got %d", len(posDoc.n))
	}
	if posDoc.n[0] != 10 || posDoc.n[1] != 20 {
		t.Errorf("Expected [10, 20], got %v", posDoc.n)
	}
}
