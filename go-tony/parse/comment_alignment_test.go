package parse

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
)

// TestCommentAlignment tests that comments are correctly aligned with IR nodes
// according to the specification in docs/ir.md
func TestCommentAlignment(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, node *ir.Node)
	}{
		{
			name:  "head comment on scalar",
			input: "# head comment\nnull",
			check: func(t *testing.T, node *ir.Node) {
				// Head comment should wrap the node: node.Type == CommentType, node.Values[0] is the actual value
				if node.Type != ir.CommentType {
					t.Errorf("expected CommentType at root, got %v", node.Type)
					return
				}
				if len(node.Lines) == 0 {
					t.Error("expected head comment lines")
					return
				}
				if !strings.Contains(node.Lines[0], "head comment") {
					t.Errorf("expected head comment text, got %v", node.Lines)
				}
				if len(node.Values) != 1 {
					t.Errorf("expected 1 value (the actual node), got %d", len(node.Values))
					return
				}
				if node.Values[0].Type != ir.NullType {
					t.Errorf("expected NullType in Values[0], got %v", node.Values[0].Type)
				}
			},
		},
		{
			name:  "line comment on scalar",
			input: "null # line comment",
			check: func(t *testing.T, node *ir.Node) {
				// Line comment should be in node.Comment
				if node.Type != ir.NullType {
					t.Errorf("expected NullType, got %v", node.Type)
					return
				}
				if node.Comment == nil {
					t.Error("expected line comment in Comment field")
					return
				}
				if node.Comment.Type != ir.CommentType {
					t.Errorf("expected CommentType in Comment, got %v", node.Comment.Type)
				}
				if len(node.Comment.Lines) == 0 {
					t.Error("expected comment lines")
					return
				}
				// Line comment should preserve whitespace
				if !strings.Contains(node.Comment.Lines[0], "# line comment") {
					t.Errorf("expected line comment with whitespace preserved, got %q", node.Comment.Lines[0])
				}
			},
		},
		{
			name:  "trailing comment after value",
			input: "null\n# trailing comment",
			check: func(t *testing.T, node *ir.Node) {
				// Trailing comment should be appended to root node's Comment.Lines
				// with a dummy "" as first entry if no line comment
				if node.Type != ir.NullType {
					t.Errorf("expected NullType, got %v", node.Type)
					return
				}
				if node.Comment == nil {
					t.Error("expected trailing comment in Comment field")
					return
				}
				// Should have dummy "" followed by trailing comment
				if len(node.Comment.Lines) < 2 {
					t.Errorf("expected at least 2 lines (dummy + trailing), got %d: %v", len(node.Comment.Lines), node.Comment.Lines)
					return
				}
				if node.Comment.Lines[0] != "" {
					t.Errorf("expected empty string as first line (dummy), got %q", node.Comment.Lines[0])
				}
				found := false
				for _, ln := range node.Comment.Lines[1:] {
					if strings.Contains(ln, "trailing comment") {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected trailing comment in lines, got %v", node.Comment.Lines)
				}
			},
		},
		{
			name:  "line comment followed by trailing comment",
			input: "null # line\n# trailing",
			check: func(t *testing.T, node *ir.Node) {
				if node.Type != ir.NullType {
					t.Errorf("expected NullType, got %v", node.Type)
					return
				}
				if node.Comment == nil {
					t.Error("expected comments in Comment field")
					return
				}
				// Should have line comment followed by trailing
				if len(node.Comment.Lines) < 2 {
					t.Errorf("expected at least 2 lines, got %d: %v", len(node.Comment.Lines), node.Comment.Lines)
					return
				}
				// First line should be line comment with whitespace
				if !strings.Contains(node.Comment.Lines[0], "# line") {
					t.Errorf("expected line comment first, got %q", node.Comment.Lines[0])
				}
				// Second line should be trailing
				found := false
				for _, ln := range node.Comment.Lines[1:] {
					if strings.Contains(ln, "trailing") {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected trailing comment, got %v", node.Comment.Lines)
				}
			},
		},
		{
			name:  "head comment on object",
			input: "# object comment\nfoo: bar",
			check: func(t *testing.T, node *ir.Node) {
				if node.Type != ir.CommentType {
					t.Errorf("expected CommentType at root, got %v", node.Type)
					return
				}
				if len(node.Values) != 1 || node.Values[0].Type != ir.ObjectType {
					t.Error("expected ObjectType wrapped in comment")
					return
				}
				if !strings.Contains(strings.Join(node.Lines, " "), "object comment") {
					t.Errorf("expected head comment text, got %v", node.Lines)
				}
			},
		},
		{
			name:  "line comment on object value",
			input: "foo: bar # comment on value",
			check: func(t *testing.T, node *ir.Node) {
				if node.Type != ir.ObjectType {
					t.Errorf("expected ObjectType, got %v", node.Type)
					return
				}
				if len(node.Values) != 1 {
					t.Errorf("expected 1 value, got %d", len(node.Values))
					return
				}
				val := node.Values[0]
				if val.Comment == nil {
					t.Error("expected line comment on value")
					return
				}
				if !strings.Contains(strings.Join(val.Comment.Lines, " "), "comment on value") {
					t.Errorf("expected comment text, got %v", val.Comment.Lines)
				}
			},
		},
		{
			name: "head comment on array element",
			input: `- a
# comment on b
- b
- c`,
			check: func(t *testing.T, node *ir.Node) {
				if node.Type != ir.ArrayType {
					t.Errorf("expected ArrayType, got %v", node.Type)
					return
				}
				if len(node.Values) != 3 {
					t.Errorf("expected 3 values, got %d", len(node.Values))
					return
				}
				// Second element should have head comment
				second := node.Values[1]
				if second.Type != ir.CommentType {
					t.Errorf("expected CommentType wrapper for second element, got %v", second.Type)
					return
				}
				if !strings.Contains(strings.Join(second.Lines, " "), "comment on b") {
					t.Errorf("expected comment text, got %v", second.Lines)
				}
			},
		},
		{
			name: "line comment on array element",
			input: `- a # comment on a
- b
- c`,
			check: func(t *testing.T, node *ir.Node) {
				if node.Type != ir.ArrayType {
					t.Errorf("expected ArrayType, got %v", node.Type)
					return
				}
				if len(node.Values) != 3 {
					t.Errorf("expected 3 values, got %d", len(node.Values))
					return
				}
				// First element should have line comment
				first := node.Values[0]
				if first.Type != ir.StringType {
					t.Errorf("expected StringType for first element, got %v", first.Type)
					return
				}
				if first.Comment == nil {
					t.Error("expected line comment on first element")
					return
				}
				if !strings.Contains(strings.Join(first.Comment.Lines, " "), "comment on a") {
					t.Errorf("expected comment text, got %v", first.Comment.Lines)
				}
			},
		},
		{
			name:  "multiple head comment lines",
			input: "# line 1\n# line 2\nnull",
			check: func(t *testing.T, node *ir.Node) {
				if node.Type != ir.CommentType {
					t.Errorf("expected CommentType, got %v", node.Type)
					return
				}
				if len(node.Lines) < 2 {
					t.Errorf("expected at least 2 comment lines, got %d", len(node.Lines))
					return
				}
			},
		},
		{
			name:  "whitespace preservation in line comment",
			input: "null     # aligned comment",
			check: func(t *testing.T, node *ir.Node) {
				if node.Comment == nil {
					t.Error("expected line comment")
					return
				}
				// The whitespace between value and # should be preserved
				if len(node.Comment.Lines) == 0 {
					t.Error("expected comment lines")
					return
				}
				// Check that whitespace is preserved (should start with spaces)
				line := node.Comment.Lines[0]
				if !strings.HasPrefix(line, "     ") {
					t.Errorf("expected whitespace preserved, got %q", line)
				}
			},
		},
		{
			name: "nested object with comments",
			input: `# outer comment
outer:
  # inner comment
  inner: value # line comment`,
			check: func(t *testing.T, node *ir.Node) {
				// Root should be wrapped in comment
				if node.Type != ir.CommentType {
					t.Errorf("expected CommentType at root, got %v", node.Type)
					return
				}
				if !strings.Contains(strings.Join(node.Lines, " "), "outer comment") {
					t.Errorf("expected outer comment, got %v", node.Lines)
				}
				// Unwrap to get the object
				obj := node.Values[0]
				if obj.Type != ir.ObjectType {
					t.Errorf("expected ObjectType, got %v", obj.Type)
					return
				}
				// outer value should be an object with inner comment
				if len(obj.Values) != 1 {
					t.Errorf("expected 1 value (outer), got %d", len(obj.Values))
					return
				}
				innerObj := obj.Values[0]
				// The inner object might be wrapped in a comment for the inner comment
				if innerObj.Type == ir.CommentType {
					if !strings.Contains(strings.Join(innerObj.Lines, " "), "inner comment") {
						t.Errorf("expected inner comment, got %v", innerObj.Lines)
					}
					innerObj = innerObj.Values[0]
				}
				if innerObj.Type != ir.ObjectType {
					t.Errorf("expected inner ObjectType, got %v", innerObj.Type)
					return
				}
				// Check inner value has line comment
				if len(innerObj.Values) != 1 {
					t.Errorf("expected 1 inner value, got %d", len(innerObj.Values))
					return
				}
				innerVal := innerObj.Values[0]
				if innerVal.Comment == nil {
					t.Error("expected line comment on inner value")
				}
			},
		},
		{
			name:  "comment-only document",
			input: "# just a comment",
			check: func(t *testing.T, node *ir.Node) {
				// A document with only comments should have CommentType with empty Values
				if node == nil {
					t.Error("expected non-nil node for comment-only document")
					return
				}
				if node.Type != ir.CommentType {
					t.Errorf("expected CommentType for comment-only doc, got %v", node.Type)
					return
				}
				if len(node.Values) != 0 {
					t.Errorf("expected 0 values for comment-only doc, got %d", len(node.Values))
				}
			},
		},
		{
			name:  "empty line comment placeholder",
			input: "null\n# trailing 1\n# trailing 2",
			check: func(t *testing.T, node *ir.Node) {
				if node.Comment == nil {
					t.Error("expected comments")
					return
				}
				// First line should be empty (dummy placeholder)
				if len(node.Comment.Lines) < 1 || node.Comment.Lines[0] != "" {
					t.Errorf("expected empty placeholder as first line, got %v", node.Comment.Lines)
				}
				// Should have both trailing comments
				if len(node.Comment.Lines) < 3 {
					t.Errorf("expected at least 3 lines (placeholder + 2 trailing), got %v", node.Comment.Lines)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := Parse([]byte(tt.input), ParseComments(true))
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}
			if node == nil {
				t.Fatal("Parse returned nil node")
			}
			tt.check(t, node)
		})
	}
}

// TestCommentAlignmentRoundTrip tests that comments survive parse -> encode -> parse cycles
func TestCommentAlignmentRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "head comment",
			input: "# head comment\nnull",
		},
		{
			name:  "line comment",
			input: "null # line comment",
		},
		{
			name:  "both head and line",
			input: "# head\nnull # line",
		},
		{
			name:  "trailing comment",
			input: "null\n# trailing",
		},
		{
			name: "object with comments",
			input: `# header
foo: bar # inline
# trailing`,
		},
		{
			name: "array with comments",
			input: `# header
- a # comment a
# comment b
- b
- c`,
		},
		{
			name: "multiline comment",
			input: `# line 1
# line 2
# line 3
null`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// First parse
			node1, err := Parse([]byte(tt.input), ParseComments(true))
			if err != nil {
				t.Fatalf("First parse error: %v", err)
			}

			// Encode
			var buf bytes.Buffer
			err = encode.Encode(node1, &buf, encode.EncodeComments(true))
			if err != nil {
				t.Fatalf("Encode error: %v", err)
			}
			encoded := buf.String()
			t.Logf("Encoded:\n%s", encoded)

			// Second parse
			node2, err := Parse([]byte(encoded), ParseComments(true))
			if err != nil {
				t.Fatalf("Second parse error: %v", err)
			}

			// Compare comment structure
			if err := compareCommentStructure(node1, node2); err != nil {
				t.Errorf("Comment structure differs after round-trip: %v", err)
				t.Logf("Original:\n%s", tt.input)
				t.Logf("Encoded:\n%s", encoded)
			}
		})
	}
}

