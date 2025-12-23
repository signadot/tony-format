package schema

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func TestExtractRefName(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		want     string
		wantNone bool
	}{
		{
			name: "simple reference",
			tag:  ".node",
			want: "node",
		},
		{
			name: "array with reference",
			tag:  ".array(.node)",
			want: "node",
		},
		{
			name: "array with type param",
			tag:  ".array(t)",
			wantNone: true,
		},
		{
			name: "nested array",
			tag:  ".array(.array(.node))",
			want: "node",
		},
		{
			name:     "no reference",
			tag:      "!irtype",
			wantNone: true,
		},
		{
			name:     "empty tag",
			tag:      "",
			wantNone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRefName(tt.tag)
			if tt.wantNone {
				if got != "" {
					t.Errorf("extractRefName() = %q, want empty", got)
				}
			} else {
				if got != tt.want {
					t.Errorf("extractRefName() = %q, want %q", got, tt.want)
				}
			}
		})
	}
}

func TestBuildDependencyGraph(t *testing.T) {
	tests := []struct {
		name      string
		define    map[string]*ir.Node
		wantEdges int
		wantNodes int
	}{
		{
			name: "simple self-reference",
			define: map[string]*ir.Node{
				"node": &ir.Node{
					Type: ir.ObjectType,
					Fields: []*ir.Node{ir.FromString("parent")},
					Values: []*ir.Node{
						&ir.Node{Tag: ".node", Type: ir.StringType},
					},
				},
			},
			wantEdges: 1,
			wantNodes: 1,
		},
		{
			name: "mutual reference",
			define: map[string]*ir.Node{
				"a": &ir.Node{
					Type: ir.ObjectType,
					Fields: []*ir.Node{ir.FromString("b")},
					Values: []*ir.Node{
						&ir.Node{Tag: ".b", Type: ir.StringType},
					},
				},
				"b": &ir.Node{
					Type: ir.ObjectType,
					Fields: []*ir.Node{ir.FromString("a")},
					Values: []*ir.Node{
						&ir.Node{Tag: ".a", Type: ir.StringType},
					},
				},
			},
			wantEdges: 2,
			wantNodes: 2,
		},
		{
			name: "array reference (escape hatch)",
			define: map[string]*ir.Node{
				"node": &ir.Node{
					Type: ir.ObjectType,
					Fields: []*ir.Node{ir.FromString("children")},
					Values: []*ir.Node{
						&ir.Node{Tag: ".array(.node)", Type: ir.ArrayType},
					},
				},
			},
			wantEdges: 1,
			wantNodes: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph, err := buildDependencyGraph(tt.define)
			if err != nil {
				t.Fatalf("buildDependencyGraph() error = %v", err)
			}
			if len(graph.Edges) != tt.wantEdges {
				t.Errorf("buildDependencyGraph() edges = %d, want %d", len(graph.Edges), tt.wantEdges)
			}
			if len(graph.Nodes) != tt.wantNodes {
				t.Errorf("buildDependencyGraph() nodes = %d, want %d", len(graph.Nodes), tt.wantNodes)
			}
		})
	}
}

