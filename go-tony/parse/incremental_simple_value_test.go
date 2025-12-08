package parse

import (
	"bytes"
	"io"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/token"
)

// TestNodeParser_SimpleStringValue tests parsing a simple string value
func TestNodeParser_SimpleStringValue(t *testing.T) {
	input := `"hello"`
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
	if node.Type != ir.StringType {
		t.Errorf("node.Type = %v, want %v", node.Type, ir.StringType)
	}
	if node.String != "hello" {
		t.Errorf("node.String = %q, want %q", node.String, "hello")
	}

	// Should get EOF after parsing one node
	_, err = parser.ParseNext()
	if err != io.EOF {
		t.Errorf("second ParseNext() error = %v, want io.EOF", err)
	}
}

// TestNodeParser_SimpleIntegerValue tests parsing a simple integer value
func TestNodeParser_SimpleIntegerValue(t *testing.T) {
	input := `42`
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
	if node.Type != ir.NumberType {
		t.Errorf("node.Type = %v, want %v", node.Type, ir.NumberType)
	}
	if node.Int64 == nil || *node.Int64 != 42 {
		t.Errorf("node.Int64 = %v, want 42", node.Int64)
	}
}

// TestNodeParser_SimpleBoolValue tests parsing boolean values
func TestNodeParser_SimpleBoolValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"true", "true", true},
		{"false", "false", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bytes.NewReader([]byte(tt.input))
			source := token.NewTokenSource(reader)
			parser := NewNodeParser(source)

			node, err := parser.ParseNext()
			if err != nil {
				t.Fatalf("ParseNext() error = %v", err)
			}
			if node == nil {
				t.Fatal("ParseNext() returned nil node")
			}
			if node.Type != ir.BoolType {
				t.Errorf("node.Type = %v, want %v", node.Type, ir.BoolType)
			}
			if node.Bool != tt.want {
				t.Errorf("node.Bool = %v, want %v", node.Bool, tt.want)
			}
		})
	}
}

// TestNodeParser_SimpleNullValue tests parsing null value
func TestNodeParser_SimpleNullValue(t *testing.T) {
	input := `null`
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
	if node.Type != ir.NullType {
		t.Errorf("node.Type = %v, want %v", node.Type, ir.NullType)
	}
}

// TestNodeParser_SimpleValueWithLeadingComments tests leading comments with simple values
func TestNodeParser_SimpleValueWithLeadingComments(t *testing.T) {
	input := `# leading comment
42`
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

	// Leading comments should wrap the node in a CommentType
	if node.Type != ir.CommentType {
		t.Errorf("node.Type = %v, want %v (leading comments should wrap node)", node.Type, ir.CommentType)
	}
	if len(node.Values) != 1 {
		t.Fatalf("node.Values length = %d, want 1", len(node.Values))
	}
	valueNode := node.Values[0]
	if valueNode.Type != ir.NumberType || valueNode.Int64 == nil || *valueNode.Int64 != 42 {
		t.Errorf("valueNode = %v, want NumberType with value 42", valueNode)
	}
	if len(node.Lines) == 0 {
		t.Error("expected comment lines in leading comment wrapper")
	}
}

// TestNodeParser_SimpleValueWithTrailingCommentSameLine tests trailing comment on same line
func TestNodeParser_SimpleValueWithTrailingCommentSameLine(t *testing.T) {
	input := `42 # same line comment`
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
	if node.Type != ir.NumberType {
		t.Errorf("node.Type = %v, want %v", node.Type, ir.NumberType)
	}
	if node.Comment == nil {
		t.Fatal("expected comment to be associated with node")
	}
	if len(node.Comment.Lines) == 0 {
		t.Error("expected comment lines")
	}
}

// TestNodeParser_SimpleValueWithTrailingCommentDifferentLine tests that comments on different lines are NOT associated
func TestNodeParser_SimpleValueWithTrailingCommentDifferentLine(t *testing.T) {
	input := `42
# different line comment`
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
	if node.Type != ir.NumberType {
		t.Errorf("node.Type = %v, want %v", node.Type, ir.NumberType)
	}
	// Comment on different line should NOT be associated with this node
	// (it will be re-associated later by associateComments)
	if node.Comment != nil {
		t.Errorf("node.Comment = %v, want nil (comment on different line should not be associated)", node.Comment)
	}

	// The comment should be available for the next node (or EOF)
	// Let's verify we can read it
	_, err = parser.ParseNext()
	// Should get EOF since there's no actual node after the comment
	if err != io.EOF {
		t.Errorf("second ParseNext() error = %v, want io.EOF (comment on different line not part of node)", err)
	}
}

// TestNodeParser_MultipleSimpleValues tests parsing multiple simple values in sequence
func TestNodeParser_MultipleSimpleValues(t *testing.T) {
	input := `"first" 42 true`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	parser := NewNodeParser(source)

	// Parse first value
	node1, err := parser.ParseNext()
	if err != nil {
		t.Fatalf("first ParseNext() error = %v", err)
	}
	if node1 == nil || node1.Type != ir.StringType || node1.String != "first" {
		t.Errorf("first node = %v, want StringType 'first'", node1)
	}

	// Parse second value
	node2, err := parser.ParseNext()
	if err != nil {
		t.Fatalf("second ParseNext() error = %v", err)
	}
	if node2 == nil || node2.Type != ir.NumberType || node2.Int64 == nil || *node2.Int64 != 42 {
		t.Errorf("second node = %v, want NumberType 42", node2)
	}

	// Parse third value
	node3, err := parser.ParseNext()
	if err != nil {
		t.Fatalf("third ParseNext() error = %v", err)
	}
	if node3 == nil || node3.Type != ir.BoolType || !node3.Bool {
		t.Errorf("third node = %v, want BoolType true", node3)
	}

	// Should get EOF after third node
	_, err = parser.ParseNext()
	if err != io.EOF {
		t.Errorf("fourth ParseNext() error = %v, want io.EOF", err)
	}
}

// TestNodeParser_SimpleValueAtEOFWithTrailingComment tests trailing comment at EOF
func TestNodeParser_SimpleValueAtEOFWithTrailingComment(t *testing.T) {
	input := `42 # trailing comment at EOF`
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
	if node.Type != ir.NumberType {
		t.Errorf("node.Type = %v, want %v", node.Type, ir.NumberType)
	}
	// Comment on same line should be associated
	if node.Comment == nil {
		t.Fatal("expected comment to be associated with node at EOF")
	}
}

// TestNodeParser_MixedBracketedAndSimpleValues tests mixing bracketed and simple values
func TestNodeParser_MixedBracketedAndSimpleValues(t *testing.T) {
	input := `{key: value} "simple"`
	reader := bytes.NewReader([]byte(input))
	source := token.NewTokenSource(reader)
	parser := NewNodeParser(source)

	// Parse bracketed structure
	node1, err := parser.ParseNext()
	if err != nil {
		t.Fatalf("first ParseNext() error = %v", err)
	}
	if node1 == nil || node1.Type != ir.ObjectType {
		t.Errorf("first node = %v, want ObjectType", node1)
	}

	// Parse simple value
	node2, err := parser.ParseNext()
	if err != nil {
		t.Fatalf("second ParseNext() error = %v", err)
	}
	if node2 == nil || node2.Type != ir.StringType || node2.String != "simple" {
		t.Errorf("second node = %v, want StringType 'simple'", node2)
	}
}