// compareCommentStructure compares the comment structure of two nodes
func compareCommentStructure(a, b *ir.Node) error {
	if a == nil && b == nil {
		return nil
	}
	if a == nil || b == nil {
		return errorf("one node is nil: a=%v, b=%v", a, b)
	}

	// Compare types
	if a.Type != b.Type {
		return errorf("type mismatch: %v vs %v", a.Type, b.Type)
	}

	// For comment nodes, compare lines
	if a.Type == ir.CommentType {
		if len(a.Lines) != len(b.Lines) {
			return errorf("comment lines count mismatch: %d vs %d", len(a.Lines), len(b.Lines))
		}
		// Compare wrapped values
		if len(a.Values) != len(b.Values) {
			return errorf("comment values count mismatch: %d vs %d", len(a.Values), len(b.Values))
		}
		for i := range a.Values {
			if err := compareCommentStructure(a.Values[i], b.Values[i]); err != nil {
				return errorf("comment value[%d]: %v", i, err)
			}
		}
		return nil
	}

	// Compare line comments (Comment field)
	aHasComment := a.Comment != nil
	bHasComment := b.Comment != nil
	if aHasComment != bHasComment {
		return errorf("comment field presence mismatch: %v vs %v", aHasComment, bHasComment)
	}
	if aHasComment {
		if len(a.Comment.Lines) != len(b.Comment.Lines) {
			return errorf("line comment count mismatch: %d vs %d", len(a.Comment.Lines), len(b.Comment.Lines))
		}
	}

	// Compare child values
	if len(a.Values) != len(b.Values) {
		return errorf("values count mismatch: %d vs %d", len(a.Values), len(b.Values))
	}
	for i := range a.Values {
		if err := compareCommentStructure(a.Values[i], b.Values[i]); err != nil {
			return errorf("value[%d]: %v", i, err)
		}
	}

	return nil
}

