package token

import (
	"bytes"
	"testing"
)

// TestMixedSyntax_OptionalCommasAndColons tests path tracking with mixed syntax:
// - Optional commas in arrays and objects
// - Optional colons in objects (keys without colons have implicit values)
// - Mixing these features together
func TestMixedSyntax_OptionalCommasAndColons(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []struct {
			path  string
			token TokenType
		}
	}{
		{
			name:  "array with commas",
			input: `[a, b, c]`,
			expected: []struct {
				path  string
				token TokenType
			}{
				{"[0]", TLiteral},
				{"[1]", TLiteral},
				{"[2]", TLiteral},
			},
		},
		{
			name:  "array without commas",
			input: `[a b c]`,
			expected: []struct {
				path  string
				token TokenType
			}{
				{"[0]", TLiteral},
				{"[1]", TLiteral},
				{"[2]", TLiteral},
			},
		},
		{
			name:  "array mixed commas",
			input: `[a, b c, d]`,
			expected: []struct {
				path  string
				token TokenType
			}{
				{"[0]", TLiteral},
				{"[1]", TLiteral},
				{"[2]", TLiteral},
				{"[3]", TLiteral},
			},
		},
		{
			name:  "object with colons",
			input: `{a: 1, b: 2}`,
			expected: []struct {
				path  string
				token TokenType
			}{
				{"a", TInteger}, // key "a" with value 1
				{"b", TInteger}, // key "b" with value 2
			},
		},
		{
			name:  "object without colons (implicit values)",
			input: `{a b}`,
			expected: []struct {
				path  string
				token TokenType
			}{
				{"a", TLiteral}, // key "a" with implicit value "b"
			},
		},
		{
			name:  "object mixed colons",
			input: `{a: 1, b c, d: 3}`,
			expected: []struct {
				path  string
				token TokenType
			}{
				{"a", TInteger}, // key "a" with value 1
				{"b", TLiteral}, // key "b" with implicit value "c"
				{"d", TInteger}, // key "d" with value 3
			},
		},
		{
			name:  "object mixed colons and commas",
			input: `{a: 1, b c, d: 4}`,
			expected: []struct {
				path  string
				token TokenType
			}{
				{"a", TInteger}, // key "a" with value 1
				{"b", TLiteral}, // key "b" with implicit value "c"
				{"d", TInteger}, // key "d" with value 4
			},
		},
		{
			name:  "nested mixed syntax",
			input: `{arr: [a, b c], obj: {x: 1, y z}}`,
			expected: []struct {
				path  string
				token TokenType
			}{
				{"arr[0]", TLiteral},  // array element
				{"arr[1]", TLiteral},  // array element
				{"arr[2]", TLiteral},  // array element
				{"obj", TLiteral},     // key "obj" (the object key itself)
				{"obj.x", TInteger},   // key "x" with value 1
				{"obj.y", TLiteral},   // key "y" with implicit value "z"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			var nodeStarts []struct {
				offset int
				path   string
				token  Token
			}

			onNodeStart := func(offset int, path string, token Token) {
				// Track all node starts (including structural tokens for debugging)
				nodeStarts = append(nodeStarts, struct {
					offset int
					path   string
					token  Token
				}{offset, path, token})
			}

			sink := NewTokenSink(&buf, onNodeStart)

			// Tokenize input
			tokens, err := Tokenize(nil, []byte(tt.input))
			if err != nil {
				t.Fatalf("Tokenize error: %v", err)
			}

			// Write tokens
			if err := sink.Write(tokens); err != nil {
				t.Fatalf("Write error: %v", err)
			}

			// Filter to only value tokens with paths (skip structural tokens like brackets)
			var valuePaths []struct {
				path  string
				token TokenType
			}
			for _, ns := range nodeStarts {
				// Skip structural tokens and root path (empty string)
				if ns.path != "" && ns.token.Type != TLCurl && ns.token.Type != TRCurl &&
					ns.token.Type != TLSquare && ns.token.Type != TRSquare &&
					ns.token.Type != TComma && ns.token.Type != TColon {
					valuePaths = append(valuePaths, struct {
						path  string
						token TokenType
					}{ns.path, ns.token.Type})
				}
			}

			// Verify expected paths
			if len(valuePaths) != len(tt.expected) {
				t.Logf("Node starts detected: %d", len(nodeStarts))
				for i, ns := range nodeStarts {
					t.Logf("  [%d] offset=%d, path=%q, token=%v", i, ns.offset, ns.path, ns.token.Type)
				}
				t.Logf("Value paths: %d", len(valuePaths))
				for i, vp := range valuePaths {
					t.Logf("  [%d] path=%q, token=%v", i, vp.path, vp.token)
				}
				t.Errorf("Expected %d paths, got %d", len(tt.expected), len(valuePaths))
				return
			}

			for i, expected := range tt.expected {
				if i >= len(valuePaths) {
					t.Errorf("Missing path at index %d: expected %q", i, expected.path)
					continue
				}
				actual := valuePaths[i]
				if actual.path != expected.path {
					t.Errorf("Path mismatch at index %d: expected %q, got %q", i, expected.path, actual.path)
				}
				if actual.token != expected.token {
					t.Errorf("Token type mismatch at index %d: expected %v, got %v", i, expected.token, actual.token)
				}
			}
		})
	}
}
