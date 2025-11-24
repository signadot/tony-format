package server

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func TestMergeDiffs(t *testing.T) {
	tests := []struct {
		name      string
		direct    string // YAML string
		children  string // YAML string
		want      string // YAML string
		wantError bool
	}{
		{
			name:     "Merge Object + Object",
			direct:   `{ "a": 1 }`,
			children: `{ "b": 2 }`,
			want:     `{ "a": 1, "b": 2 }`,
		},
		{
			name:     "Merge Object + Object (Override)",
			direct:   `{ "a": 1 }`,
			children: `{ "a": 2 }`,
			want:     `{ "a": 2 }`, // Children override direct in current logic?
			// Wait, let's check logic:
			// combined[field.String] = direct.Values[i]
			// combined[field.String] = children.Values[i] (overwrites)
			// So yes, children override direct.
		},
		{
			name:     "Merge Sparse + Sparse",
			direct:   `!sparsearray { 1: "one" }`,
			children: `!sparsearray { 2: "two" }`,
			want:     `!sparsearray { 1: "one", 2: "two" }`,
		},
		{
			name:     "Merge Sparse + Object (Integer Keys)",
			direct:   `!sparsearray { 1: "one" }`,
			children: `{ "2": "two" }`,
			want:     `!sparsearray { 1: "one", 2: "two" }`,
		},
		{
			name:      "Merge Sparse + Object (Non-Integer Keys)",
			direct:    `!sparsearray { 1: "one" }`,
			children:  `{ "foo": "bar" }`,
			wantError: true,
		},
		{
			name:     "Merge Object + Sparse",
			direct:   `{ "a": 1 }`,
			children: `!sparsearray { 2: "two" }`,
			want:     `{ "2": "two", "a": 1 }`, // Keys must be sorted to match ir.FromMap
		},
		{
			name:     "Direct Only",
			direct:   `{ "a": 1 }`,
			children: "",
			want:     `{ "a": 1 }`,
		},
		{
			name:     "Children Only",
			direct:   "",
			children: `{ "b": 2 }`,
			want:     `{ "b": 2 }`,
		},
		{
			name:     "Type Mismatch (Direct takes precedence)",
			direct:   `[1, 2]`,
			children: `{ "a": 1 }`,
			want:     `[1, 2]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var directNode, childrenNode *ir.Node
			var err error

			if tt.direct != "" {
				directNode, err = parse.Parse([]byte(tt.direct))
				if err != nil {
					t.Fatalf("failed to parse direct: %v", err)
				}
			}

			if tt.children != "" {
				childrenNode, err = parse.Parse([]byte(tt.children))
				if err != nil {
					t.Fatalf("failed to parse children: %v", err)
				}
			}

			got, err := mergeDiffs(directNode, childrenNode)
			if (err != nil) != tt.wantError {
				t.Errorf("mergeDiffs() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if err != nil {
				return
			}

			wantNode, err := parse.Parse([]byte(tt.want))
			if err != nil {
				t.Fatalf("failed to parse want: %v", err)
			}

			// Normalize both want and got to ensure fair comparison
			// This removes parser artifacts like !bracket tags and comments
			// and clears parent pointers which are cyclic and hard to compare
			normalize(wantNode)
			normalize(got)

			// Compare structure
			if diff := cmp.Diff(wantNode, got, cmp.AllowUnexported(ir.Node{})); diff != "" {
				t.Errorf("mergeDiffs() mismatch (-want +got):\n%s", diff)
			}

			// Check Sparse Array Tag specifically if expected
			if ir.TagHas(wantNode.Tag, ir.IntKeysTag) {
				if !ir.TagHas(got.Tag, ir.IntKeysTag) {
					t.Error("expected result to be sparse array, but it wasn't")
				}
			} else {
				if ir.TagHas(got.Tag, ir.IntKeysTag) {
					t.Error("expected result NOT to be sparse array, but it was")
				}
			}
		})
	}
}

func normalize(n *ir.Node) {
	if n == nil {
		return
	}
	// Remove !bracket tag which is added by parser for JSON-like input
	// Also simplify !sparsearray.sparsearray to !sparsearray
	if strings.Contains(n.Tag, "bracket") {
		n.Tag = strings.ReplaceAll(n.Tag, "!bracket", "")
		n.Tag = strings.ReplaceAll(n.Tag, "bracket", "") // Handle composed cases
		n.Tag = strings.Trim(n.Tag, ".")
		if n.Tag != "" && !strings.HasPrefix(n.Tag, "!") {
			n.Tag = "!" + n.Tag
		}
	}

	// Simplify sparsearray tags for comparison
	if strings.Contains(n.Tag, "sparsearray") {
		n.Tag = "!sparsearray"
	}

	// Clear location/comment info
	n.Lines = nil
	n.Comment = nil

	// Clear parent pointers for comparison
	n.Parent = nil
	n.ParentIndex = 0
	n.ParentField = ""

	for _, field := range n.Fields {
		normalize(field)
	}
	for _, value := range n.Values {
		normalize(value)
	}
}
