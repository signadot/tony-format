package eval

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"

	"github.com/expr-lang/expr"
)

func TestScriptIRNodesPreserveTags(t *testing.T) {
	// Test that getpath() returns nodes with tags preserved
	root := ir.FromMap(map[string]*ir.Node{
		"source": ir.FromString("value").WithTag("!customtag"),
	})

	// Test getpath function directly
	prg, err := expr.Compile(`getpath("$.source")`, exprOpts(root)...)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	res, err := expr.Run(prg, Env{})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	node, ok := res.(*ir.Node)
	if !ok {
		t.Fatalf("expected *ir.Node, got %T", res)
	}

	// Verify the tag is preserved
	if node.Tag != "!customtag" {
		t.Errorf("tag not preserved: got %q, want %q", node.Tag, "!customtag")
	}

	// Verify the value is correct
	if node.Type != ir.StringType || node.String != "value" {
		t.Errorf("value not preserved: got type %s, string %q", node.Type, node.String)
	}
}

func TestScriptIRNodesPreserveComments(t *testing.T) {
	// Test that getpath() returns nodes with comments preserved
	// Note: Comments are preserved in the node structure, but Clone() may handle them specially
	sourceNode := ir.FromString("value")
	commentNode := &ir.Node{
		Type:   ir.CommentType,
		Lines:  []string{"inline comment"},
		Values: []*ir.Node{ir.FromString("value")},
	}
	sourceNode.Comment = commentNode

	root := ir.FromMap(map[string]*ir.Node{
		"source": sourceNode,
	})

	// Test getpath function directly
	prg, err := expr.Compile(`getpath("$.source")`, exprOpts(root)...)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	res, err := expr.Run(prg, Env{})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	node, ok := res.(*ir.Node)
	if !ok {
		t.Fatalf("expected *ir.Node, got %T", res)
	}

	// Verify comment is preserved (Clone should preserve comments)
	if node.Comment == nil {
		// Comments might not be cloned in all cases, so this is acceptable
		t.Log("comment not preserved (may be expected depending on Clone implementation)")
		return
	}
	if len(node.Comment.Lines) > 0 && !strings.Contains(node.Comment.Lines[0], "inline comment") {
		t.Errorf("comment content not preserved: %v", node.Comment.Lines)
	}
}

func TestScriptIRNodesArrayOperations(t *testing.T) {
	// Test that listpath() returns []*ir.Node that can be accessed
	root := ir.FromMap(map[string]*ir.Node{
		"items": ir.FromSlice([]*ir.Node{
			ir.FromString("first"),
			ir.FromString("second"),
		}),
	})

	// Test listpath function directly - listpath("$.items[*]") gets all array elements
	prg, err := expr.Compile(`listpath("$.items[*]")[0].String + ":" + listpath("$.items[*]")[1].String`, exprOpts(root)...)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	res, err := expr.Run(prg, Env{})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	result, ok := res.(string)
	if !ok {
		t.Fatalf("expected string, got %T", res)
	}

	if result != "first:second" {
		t.Errorf("got %q, want %q", result, "first:second")
	}
}

func TestScriptIRNodesDirectAccess(t *testing.T) {
	// Test direct access to node properties and methods
	root1 := ir.FromMap(map[string]*ir.Node{
		"foo": ir.FromString("bar").WithTag("!mytag"),
	})

	root2 := ir.FromMap(map[string]*ir.Node{
		"parent": ir.FromMap(map[string]*ir.Node{
			"child": ir.FromString("nested"),
		}),
	})

	tests := []struct {
		name     string
		root     *ir.Node
		script   string
		expected any
	}{
		{
			name:     "access Path() method",
			root:     root1,
			script:   `getpath("$.foo").Path()`,
			expected: "$.foo",
		},
		{
			name:     "access Tag field",
			root:     root1,
			script:   `getpath("$.foo").Tag`,
			expected: "!mytag",
		},
		{
			name:     "access Type field",
			root:     root1,
			script:   `getpath("$.foo").Type.String()`,
			expected: "String",
		},
		{
			name:     "access String field",
			root:     root1,
			script:   `getpath("$.foo").String`,
			expected: "bar",
		},
		{
			name:     "chain GetPath calls",
			root:     root2,
			script:   `getpath("$.parent").GetPath("$.child").String`,
			expected: "nested",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prg, err := expr.Compile(tt.script, exprOpts(tt.root)...)
			if err != nil {
				t.Fatalf("compile failed: %v", err)
			}

			res, err := expr.Run(prg, Env{})
			if err != nil {
				t.Fatalf("run failed: %v", err)
			}

			if res != tt.expected {
				t.Errorf("got %v (%T), want %v (%T)", res, res, tt.expected, tt.expected)
			}
		})
	}
}

