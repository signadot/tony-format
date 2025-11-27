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
}
