package gomap_test

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

type TestStruct struct {
	Name  string `tony:"field=name"`
	Value int    `tony:"field=value"`
}

func TestCommentHandling(t *testing.T) {
	// Create a Tony document with comments
	tonyWithComments := `# This is a comment
name: test  # inline comment
value: 42
`

	// Test 1: Parse WITH comments, then ToTony WITH comments
	t.Run("ParseWithComments_ToTonyWithComments", func(t *testing.T) {
		node, err := parse.Parse([]byte(tonyWithComments), parse.ParseComments(true))
		if err != nil {
			t.Fatalf("Parse error: %v", err)
		}

		var s TestStruct
		err = gomap.FromTonyIR(node, &s)
		if err != nil {
			t.Fatalf("FromTonyIR error: %v", err)
		}

		// Convert back to Tony with comments
		outNode, err := gomap.ToTonyIR(s)
		if err != nil {
			t.Fatalf("ToTonyIR error: %v", err)
		}

		// Note: ToTonyIR creates a new IR tree, so comments from original parse are not preserved
		// This is expected behavior - comments are tied to the parsed IR, not the struct
		t.Logf("Output node type: %v", outNode.Type)
	})

	// Test 2: Parse WITHOUT comments
	t.Run("ParseWithoutComments", func(t *testing.T) {
		node, err := parse.Parse([]byte(tonyWithComments), parse.ParseComments(false))
		if err != nil {
			t.Fatalf("Parse error: %v", err)
		}

		// Verify no comments in parsed node
		if node.Comment != nil {
			t.Errorf("Expected no comments, but found: %v", node.Comment)
		}

		var s TestStruct
		err = gomap.FromTonyIR(node, &s)
		if err != nil {
			t.Fatalf("FromTonyIR error: %v", err)
		}

		if s.Name != "test" || s.Value != 42 {
			t.Errorf("Unexpected values: Name=%s, Value=%d", s.Name, s.Value)
		}
	})

	// Test 3: ToTony with encode comments option
	t.Run("ToTonyWithEncodeComments", func(t *testing.T) {
		s := TestStruct{Name: "test", Value: 42}

		// ToTony without comments (default)
		bytes1, err := gomap.ToTony(s)
		if err != nil {
			t.Fatalf("ToTony error: %v", err)
		}
		t.Logf("Without comments:\n%s", string(bytes1))

		// Note: ToTony doesn't currently support passing encode options
		// This is a potential enhancement needed
	})

	// Test 4: Roundtrip with IR that has comments
	t.Run("RoundtripWithCommentsInIR", func(t *testing.T) {
		// Create IR node with comment manually
		node := ir.FromMap(map[string]*ir.Node{
			"name":  ir.FromString("test"),
			"value": ir.FromInt(42),
		})

		// Add comment to the node
		node.Comment = &ir.Node{
			Type:  ir.CommentType,
			Lines: []string{"This is a test comment"},
		}

		var s TestStruct
		err := gomap.FromTonyIR(node, &s)
		if err != nil {
			t.Fatalf("FromTonyIR error: %v", err)
		}

		// Convert back - comments won't be preserved
		outNode, err := gomap.ToTonyIR(s)
		if err != nil {
			t.Fatalf("ToTonyIR error: %v", err)
		}

		// Verify data is correct
		if s.Name != "test" || s.Value != 42 {
			t.Errorf("Unexpected values: Name=%s, Value=%d", s.Name, s.Value)
		}

		// Comments are not preserved through ToTonyIR (expected)
		if outNode.Comment != nil {
			t.Logf("Note: Comments found in output (unexpected): %v", outNode.Comment)
		}
	})
	// Test 5: Comment extraction with tags
	t.Run("CommentExtractionWithTags", func(t *testing.T) {
		type schemaTag struct{}
		type StructWithComments struct {
			schemaTag    `tony:"schema=test,comment=Comments,lineComment=LineComments"`
			Name         string `tony:"field=name"`
			Comments     []string
			LineComments []string
		}

		tonyData := `# Block comment
name: test # Inline comment
`
		node, err := parse.Parse([]byte(tonyData), parse.ParseComments(true))
		if err != nil {
			t.Fatalf("Parse error: %v", err)
		}

		var s StructWithComments
		err = gomap.FromTonyIR(node, &s)
		if err != nil {
			t.Fatalf("FromTonyIR error: %v", err)
		}

		// Verify block comments
		if len(s.Comments) != 1 || s.Comments[0] != "# Block comment" {
			t.Errorf("Expected block comment '# Block comment', got %v", s.Comments)
		}

		// Verify line comments
		// Note: The inline comment is attached to the "name" field value, not the root object.
		// However, the root object might have a line comment if it's on the same line as the start of the object?
		// In this case, "name: test # Inline comment" -> the comment is on the "test" string node.
		// The root object (StructWithComments) doesn't have a line comment here.

		// Let's try a case where the root object has a line comment (if possible in Tony)
		// Or check if the parser attaches comments to the root object.

		// Actually, for a struct, the "LineComments" field usually captures the line comment of the struct itself.
		// e.g.
		// myStruct: # line comment
		//   name: test

		// Let's construct a node manually to be sure about structure
		rootNode := ir.FromMap(map[string]*ir.Node{
			"name": ir.FromString("test"),
		})
		// Add block comment to root
		rootNodeWithComment := &ir.Node{
			Type:   ir.CommentType,
			Lines:  []string{"# Block comment"},
			Values: []*ir.Node{rootNode},
		}
		// Add line comment to root
		rootNode.Comment = &ir.Node{
			Type:  ir.CommentType,
			Lines: []string{"# Line comment"},
		}

		var s2 StructWithComments
		err = gomap.FromTonyIR(rootNodeWithComment, &s2)
		if err != nil {
			t.Fatalf("FromTonyIR error: %v", err)
		}

		if len(s2.Comments) != 1 || s2.Comments[0] != "# Block comment" {
			t.Errorf("Expected block comment '# Block comment', got %v", s2.Comments)
		}
		if len(s2.LineComments) != 1 || s2.LineComments[0] != "# Line comment" {
			t.Errorf("Expected line comment '# Line comment', got %v", s2.LineComments)
		}
	})
}