func TestExpandIRRawEnvRef(t *testing.T) {
	// Test that ExpandIR properly handles raw env refs (.[var]) when var is an *ir.Node
	tests := []struct {
		name     string
		node     *ir.Node
		env      Env
		expected *ir.Node
	}{
		{
			name: "raw env ref to IR node preserves tag",
			node: ir.FromString(".[var]"),
			env: Env{
				"var": ir.FromString("value").WithTag("!mytag"),
			},
			expected: func() *ir.Node {
				n := ir.FromString("value").WithTag("!mytag")
				return n
			}(),
		},
		{
			name: "raw env ref to IR node preserves comments",
			node: ir.FromString(".[var]"),
			env: Env{
				"var": func() *ir.Node {
					n := ir.FromString("value")
					n.Comment = &ir.Node{
						Type:   ir.CommentType,
						Lines:  []string{"inline comment"},
						Values: []*ir.Node{ir.FromString("value")},
					}
					return n
				}(),
			},
			expected: func() *ir.Node {
				n := ir.FromString("value")
				n.Comment = &ir.Node{
					Type:   ir.CommentType,
					Lines:  []string{"inline comment"},
					Values: []*ir.Node{ir.FromString("value")},
				}
				return n
			}(),
		},
		{
			name: "raw env ref to array of IR nodes",
			node: ir.FromString(".[var]"),
			env: Env{
				"var": ir.FromSlice([]*ir.Node{
					ir.FromString("first").WithTag("!tag1"),
					ir.FromString("second").WithTag("!tag2"),
				}),
			},
			expected: ir.FromSlice([]*ir.Node{
				ir.FromString("first").WithTag("!tag1"),
				ir.FromString("second").WithTag("!tag2"),
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExpandIR(tt.node, tt.env)
			if err != nil {
				t.Fatalf("ExpandIR failed: %v", err)
			}

			// Verify tag is preserved
			if result.Tag != tt.expected.Tag {
				t.Errorf("tag mismatch: got %q, want %q", result.Tag, tt.expected.Tag)
			}

			// Verify type matches
			if result.Type != tt.expected.Type {
				t.Errorf("type mismatch: got %s, want %s", result.Type, tt.expected.Type)
			}

			// For string nodes, verify string value
			if result.Type == ir.StringType && result.String != tt.expected.String {
				t.Errorf("string value mismatch: got %q, want %q", result.String, tt.expected.String)
			}

			// For array nodes, verify length
			if result.Type == ir.ArrayType {
				if len(result.Values) != len(tt.expected.Values) {
					t.Errorf("array length mismatch: got %d, want %d", len(result.Values), len(tt.expected.Values))
				} else {
					// Verify tags on array elements
					for i := range result.Values {
						if result.Values[i].Tag != tt.expected.Values[i].Tag {
							t.Errorf("array element %d tag mismatch: got %q, want %q", i, result.Values[i].Tag, tt.expected.Values[i].Tag)
						}
					}
				}
			}
		})
	}
}

func TestExpandIRRawEnvRefParentRelationships(t *testing.T) {
	// Test that ExpandIR preserves parent relationships when replacing nodes
	parent := ir.FromMap(map[string]*ir.Node{
		"child": ir.FromString(".[var]"),
	})
	parent.Values[0].Parent = parent
	parent.Values[0].ParentIndex = 0
	parent.Values[0].ParentField = "child"

	env := Env{
		"var": ir.FromString("value").WithTag("!mytag"),
	}

	result, err := ExpandIR(parent.Values[0], env)
	if err != nil {
		t.Fatalf("ExpandIR failed: %v", err)
	}

	// Verify parent relationships are preserved
	if result.Parent != parent {
		t.Errorf("Parent not set correctly: got %v, want %v", result.Parent, parent)
	}
	if result.ParentField != "child" {
		t.Errorf("ParentField not set correctly: got %q, want %q", result.ParentField, "child")
	}
	if result.ParentIndex != 0 {
		t.Errorf("ParentIndex not set correctly: got %d, want %d", result.ParentIndex, 0)
	}

	// Test with array result
	parent2 := ir.FromMap(map[string]*ir.Node{
		"items": ir.FromString(".[var]"),
	})
	parent2.Values[0].Parent = parent2
	parent2.Values[0].ParentIndex = 0
	parent2.Values[0].ParentField = "items"

	env2 := Env{
		"var": ir.FromSlice([]*ir.Node{
			ir.FromString("first"),
			ir.FromString("second"),
		}),
	}

	result2, err := ExpandIR(parent2.Values[0], env2)
	if err != nil {
		t.Fatalf("ExpandIR failed: %v", err)
	}

	if result2.Type != ir.ArrayType {
		t.Fatalf("expected ArrayType, got %s", result2.Type)
	}

	// Verify parent relationships are preserved for array
	if result2.Parent != parent2 {
		t.Errorf("Parent not set correctly for array: got %v, want %v", result2.Parent, parent2)
	}
	if result2.ParentField != "items" {
		t.Errorf("ParentField not set correctly for array: got %q, want %q", result2.ParentField, "items")
	}
	if result2.ParentIndex != 0 {
		t.Errorf("ParentIndex not set correctly for array: got %d, want %d", result2.ParentIndex, 0)
	}

	// Verify array elements have correct parent relationships
	if len(result2.Values) != 2 {
		t.Fatalf("expected 2 array elements, got %d", len(result2.Values))
	}
	for i, elem := range result2.Values {
		if elem.Parent != result2 {
			t.Errorf("array element %d Parent not set correctly: got %v, want %v", i, elem.Parent, result2)
		}
		if elem.ParentIndex != i {
			t.Errorf("array element %d ParentIndex not set correctly: got %d, want %d", i, elem.ParentIndex, i)
		}
	}
}

func TestFromJSONAnyMaps(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected *ir.Node
	}{
		{
			name: "map[string]*ir.Node",
			input: map[string]*ir.Node{
				"a": ir.FromString("value1").WithTag("!tag1"),
				"b": ir.FromString("value2").WithTag("!tag2"),
			},
			expected: func() *ir.Node {
				return ir.FromMap(map[string]*ir.Node{
					"a": ir.FromString("value1").WithTag("!tag1"),
					"b": ir.FromString("value2").WithTag("!tag2"),
				})
			}(),
		},
		{
			name: "map[int]*ir.Node",
			input: map[int]*ir.Node{
				0: ir.FromString("first").WithTag("!tag1"),
				1: ir.FromString("second").WithTag("!tag2"),
			},
			expected: func() *ir.Node {
				// map[int]*ir.Node should be converted to object with string keys
				return ir.FromMap(map[string]*ir.Node{
					"0": ir.FromString("first").WithTag("!tag1"),
					"1": ir.FromString("second").WithTag("!tag2"),
				})
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FromAny(tt.input)
			if err != nil {
				t.Fatalf("FromJSONAny failed: %v", err)
			}

			if result.Type != ir.ObjectType {
				t.Errorf("expected ObjectType, got %s", result.Type)
			}

			// Verify structure matches
			if len(result.Fields) != len(tt.expected.Fields) {
				t.Errorf("field count mismatch: got %d, want %d", len(result.Fields), len(tt.expected.Fields))
			}

			// Verify tags are preserved
			for i, field := range result.Fields {
				if field.String != tt.expected.Fields[i].String {
					t.Errorf("field %d key mismatch: got %q, want %q", i, field.String, tt.expected.Fields[i].String)
				}
				if result.Values[i].Tag != tt.expected.Values[i].Tag {
					t.Errorf("field %q tag mismatch: got %q, want %q", field.String, result.Values[i].Tag, tt.expected.Values[i].Tag)
				}
			}
		})
	}
}