func TestValidateCycles_ValidCycles(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{
		{
			name: "array escape hatch",
			schema: `
define:
  node:
    parent: .node
    children: .array(.node)
`,
		},
		{
			name: "nullable escape hatch",
			schema: `
define:
  node:
    parent: !or
    - null
    - .node
`,
		},
		{
			name: "nullable escape hatch with !not.and",
			schema: `
define:
  node:
    parent: !not.and
    - !not null
    - .node
`,
		},
		{
			name: "nullable escape hatch with array in !or",
			schema: `
define:
  node:
    parent: !or
    - .array(.node)
    - null
`,
		},
		{
			name: "nullable escape hatch with array in !and",
			schema: `
define:
  node:
    parent: !and
    - .array(.node)
    - null
`,
		},
		{
			name: "nullable escape hatch with !not.or containing array",
			schema: `
define:
  node:
    parent: !not.or
    - !not null
    - .array(.node)
`,
		},
		{
			name: "nullable escape hatch with !and including null",
			schema: `
define:
  node:
    parent: !and
    - null
    - .node
`,
		},
		{
			name: "nullable escape hatch with !not.or[!not null]",
			schema: `
define:
  node:
    parent: !not.or
    - !not null
    - .node
`,
		},
		{
			name: "no cycles",
			schema: `
define:
  a:
    b: .b
  b:
    value: string
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := parse.Parse([]byte(tt.schema))
			if err != nil {
				t.Fatalf("Failed to parse schema: %v", err)
			}
			schema, err := ParseSchema(node)
			if err != nil {
				t.Fatalf("Failed to parse schema: %v", err)
			}
			if err := ValidateCycles(schema); err != nil {
				t.Errorf("ValidateCycles() error = %v, want nil", err)
			}
		})
	}
}

func TestValidateCycles_ImpossibleCycles(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		wantErr bool
	}{
		{
			name: "self-reference no escape",
			schema: `
define:
  node:
    parent: .node
`,
			wantErr: true,
		},
		{
			name: "mutual reference no escape",
			schema: `
define:
  a:
    b: .b
  b:
    a: .a
`,
			wantErr: true,
		},
		{
			name: "chain cycle no escape",
			schema: `
define:
  a:
    b: .b
  b:
    c: .c
  c:
    a: .a
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := parse.Parse([]byte(tt.schema))
			if err != nil {
				t.Fatalf("Failed to parse schema: %v", err)
			}
			schema, err := ParseSchema(node)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSchema() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if err != nil && !strings.Contains(err.Error(), "impossible cycle") {
					t.Errorf("ParseSchema() error message should contain 'impossible cycle', got: %v", err)
				}
				return
			}
			// If no error expected, validate that schema was parsed successfully
			if schema == nil {
				t.Error("ParseSchema() returned nil schema but no error")
			}
		})
	}
}

func TestFindCycles(t *testing.T) {
		graph := &dependencyGraph{
		Nodes: map[string]bool{
			"a": true,
			"b": true,
			"c": true,
			"d": true,
		},
		Edges: []edge{
			{From: "a", To: "b"},
			{From: "b", To: "c"},
			{From: "c", To: "a"},
			{From: "d", To: "d"}, // self-loop
		},
	}

	cycles := findCycles(graph)
	
	// Should find the cycle a->b->c->a and the self-loop d->d
	if len(cycles) < 1 {
		t.Errorf("findCycles() found %d cycles, want at least 1", len(cycles))
	}
	
	// Check that we found the cycle a->b->c->a
	foundCycle := false
	for _, cyc := range cycles {
		if len(cyc) == 4 && cyc[0] == "a" && cyc[1] == "b" && cyc[2] == "c" && cyc[3] == "a" {
			foundCycle = true
			break
		}
	}
	if !foundCycle {
		t.Error("findCycles() did not find the cycle a->b->c->a")
	}
}

func TestFindCycles_NonTrivial(t *testing.T) {
	// Graph with a longer cycle and branches:
	// a -> b -> c -> d -> e -> c (cycle: c->d->e->c)
	// a -> f (branch, not part of cycle)
	// b -> g -> h (branch, not part of cycle)
	graph := &dependencyGraph{
		Nodes: map[string]bool{
			"a": true, "b": true, "c": true, "d": true,
			"e": true, "f": true, "g": true, "h": true,
		},
		Edges: []edge{
			{From: "a", To: "b"},
			{From: "a", To: "f"}, // branch
			{From: "b", To: "c"},
			{From: "b", To: "g"}, // branch
			{From: "g", To: "h"}, // branch
			{From: "c", To: "d"}, // cycle starts here
			{From: "d", To: "e"},
			{From: "e", To: "c"}, // cycle closes: c->d->e->c
		},
	}

	cycles := findCycles(graph)

	// Should find the cycle c->d->e->c
	foundCycle := false
	for _, cyc := range cycles {
		if len(cyc) == 4 && cyc[0] == "c" && cyc[1] == "d" && cyc[2] == "e" && cyc[3] == "c" {
			foundCycle = true
			break
		}
	}
	if !foundCycle {
		t.Errorf("findCycles() did not find the cycle c->d->e->c. Found cycles: %v", cycles)
	}

	// Should not include branches (a->f, b->g->h) in cycles
	for _, cyc := range cycles {
		for _, node := range cyc {
			if node == "f" || node == "g" || node == "h" {
				t.Errorf("findCycles() incorrectly included branch node %s in cycle: %v", node, cyc)
			}
		}
	}
}

