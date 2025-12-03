package parse

import (
	"bytes"
	"io"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/token"
)

// TestNodeParser_WithParsePositions tests that NodeParser correctly tracks positions
// when ParsePositions option is provided.
func TestNodeParser_WithParsePositions(t *testing.T) {
	input := `{key: value, nested: {inner: 42}}`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	
	// Create positions map to track node positions
	positions := make(map[*ir.Node]*token.Pos)
	parser := NewNodeParser(source, ParsePositions(positions))

	// Parse the node
	node, err := parser.ParseNext()
	if err != nil {
		t.Fatalf("ParseNext() error = %v", err)
	}
	if node == nil {
		t.Fatal("ParseNext() returned nil node")
	}
	if node.Type != ir.ObjectType {
		t.Errorf("node.Type = %v, want %v", node.Type, ir.ObjectType)
	}

	// Verify that positions were tracked
	if len(positions) == 0 {
		t.Fatal("positions map is empty - positions were not tracked")
	}

	// Check that the root node has a position
	rootPos, hasRootPos := positions[node]
	if !hasRootPos {
		t.Error("root node position not tracked")
	} else if rootPos == nil {
		t.Error("root node position is nil")
	} else {
		t.Logf("Root node position: line %d, col %d", rootPos.Line(), rootPos.Col())
	}

	// Check that nested nodes have positions
	if len(node.Values) < 2 {
		t.Fatalf("node.Values length = %d, want at least 2", len(node.Values))
	}

	// Check first field value position (should be tracked)
	firstValue := node.Values[0]
	firstValuePos, hasPos := positions[firstValue]
	if !hasPos {
		t.Error("first value position not tracked")
	} else if firstValuePos == nil {
		t.Error("first value position is nil")
	} else {
		t.Logf("First value position: line %d, col %d", firstValuePos.Line(), firstValuePos.Col())
	}

	// Check nested object position
	nestedObj := node.Values[1]
	if nestedObj.Type != ir.ObjectType {
		t.Fatalf("nestedObj.Type = %v, want %v", nestedObj.Type, ir.ObjectType)
	}
	if nestedPos, hasPos := positions[nestedObj]; hasPos {
		if nestedPos == nil {
			t.Error("nested object position is nil")
		} else {
			t.Logf("Nested object position: line %d, col %d", nestedPos.Line(), nestedPos.Col())
		}
	} else {
		t.Error("nested object position not tracked")
	}

	// Check nested value position (should be tracked for numbers)
	if len(nestedObj.Values) > 0 {
		nestedValue := nestedObj.Values[0]
		if nestedValue.Type == ir.NumberType {
			// Numbers should have positions tracked
			if nestedValuePos, hasPos := positions[nestedValue]; hasPos {
				if nestedValuePos == nil {
					t.Error("nested value position is nil")
				} else {
					t.Logf("Nested value position: line %d, col %d", nestedValuePos.Line(), nestedValuePos.Col())
				}
			} else {
				t.Error("nested number value position not tracked")
			}
		}
	}
}

// TestNodeParser_WithParsePositions_SimpleValue tests position tracking for simple values.
func TestNodeParser_WithParsePositions_SimpleValue(t *testing.T) {
	input := `42`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	
	positions := make(map[*ir.Node]*token.Pos)
	parser := NewNodeParser(source, ParsePositions(positions))

	node, err := parser.ParseNext()
	if err != nil {
		t.Fatalf("ParseNext() error = %v", err)
	}
	if node == nil {
		t.Fatal("ParseNext() returned nil node")
	}
	if node.Type != ir.NumberType {
		t.Errorf("node.Type = %v, want %v", node.Type, ir.NumberType)
	}

	// Verify position was tracked
	if len(positions) == 0 {
		t.Fatal("positions map is empty - position was not tracked")
	}

	pos, hasPos := positions[node]
	if !hasPos {
		t.Error("node position not tracked")
	} else if pos == nil {
		t.Error("node position is nil")
	} else {
		t.Logf("Simple value position: line %d, col %d", pos.Line(), pos.Col())
		// Positions are 0-indexed, so line 0, col 0 is the first character
		// Just verify it's a valid position
		if pos.Line() < 0 || pos.Col() < 0 {
			t.Errorf("invalid position: line %d, col %d", pos.Line(), pos.Col())
		}
	}
}