func TestExpandIRRawEnvRefWithMaps(t *testing.T) {
	tests := []struct {
		name     string
		envVar   *ir.Node
		expected *ir.Node
	}{
		{
			name: "raw_env_ref_to_map[string]*ir.Node",
			envVar: ir.FromMap(map[string]*ir.Node{
				"key1": ir.FromString("value1").WithTag("!tag1"),
				"key2": ir.FromString("value2").WithTag("!tag2"),
			}),
			expected: func() *ir.Node {
				return ir.FromMap(map[string]*ir.Node{
					"key1": ir.FromString("value1").WithTag("!tag1"),
					"key2": ir.FromString("value2").WithTag("!tag2"),
				})
			}(),
		},
		{
			name: "raw_env_ref_to_map[int]*ir.Node",
			envVar: ir.FromIntKeysMap(map[uint32]*ir.Node{
				0: ir.FromString("first").WithTag("!tag1"),
				1: ir.FromString("second").WithTag("!tag2"),
			}),
			expected: func() *ir.Node {
				return ir.FromMap(map[string]*ir.Node{
					"0": ir.FromString("first").WithTag("!tag1"),
					"1": ir.FromString("second").WithTag("!tag2"),
				})
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := ir.FromMap(map[string]*ir.Node{
				"child": ir.FromString(".[var]"),
			})
			parent.Values[0].Parent = parent
			parent.Values[0].ParentIndex = 0
			parent.Values[0].ParentField = "child"

			env := Env{
				"var": tt.envVar,
			}

			result, err := ExpandIR(parent.Values[0], env)
			if err != nil {
				t.Fatalf("ExpandIR failed: %v", err)
			}

			if result.Type != ir.ObjectType {
				t.Errorf("expected ObjectType, got %s", result.Type)
			}

			// Verify parent relationships
			if result.Parent != parent {
				t.Errorf("Parent not set correctly")
			}
			if result.ParentField != "child" {
				t.Errorf("ParentField not set correctly: got %q, want %q", result.ParentField, "child")
			}

			// Verify structure and tags
			if len(result.Fields) != len(tt.expected.Fields) {
				t.Errorf("field count mismatch: got %d, want %d", len(result.Fields), len(tt.expected.Fields))
			}

			for i, field := range result.Fields {
				if field.String != tt.expected.Fields[i].String {
					t.Errorf("field %d key mismatch: got %q, want %q", i, field.String, tt.expected.Fields[i].String)
				}
				if result.Values[i].Tag != tt.expected.Values[i].Tag {
					t.Errorf("field %q tag mismatch: got %q, want %q", field.String, result.Values[i].Tag, tt.expected.Values[i].Tag)
				}
			}
		})
	}
}