func errorf(format string, args ...interface{}) error {
	return &commentError{msg: fmt.Sprintf(format, args...)}
}

type commentError struct {
	msg string
}

func (e *commentError) Error() string {
	return e.msg
}

// TestCommentAlignmentVertical tests that vertically aligned comments preserve alignment
func TestCommentAlignmentVertical(t *testing.T) {
	input := `foo: 1    # comment 1
bar: 2    # comment 2
baz: 333  # comment 3`

	node, err := Parse([]byte(input), ParseComments(true))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	if node.Type != ir.ObjectType {
		t.Fatalf("expected ObjectType, got %v", node.Type)
	}

	// Check each value has a line comment with whitespace
	for i, val := range node.Values {
		if val.Comment == nil {
			t.Errorf("value[%d]: expected line comment", i)
			continue
		}
		if len(val.Comment.Lines) == 0 {
			t.Errorf("value[%d]: expected comment lines", i)
			continue
		}
		// The comment should have leading whitespace preserved
		line := val.Comment.Lines[0]
		if !strings.Contains(line, "#") {
			t.Errorf("value[%d]: expected # in comment, got %q", i, line)
		}
		// Check whitespace exists before #
		idx := strings.Index(line, "#")
		if idx == 0 {
			t.Errorf("value[%d]: expected whitespace before #, got %q", i, line)
		}
	}

	// Encode and verify alignment is preserved
	var buf bytes.Buffer
	err = encode.Encode(node, &buf, encode.EncodeComments(true))
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	encoded := buf.String()
	t.Logf("Encoded:\n%s", encoded)

	// Each line should still have # in it
	lines := strings.Split(strings.TrimSpace(encoded), "\n")
	commentCount := 0
	for _, line := range lines {
		if strings.Contains(line, "#") {
			commentCount++
		}
	}
	if commentCount != 3 {
		t.Errorf("expected 3 lines with comments, got %d", commentCount)
	}
}

