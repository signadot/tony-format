package codegen

import (
	"go/parser"
	"testing"
)

func TestASTTypeToSchemaNode_Parameterized(t *testing.T) {
	tests := []struct {
		expr    string
		wantTag string
		wantStr string
	}{
		{"[]string", "", ".[array(string)]"},
		{"[]int", "", ".[array(int)]"},
		{"*string", "", ".[nullable(string)]"},
		{"*int", "", ".[nullable(int)]"},
		{"map[string]string", "", ".[object(string)]"},
		{"map[string]int", "", ".[object(int)]"},
		{"map[uint32]string", "", ".[sparsearray(string)]"},
		{"map[uint32]int", "", ".[sparsearray(int)]"},
		// Nested types fall back to object for now
		{"[]*string", "", ".[array(object)]"},
		{"map[string]*int", "", ".[object(object)]"},
		// Selector
		{"format.Format", "!format:format", ""},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			expr, err := parser.ParseExpr(tt.expr)
			if err != nil {
				t.Fatalf("ParseExpr(%q) failed: %v", tt.expr, err)
			}

			node, err := ASTTypeToSchemaNode(expr, nil, "", nil, nil)
			if err != nil {
				t.Fatalf("ASTTypeToSchemaNode() error = %v", err)
			}

			if node.Tag != tt.wantTag {
				t.Errorf("Tag = %q, want %q", node.Tag, tt.wantTag)
			}
			if node.String != tt.wantStr {
				t.Errorf("String = %q, want %q", node.String, tt.wantStr)
			}
		})
	}
}