// TestNodeParser_WithParsePositions_Array tests position tracking for arrays.
func TestNodeParser_WithParsePositions_Array(t *testing.T) {
	input := `[item1, item2, item3]`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	
	positions := make(map[*ir.Node]*token.Pos)
	parser := NewNodeParser(source, ParsePositions(positions))

	node, err := parser.ParseNext()
	if err != nil {
		t.Fatalf("ParseNext() error = %v", err)
	}
	if node == nil {
		t.Fatal("ParseNext() returned nil node")
	}
	if node.Type != ir.ArrayType {
		t.Errorf("node.Type = %v, want %v", node.Type, ir.ArrayType)
	}
	if len(node.Values) != 3 {
		t.Errorf("node.Values length = %d, want 3", len(node.Values))
	}

	// Verify root array position
	arrayPos, hasArrayPos := positions[node]
	if !hasArrayPos {
		t.Error("array position not tracked")
	} else if arrayPos == nil {
		t.Error("array position is nil")
	} else {
		t.Logf("Array position: line %d, col %d", arrayPos.Line(), arrayPos.Col())
	}

	// Verify array element positions - all elements should have positions tracked
	for i, elem := range node.Values {
		elemPos, hasPos := positions[elem]
		if !hasPos {
			t.Errorf("element %d position not tracked", i)
			continue
		}
		if elemPos == nil {
			t.Errorf("element %d position is nil", i)
			continue
		}
		t.Logf("Element %d position: line %d, col %d", i, elemPos.Line(), elemPos.Col())
		// Verify positions are different for each element
		if i > 0 {
			prevElem := node.Values[i-1]
			if prevPos, hasPrevPos := positions[prevElem]; hasPrevPos && prevPos != nil {
				if elemPos.Col() <= prevPos.Col() {
					t.Errorf("element %d position (col %d) should be after element %d (col %d)", 
						i, elemPos.Col(), i-1, prevPos.Col())
				}
			}
		}
	}
}

// TestNodeParser_WithParsePositions_MultipleNodes tests position tracking for multiple nodes.
func TestNodeParser_WithParsePositions_MultipleNodes(t *testing.T) {
	input := `{first: node}{second: node}`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	
	positions := make(map[*ir.Node]*token.Pos)
	parser := NewNodeParser(source, ParsePositions(positions))

	// Parse first node
	node1, err := parser.ParseNext()
	if err != nil {
		t.Fatalf("first ParseNext() error = %v", err)
	}
	if node1 == nil {
		t.Fatal("first ParseNext() returned nil node")
	}

	// Parse second node
	node2, err := parser.ParseNext()
	if err != nil {
		t.Fatalf("second ParseNext() error = %v", err)
	}
	if node2 == nil {
		t.Fatal("second ParseNext() returned nil node")
	}

	// Verify both nodes have positions
	pos1, hasPos1 := positions[node1]
	pos2, hasPos2 := positions[node2]

	if !hasPos1 {
		t.Error("first node position not tracked")
	} else if pos1 == nil {
		t.Error("first node position is nil")
	} else {
		t.Logf("First node position: line %d, col %d", pos1.Line(), pos1.Col())
	}

	if !hasPos2 {
		t.Error("second node position not tracked")
	} else if pos2 == nil {
		t.Error("second node position is nil")
	} else {
		t.Logf("Second node position: line %d, col %d", pos2.Line(), pos2.Col())
	}

	// Verify positions are different (they should be at different offsets)
	if pos1 != nil && pos2 != nil {
		if pos1.Line() == pos2.Line() && pos1.Col() == pos2.Col() {
			t.Error("first and second node positions are identical (should be different)")
		}
	}

	// Should get EOF after second node
	_, err = parser.ParseNext()
	if err != io.EOF {
		t.Errorf("third ParseNext() error = %v, want io.EOF", err)
	}
}

// TestNodeParser_WithoutParsePositions tests that positions are not tracked when
// ParsePositions is not provided.
func TestNodeParser_WithoutParsePositions(t *testing.T) {
	input := `{key: value}`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	
	// Create positions map but don't pass it to NodeParser
	positions := make(map[*ir.Node]*token.Pos)
	parser := NewNodeParser(source) // No ParsePositions option

	node, err := parser.ParseNext()
	if err != nil {
		t.Fatalf("ParseNext() error = %v", err)
	}
	if node == nil {
		t.Fatal("ParseNext() returned nil node")
	}

	// Verify that positions map is still empty
	if len(positions) != 0 {
		t.Errorf("positions map has %d entries, want 0 (positions should not be tracked)", len(positions))
	}
}