// TestCommentAssociationBugs tests specific comment association bugs reported in issue #8
func TestCommentAssociationBugs(t *testing.T) {
	t.Run("line comment on container field should not become head comment on value", func(t *testing.T) {
		// Issue: "controlPlane:  # hello" should have # hello as a LINE comment
		// on the controlPlane value (the inner object), not as a HEAD comment
		// wrapping the inner object.
		input := `controlPlane:  # hello
  artifactsAPI: http://example.com`

		node, err := Parse([]byte(input), ParseComments(true))
		if err != nil {
			t.Fatalf("Parse error: %v", err)
		}

		if node.Type != ir.ObjectType {
			t.Fatalf("expected ObjectType at root, got %v", node.Type)
		}
		if len(node.Values) != 1 {
			t.Fatalf("expected 1 value, got %d", len(node.Values))
		}

		// The value of controlPlane should be an ObjectType with a .comment field
		// NOT a CommentType wrapping an ObjectType
		controlPlaneValue := node.Values[0]

		if controlPlaneValue.Type == ir.CommentType {
			t.Errorf("BUG: line comment '# hello' became a head comment wrapper\n"+
				"Expected: ObjectType with .comment field containing '# hello'\n"+
				"Got: CommentType wrapping ObjectType with lines: %v", controlPlaneValue.Lines)
			return
		}

		if controlPlaneValue.Type != ir.ObjectType {
			t.Errorf("expected ObjectType for controlPlane value, got %v", controlPlaneValue.Type)
			return
		}

		// The object should have a line comment in .comment field
		if controlPlaneValue.Comment == nil {
			t.Error("expected line comment in .comment field of controlPlane value")
			return
		}

		if !strings.Contains(strings.Join(controlPlaneValue.Comment.Lines, " "), "hello") {
			t.Errorf("expected '# hello' in comment, got %v", controlPlaneValue.Comment.Lines)
		}
	})

	t.Run("head comment before field should associate with field not inner value", func(t *testing.T) {
		// Issue: "# replicas\nreplicas:" should have # replicas as a HEAD comment
		// on the replicas field/value pair, not pushed down into the inner object.
		input := `# replicas
replicas:
  controllerManager: 2`

		node, err := Parse([]byte(input), ParseComments(true))
		if err != nil {
			t.Fatalf("Parse error: %v", err)
		}

		// Root should be wrapped in comment (head comment on the object)
		// OR the first field's value should have the head comment
		// But the inner object (controllerManager: 2) should NOT have the comment

		var rootObj *ir.Node
		if node.Type == ir.CommentType {
			if len(node.Values) == 0 {
				t.Fatal("CommentType has no values")
			}
			rootObj = node.Values[0]
		} else {
			rootObj = node
		}

		if rootObj.Type != ir.ObjectType {
			t.Fatalf("expected ObjectType, got %v", rootObj.Type)
		}

		if len(rootObj.Values) != 1 {
			t.Fatalf("expected 1 value (replicas), got %d", len(rootObj.Values))
		}

		replicasValue := rootObj.Values[0]

		// The replicas value should be wrapped in comment OR have the comment
		// But if we unwrap, the INNER object should NOT have the comment
		var innerObj *ir.Node
		if replicasValue.Type == ir.CommentType {
			// This is acceptable - head comment wraps the replicas value
			if !strings.Contains(strings.Join(replicasValue.Lines, " "), "replicas") {
				t.Errorf("expected '# replicas' in head comment, got %v", replicasValue.Lines)
			}
			if len(replicasValue.Values) == 0 {
				t.Fatal("CommentType has no values")
			}
			innerObj = replicasValue.Values[0]
		} else {
			innerObj = replicasValue
		}

		if innerObj.Type != ir.ObjectType {
			t.Fatalf("expected inner ObjectType, got %v", innerObj.Type)
		}

		// The INNER object's value (controllerManager: 2) should NOT have the # replicas comment
		if len(innerObj.Values) != 1 {
			t.Fatalf("expected 1 inner value, got %d", len(innerObj.Values))
		}

		controllerManagerValue := innerObj.Values[0]

		// Check if the comment incorrectly migrated to the inner value
		if controllerManagerValue.Type == ir.CommentType {
			if strings.Contains(strings.Join(controllerManagerValue.Lines, " "), "replicas") {
				t.Errorf("BUG: head comment '# replicas' incorrectly associated with inner value\n"+
					"Expected: comment on 'replicas' field/value, not on 'controllerManager'\n"+
					"Got: CommentType wrapping controllerManager with lines: %v", controllerManagerValue.Lines)
			}
		}
	})

	t.Run("combined line and head comments on nested structure", func(t *testing.T) {
		// Full reproduction of the issue from issue #8 comment
		input := `controlPlane:  # hello
  artifactsAPI: http://example.com
# replicas
replicas:
  controllerManager: 2`

		node, err := Parse([]byte(input), ParseComments(true))
		if err != nil {
			t.Fatalf("Parse error: %v", err)
		}

		if node.Type != ir.ObjectType {
			t.Fatalf("expected ObjectType at root, got %v", node.Type)
		}

		// Find controlPlane and replicas values
		var controlPlaneValue, replicasValue *ir.Node
		for i, field := range node.Fields {
			if field.String == "controlPlane" {
				controlPlaneValue = node.Values[i]
			} else if field.String == "replicas" {
				replicasValue = node.Values[i]
			}
		}

		if controlPlaneValue == nil {
			t.Fatal("controlPlane field not found")
		}
		if replicasValue == nil {
			t.Fatal("replicas field not found")
		}

		// Check controlPlane: the "# hello" should be a line comment, not head comment
		if controlPlaneValue.Type == ir.CommentType {
			t.Errorf("BUG: controlPlane's line comment became head comment wrapper\n"+
				"lines: %v", controlPlaneValue.Lines)
		}

		// Check replicas: the "# replicas" should be associated with replicas, not inner value
		if replicasValue.Type == ir.CommentType {
			// This is OK - head comment wraps replicas value
			innerObj := replicasValue.Values[0]
			if innerObj.Type == ir.ObjectType && len(innerObj.Values) > 0 {
				innerValue := innerObj.Values[0]
				if innerValue.Type == ir.CommentType {
					if strings.Contains(strings.Join(innerValue.Lines, " "), "replicas") {
						t.Errorf("BUG: '# replicas' comment pushed to inner controllerManager value\n"+
							"Expected: on replicas field\n"+
							"Got: on controllerManager with lines: %v", innerValue.Lines)
					}
				}
			}
		}
	})
}

