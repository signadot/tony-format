package schema

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
)

// TestExtractRefName removed - legacy .name syntax no longer supported
// All references now use .[name] syntax

// TestBuildDependencyGraph removed - heuristic cycle detection replaced by SAT-based checking

func TestValidateCycles_ValidCycles(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{
		{
			name: "nullable escape hatch",
			schema: `
define:
  node:
    parent: !or
    - null
    - .[node]
`,
		},
		{
			name: "nullable escape hatch with !not.and",
			schema: `
define:
  node:
    parent: !not.and
    - !not null
    - .[node]
`,
		},
		{
			name: "nullable escape hatch with !and including null",
			schema: `
define:
  node:
    parent: !and
    - null
    - .[node]
`,
		},
		{
			name: "nullable escape hatch with !not.or[!not null]",
			schema: `
define:
  node:
    parent: !not.or
    - !not null
    - .[node]
`,
		},
		{
			name: "no cycles",
			schema: `
define:
  a:
    b: .[b]
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
			// ParseSchema includes SAT-based satisfiability check
			_, err = ParseSchema(node)
			if err != nil {
				t.Errorf("ParseSchema() error = %v, want nil", err)
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
accept: .[node]
define:
  node:
    parent: .[node]
`,
			wantErr: true,
		},
		{
			name: "mutual reference no escape",
			schema: `
accept: .[a]
define:
  a:
    b: .[b]
  b:
    a: .[a]
`,
			wantErr: true,
		},
		{
			name: "chain cycle no escape",
			schema: `
accept: .[a]
define:
  a:
    b: .[b]
  b:
    c: .[c]
  c:
    a: .[a]
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

// TestEscapeHatchDetection tests the SAT-based escape hatch detection
// with various boolean combinations.
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
    - .[node]
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
    - .[node]
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
    - .[node]
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
    - .[node]
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
    - .[node]
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
    - .[node]
`,
			wantErr: false,
		},

		// === VALID: Multi-node cycles with escape ===
		{
			name: "A->B->C->A with null !or on one edge",
			schema: `
define:
  a:
    b: .[b]
  b:
    c: !or
    - null
    - .[c]
  c:
    a: .[a]
`,
			wantErr: false,
		},

		// === INVALID: No escape hatch ===
		{
			name: "direct self-reference without escape",
			schema: `
accept: .[node]
define:
  node:
    next: .[node]
`,
			wantErr:    true,
			errContain: "impossible cycle",
		},
		{
			name: "!and[!not null, ref] - impossible: null excluded, must recurse forever",
			schema: `
accept: .[node]
define:
  node:
    next: !and
    - !not null
    - .[node]
`,
			// (NOT null) AND .[node] = must be a non-null .[node]
			// No escape hatch - impossible cycle
			wantErr:    true,
			errContain: "impossible cycle",
		},
		{
			name: "!not.or[null, ref] - valid: NOT .[node] can be anything else",
			schema: `
define:
  node:
    next: !not.or
    - null
    - .[node]
`,
			// NOT (null OR .[node]) = (NOT null) AND (NOT .[node])
			// Value cannot be null AND cannot be .[node]
			// But it CAN be a string, number, etc. - all break recursion
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
    - .[node]
`,
			// NOT (null AND .[node]) = (NOT null) OR (NOT .[node])
			// Satisfied by anything that's not-null OR not-.[node]
			// A string, number all satisfy (NOT .[node])
			// Schema is realizable: next: "hello" works
			wantErr: false,
		},

		// === INVALID: All edges in cycle lack escape ===
		{
			name: "A->B->A mutual reference no escape",
			schema: `
accept: .[a]
define:
  a:
    b: .[b]
  b:
    a: .[a]
`,
			wantErr:    true,
			errContain: "impossible cycle",
		},
		{
			name: "A->B->C->A chain cycle no escape",
			schema: `
accept: .[a]
define:
  a:
    b: .[b]
  b:
    c: .[c]
  c:
    a: .[a]
`,
			wantErr:    true,
			errContain: "impossible cycle",
		},

		// === EDGE CASES ===
		{
			name: "single element !or with null",
			schema: `
define:
  node:
    next: !or
    - !or
      - null
    - .[node]
`,
			wantErr: false,
		},
		{
			name: "reference not in cycle (no error)",
			schema: `
define:
  a:
    b: .[b]
  b:
    value: string
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