// TestEscapeHatchDetection tests the SAT-based escape hatch detection
// with various boolean combinations of null and array types.
func TestEscapeHatchDetection(t *testing.T) {
	tests := []struct {
		name       string
		schema     string
		wantErr    bool
		errContain string
	}{
		// === VALID: Single escape hatches ===
		{
			name: "null in !or is escape hatch",
			schema: `
define:
  node:
    next: !or
    - null
    - .node
`,
			wantErr: false,
		},
		{
			name: "array wrapper is escape hatch",
			schema: `
define:
  node:
    children: .array(.node)
`,
			wantErr: false,
		},
		{
			name: "null AND ref is escape hatch (intersection with null)",
			schema: `
define:
  node:
    next: !and
    - null
    - .node
`,
			wantErr: false,
		},

		// === VALID: De Morgan transformations ===
		{
			name: "!not.and[!not null, ref] = null OR !ref (allows null)",
			schema: `
define:
  node:
    next: !not.and
    - !not null
    - .node
`,
			wantErr: false,
		},
		{
			name: "!not.or[!not null, ref] = null AND !ref (allows null)",
			schema: `
define:
  node:
    next: !not.or
    - !not null
    - .node
`,
			wantErr: false,
		},

		// === VALID: Mixed null and array ===
		{
			name: "!or with both null and array",
			schema: `
define:
  node:
    next: !or
    - null
    - .array(.node)
`,
			wantErr: false,
		},
		{
			name: "!and with null and array (both are escape hatches)",
			schema: `
define:
  node:
    next: !and
    - null
    - .array(.node)
`,
			wantErr: false,
		},

		// === VALID: Nested boolean expressions ===
		{
			name: "deeply nested !or containing null",
			schema: `
define:
  node:
    next: !or
    - !or
      - null
      - string
    - .node
`,
			wantErr: false,
		},
		{
			name: "!and containing !or with null",
			schema: `
define:
  node:
    next: !and
    - !or
      - null
      - number
    - .node
`,
			wantErr: false,
		},

		// === VALID: Triple negation ===
		{
			name: "!not.not.not null = !not null (excludes null) but array escapes",
			schema: `
define:
  node:
    next: !or
    - !not null
    - .array(.node)
`,
			wantErr: false,
		},

		// === VALID: Multi-node cycles with escape ===
		{
			name: "A->B->C->A with array on one edge",
			schema: `
define:
  a:
    b: .b
  b:
    c: .array(.c)
  c:
    a: .a
`,
			wantErr: false,
		},
		{
			name: "A->B->C->A with null !or on one edge",
			schema: `
define:
  a:
    b: .b
  b:
    c: !or
    - null
    - .c
  c:
    a: .a
`,
			wantErr: false,
		},

		// === INVALID: No escape hatch ===
		{
			name: "direct self-reference without escape",
			schema: `
define:
  node:
    next: .node
`,
			wantErr:    true,
			errContain: "impossible cycle",
		},
		{
			name: "!and[!not null, ref] - impossible: null excluded, must recurse forever",
			schema: `
accept: .node
define:
  node:
    next: !and
    - !not null
    - .node
`,
			// (NOT null) AND .node = must be a non-null .node
			// No escape hatch - impossible cycle
			wantErr:    true,
			errContain: "impossible cycle",
		},
		{
			name: "!not.or[null, ref] - valid: NOT .node can be anything else",
			schema: `
define:
  node:
    next: !not.or
    - null
    - .node
`,
			// NOT (null OR .node) = (NOT null) AND (NOT .node)
			// Value cannot be null AND cannot be .node
			// But it CAN be a string, number, array, etc. - all break recursion
			// Schema is realizable: next: "hello" satisfies the constraint
			wantErr: false,
		},
		{
			name: "!not.and[null, ref] - valid: almost anything satisfies this",
			schema: `
define:
  node:
    next: !not.and
    - null
    - .node
`,
			// NOT (null AND .node) = (NOT null) OR (NOT .node)
			// Satisfied by anything that's not-null OR not-.node
			// A string, number, array all satisfy (NOT .node)
			// Schema is realizable: next: "hello" works
			wantErr: false,
		},

		// === INVALID: All edges in cycle lack escape ===
		{
			name: "A->B->A mutual reference no escape",
			schema: `
define:
  a:
    b: .b
  b:
    a: .a
`,
			wantErr:    true,
			errContain: "impossible cycle",
		},
		{
			name: "A->B->C->A chain cycle no escape",
			schema: `
define:
  a:
    b: .b
  b:
    c: .c
  c:
    a: .a
`,
			wantErr:    true,
			errContain: "impossible cycle",
		},

		// === EDGE CASES ===
		{
			name: "empty !or array - current: allows (empty !or not detected)",
			schema: `
define:
  node:
    next: !and
    - !or []
    - .node
`,
			// Empty !or is always false, but current impl doesn't detect this
			wantErr: false,
		},
		{
			name: "single element !or with null",
			schema: `
define:
  node:
    next: !or
    - !or
      - null
    - .node
`,
			wantErr: false,
		},
		{
			name: "reference not in cycle (no error)",
			schema: `
define:
  a:
    b: .b
  b:
    value: string
`,
			wantErr: false,
		},
		{
			name: "self-loop with array escape",
			schema: `
define:
  node:
    self: .array(.node)
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := parse.Parse([]byte(tt.schema))
			if err != nil {
				t.Fatalf("Failed to parse schema: %v", err)
			}
			schema, err := ParseSchema(node)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseSchema() succeeded, want error containing %q", tt.errContain)
					return
				}
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("ParseSchema() error = %v, want error containing %q", err, tt.errContain)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseSchema() error = %v, want nil", err)
				return
			}
			if schema == nil {
				t.Error("ParseSchema() returned nil schema")
			}
		})
	}
}

// TestParameterizedTypeSatisfiability tests SAT-based checking with parameterized types
func TestParameterizedTypeSatisfiability(t *testing.T) {
	tests := []struct {
		name       string
		schema     string
		wantErr    bool
		errContain string
	}{
		{
			name: "parameterized list with null escape hatch",
			schema: `
define:
  list(t): !or
  - null
  - value: !t null
    next: .[list(t)]
accept: .[list(int)]
`,
			wantErr: false,
		},
		{
			name: "parameterized list without escape hatch",
			schema: `
define:
  list(t):
    value: !t null
    next: .[list(t)]
accept: .[list(int)]
`,
			wantErr:    true,
			errContain: "impossible cycle",
		},
		{
			name: "parameterized nullable wrapper",
			schema: `
define:
  nullable(t): !or [null, !t null]
  node:
    parent: .[nullable(node)]
accept: .[node]
`,
			wantErr: false,
		},
		{
			name: "nested parameterized reference",
			schema: `
define:
  tree(t): !or
  - null
  - value: !t null
    left: .[tree(t)]
    right: .[tree(t)]
accept: .[tree(string)]
`,
			wantErr: false,
		},
		{
			name: "parameterized type with impossible cycle",
			schema: `
define:
  wrapper(t): !and
  - !not null
  - !t null
  node:
    next: .[wrapper(node)]
accept: .[node]
`,
			// wrapper(node) = (!not null) AND node = must be non-null node
			// This forces infinite recursion - impossible
			wantErr:    true,
			errContain: "impossible cycle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := parse.Parse([]byte(tt.schema))
			if err != nil {
				t.Fatalf("Failed to parse schema: %v", err)
			}
			schema, err := ParseSchema(node)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseSchema() succeeded, want error containing %q", tt.errContain)
					return
				}
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("ParseSchema() error = %v, want error containing %q", err, tt.errContain)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseSchema() error = %v, want nil", err)
				return
			}
			if schema == nil {
				t.Error("ParseSchema() returned nil schema")
			}
		})
	}
}

func TestIsNullableTypeNode(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		path     string
		wantNull bool
	}{
		{
			name: "!or with null",
			schema: `
define:
  test:
    field: !or
    - null
    - string
`,
			path:     "define.test.field",
			wantNull: true,
		},
		{
			name: "!and with null",
			schema: `
define:
  test:
    field: !and
    - null
    - string
`,
			path:     "define.test.field",
			wantNull: true,
		},
		{
			name: "!not.and[!not null]",
			schema: `
define:
  test:
    field: !not.and
    - !not null
    - string
`,
			path:     "define.test.field",
			wantNull: true,
		},
		{
			name: "!not.or[!not null]",
			schema: `
define:
  test:
    field: !not.or
    - !not null
    - string
`,
			path:     "define.test.field",
			wantNull: true,
		},
		{
			name: "!not null (explicitly excludes null)",
			schema: `
define:
  test:
    field: !not null
`,
			path:     "define.test.field",
			wantNull: false,
		},
		{
			name: "!not.or[null] (De Morgan: !(null OR X) = !null AND !X)",
			schema: `
define:
  test:
    field: !not.or
    - null
    - string
`,
			path:     "define.test.field",
			wantNull: false,
		},
		{
			name: "!not.and[null] (De Morgan: !(null AND X) = !null OR !X)",
			schema: `
define:
  test:
    field: !not.and
    - null
    - string
`,
			path:     "define.test.field",
			wantNull: false, // !null OR !string doesn't guarantee null
		},
		// Additional cases for array escape hatches
		// NOTE: isNullableTypeNode checks for escape hatches (null OR array).
		// isArrayTypeNode only checks tags, not node.Type, so bare arrays
		// without tags are not detected as escape hatches by isNullableTypeNode.
		// However, extractReferences does detect ArrayType nodes as inArray=true.
		{
			name: "array type via node.Type - current: not detected by isNullableTypeNode",
			schema: `
define:
  test:
    field:
    - string
`,
			path:     "define.test.field",
			wantNull: false, // BUG: isArrayTypeNode only checks tags, not node.Type
		},
		{
			name: "!or with array",
			schema: `
define:
  test:
    field: !or
    - .array(string)
    - number
`,
			path:     "define.test.field",
			wantNull: true,
		},
		{
			name: "!not.or with array excludes array",
			schema: `
define:
  test:
    field: !not.or
    - .array(string)
    - number
`,
			path:     "define.test.field",
			wantNull: false, // !(array OR number) excludes array
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := parse.Parse([]byte(tt.schema))
			if err != nil {
				t.Fatalf("Failed to parse schema: %v", err)
			}
			
			// Navigate to the field
			parts := strings.Split(tt.path, ".")
			fieldNode := node
			for _, part := range parts {
				if part == "define" {
					fieldNode = ir.Get(fieldNode, "define")
				} else if fieldNode != nil && fieldNode.Type == ir.ObjectType {
					fieldNode = ir.Get(fieldNode, part)
				} else {
					t.Fatalf("Cannot navigate to %q", part)
				}
			}
			
			if fieldNode == nil {
				t.Fatalf("Field node is nil for path %q", tt.path)
			}
			
			got := isNullableTypeNode(fieldNode)
			if got != tt.wantNull {
				t.Errorf("isNullableTypeNode() = %v, want %v for path %q", got, tt.wantNull, tt.path)
			}
		})
	}
}