// TestCommentIRStructure tests the IR structure matches the specification in docs/ir.md
func TestCommentIRStructure(t *testing.T) {
	t.Run("head comment structure", func(t *testing.T) {
		// Per ir.md: head comment is CommentType with 1 element in Values
		input := "# head\nvalue"
		node, err := Parse([]byte(input), ParseComments(true))
		if err != nil {
			t.Fatalf("Parse error: %v", err)
		}

		// Check structure
		if node.Type != ir.CommentType {
			t.Errorf("expected CommentType, got %v", node.Type)
		}
		if len(node.Values) != 1 {
			t.Errorf("expected 1 value (per ir.md), got %d", len(node.Values))
		}
		if len(node.Lines) == 0 {
			t.Error("expected Lines to contain comment text")
		}
	})

	t.Run("line comment structure", func(t *testing.T) {
		// Per ir.md: line comment is in .comment field of non-comment node
		input := "value # line"
		node, err := Parse([]byte(input), ParseComments(true))
		if err != nil {
			t.Fatalf("Parse error: %v", err)
		}

		if node.Type == ir.CommentType {
			t.Error("line comment should NOT wrap the node")
		}
		if node.Comment == nil {
			t.Error("expected Comment field to be set")
		}
		if node.Comment.Type != ir.CommentType {
			t.Errorf("expected Comment to be CommentType, got %v", node.Comment.Type)
		}
		if len(node.Comment.Values) != 0 {
			t.Errorf("line comment should have 0 values (per ir.md), got %d", len(node.Comment.Values))
		}
	})

	t.Run("trailing comment appended to line comment", func(t *testing.T) {
		// Per ir.md: trailing comments append to root's .comment.lines
		input := "value\n# trailing"
		node, err := Parse([]byte(input), ParseComments(true))
		if err != nil {
			t.Fatalf("Parse error: %v", err)
		}

		if node.Comment == nil {
			t.Error("expected Comment field for trailing comment")
			return
		}
		// Per ir.md: if no line comment, dummy "" is first
		if len(node.Comment.Lines) < 2 {
			t.Errorf("expected at least 2 lines (dummy + trailing), got %v", node.Comment.Lines)
		}
		if node.Comment.Lines[0] != "" {
			t.Errorf("expected empty first line (dummy), got %q", node.Comment.Lines[0])
		}
	})
}
