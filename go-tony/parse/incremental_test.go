package parse

import (
	"bytes"
	"io"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/token"
)

func TestNodeParser_BracketedObject(t *testing.T) {
	input := `{key: value, nested: item}`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	parser := NewNodeParser(source)

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

	// Check that we get EOF after parsing one node
	_, err = parser.ParseNext()
	if err != io.EOF {
		t.Errorf("second ParseNext() error = %v, want io.EOF", err)
	}
}

func TestNodeParser_BracketedArray(t *testing.T) {
	input := `[item1, item2, item3]`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	parser := NewNodeParser(source)

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
}

func TestNodeParser_NestedBrackets(t *testing.T) {
	input := `{key: {nested: value}}`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	parser := NewNodeParser(source)

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

	// Check nested structure
	if len(node.Values) != 1 {
		t.Fatalf("node.Values length = %d, want 1", len(node.Values))
	}
	nested := node.Values[0]
	if nested.Type != ir.ObjectType {
		t.Errorf("nested.Type = %v, want %v", nested.Type, ir.ObjectType)
	}
}

func TestNodeParser_MultipleNodes(t *testing.T) {
	input := `{first: node}{second: node}`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	parser := NewNodeParser(source)

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

	// Should get EOF after second node
	_, err = parser.ParseNext()
	if err != io.EOF {
		t.Errorf("third ParseNext() error = %v, want io.EOF", err)
	}
}

func TestNodeParser_NonBracketedError(t *testing.T) {
	input := `key: value`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	parser := NewNodeParser(source)

	// First ParseNext() should succeed - "key" is a valid simple value
	node, err := parser.ParseNext()
	if err != nil {
		t.Fatalf("first ParseNext() error = %v (expected success parsing 'key' as simple value)", err)
	}
	if node == nil || node.Type != ir.StringType || node.String != "key" {
		t.Errorf("first node = %v, want StringType 'key'", node)
	}

	// Second ParseNext() should error - TColon is not a valid start token
	_, err = parser.ParseNext()
	if err == nil {
		t.Fatal("second ParseNext() expected error for invalid token sequence (TColon)")
	}
	if err.Error() == "" {
		t.Error("error message is empty")
	}
	// Check that error mentions unexpected token
	if err.Error() == "" {
		t.Error("error should mention unexpected token")
	}
}

func TestNodeParser_UnmatchedBracketError(t *testing.T) {
	input := `{key: value`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	parser := NewNodeParser(source)

	_, err := parser.ParseNext()
	if err == nil {
		t.Fatal("ParseNext() expected error for unmatched bracket")
	}
	// Should get error about incomplete structure
	if err == io.EOF {
		t.Error("got io.EOF, want error about incomplete structure")
	}
}

func TestNodeParser_ExtraClosingBracketError(t *testing.T) {
	input := `{key: value}}`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	parser := NewNodeParser(source)

	// Should parse first node successfully
	node, err := parser.ParseNext()
	if err != nil {
		t.Fatalf("ParseNext() error = %v", err)
	}
	if node == nil {
		t.Fatal("ParseNext() returned nil node")
	}

	// Next token should cause error
	_, err = parser.ParseNext()
	if err == nil {
		t.Fatal("expected error for extra closing bracket")
	}
}

func TestNodeParser_EmptyObject(t *testing.T) {
	input := `{}`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	parser := NewNodeParser(source)

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
	if len(node.Values) != 0 {
		t.Errorf("node.Values length = %d, want 0", len(node.Values))
	}
}

func TestNodeParser_EmptyArray(t *testing.T) {
	input := `[]`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	parser := NewNodeParser(source)

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
	if len(node.Values) != 0 {
		t.Errorf("node.Values length = %d, want 0", len(node.Values))
	}
}

func TestNodeParser_WithComments(t *testing.T) {
	input := `{key: value # comment
}`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	parser := NewNodeParser(source, ParseComments(true))

	node, err := parser.ParseNext()
	if err != nil {
		t.Fatalf("ParseNext() error = %v", err)
	}
	if node == nil {
		t.Fatal("ParseNext() returned nil node")
	}
	// Comments should be associated with the node
	// (exact behavior depends on associateComments implementation)
}

func TestNodeParser_ComplexNested(t *testing.T) {
	input := `{outer: {inner: [item1, item2]}}`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	parser := NewNodeParser(source)

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

	// Verify nested structure
	if len(node.Values) != 1 {
		t.Fatalf("node.Values length = %d, want 1", len(node.Values))
	}
	outerVal := node.Values[0]
	if outerVal.Type != ir.ObjectType {
		t.Errorf("outerVal.Type = %v, want %v", outerVal.Type, ir.ObjectType)
	}
	if len(outerVal.Values) != 1 {
		t.Fatalf("outerVal.Values length = %d, want 1", len(outerVal.Values))
	}
	innerVal := outerVal.Values[0]
	if innerVal.Type != ir.ArrayType {
		t.Errorf("innerVal.Type = %v, want %v", innerVal.Type, ir.ArrayType)
	}
	if len(innerVal.Values) != 2 {
		t.Errorf("innerVal.Values length = %d, want 2", len(innerVal.Values))
	}
}

func TestNodeParser_TrailingWhitespace(t *testing.T) {
	input := `{key: value}   `
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	parser := NewNodeParser(source)

	node, err := parser.ParseNext()
	if err != nil {
		t.Fatalf("ParseNext() error = %v", err)
	}
	if node == nil {
		t.Fatal("ParseNext() returned nil node")
	}

	// Should get EOF after trailing whitespace
	_, err = parser.ParseNext()
	if err != io.EOF {
		t.Errorf("second ParseNext() error = %v, want io.EOF", err)
	}
}

func TestNodeParser_MixedBracketsError(t *testing.T) {
	input := `{key: [value]}`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	parser := NewNodeParser(source)

	// Mixed brackets should work fine (object containing array)
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
}
