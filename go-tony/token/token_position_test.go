package token

import (
	"testing"
)

// TestTokenPositions verifies that tokenizer reports correct positions for tokens.
func TestTokenPositions(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		checks []posCheck // checks for specific tokens
	}{
		{
			name:  "simple literal",
			input: "hello",
			checks: []posCheck{
				{typ: TLiteral, line: 0, col: 0, offset: 0, bytes: "hello"},
			},
		},
		{
			name:  "two lines",
			input: "a\nb",
			checks: []posCheck{
				{typ: TLiteral, line: 0, col: 0, offset: 0, bytes: "a"},
				{typ: TIndent, line: 1, col: 0, offset: 2, bytes: ""},
				{typ: TLiteral, line: 1, col: 0, offset: 2, bytes: "b"},
			},
		},
		{
			name:  "indented second line",
			input: "a:\n  b",
			checks: []posCheck{
				{typ: TLiteral, line: 0, col: 0, offset: 0, bytes: "a"},
				{typ: TColon, line: 0, col: 1, offset: 1, bytes: ":"},
				{typ: TIndent, line: 1, col: 0, offset: 3, bytes: "  "},
				{typ: TLiteral, line: 1, col: 2, offset: 5, bytes: "b"},
			},
		},
		{
			name:  "array elements",
			input: "- a\n- b",
			checks: []posCheck{
				{typ: TArrayElt, line: 0, col: 0, offset: 0, bytes: "- "},
				{typ: TLiteral, line: 0, col: 2, offset: 2, bytes: "a"},
				{typ: TIndent, line: 1, col: 0, offset: 4, bytes: ""},
				{typ: TArrayElt, line: 1, col: 0, offset: 4, bytes: "- "},
				{typ: TLiteral, line: 1, col: 2, offset: 6, bytes: "b"},
			},
		},
		{
			name:  "bracketed object",
			input: "{a: 1}",
			checks: []posCheck{
				{typ: TLCurl, line: 0, col: 0, offset: 0, bytes: "{"},
				{typ: TLiteral, line: 0, col: 1, offset: 1, bytes: "a"},
				{typ: TColon, line: 0, col: 2, offset: 2, bytes: ":"},
				{typ: TInteger, line: 0, col: 4, offset: 4, bytes: "1"},
				{typ: TRCurl, line: 0, col: 5, offset: 5, bytes: "}"},
			},
		},
		{
			name:  "string token",
			input: `"hello"`,
			checks: []posCheck{
				{typ: TString, line: 0, col: 0, offset: 0, bytes: `"hello"`},
			},
		},
		{
			name:  "comment",
			input: "# comment\na",
			checks: []posCheck{
				{typ: TComment, line: 0, col: 0, offset: 0, bytes: "# comment"},
				{typ: TIndent, line: 1, col: 0, offset: 10, bytes: ""},
				{typ: TLiteral, line: 1, col: 0, offset: 10, bytes: "a"},
			},
		},
		{
			name:  "line comment after colon",
			input: "a: b # comment",
			checks: []posCheck{
				{typ: TLiteral, line: 0, col: 0, offset: 0, bytes: "a"},
				{typ: TColon, line: 0, col: 1, offset: 1, bytes: ":"},
				{typ: TLiteral, line: 0, col: 3, offset: 3, bytes: "b"},
				{typ: TLineComment, line: 0, col: 4, offset: 4, bytes: " # comment"},
			},
		},
		{
			name:  "tag",
			input: "!tag value",
			checks: []posCheck{
				{typ: TTag, line: 0, col: 0, offset: 0, bytes: "!tag"},
				{typ: TLiteral, line: 0, col: 5, offset: 5, bytes: "value"},
			},
		},
		{
			name:  "multiline literal",
			input: "|\n  line1\n  line2",
			checks: []posCheck{
				{typ: TMLit, line: 0, col: 0, offset: 0},
			},
		},
		{
			name:  "numbers",
			input: "123 -45 3.14",
			checks: []posCheck{
				{typ: TInteger, line: 0, col: 0, offset: 0, bytes: "123"},
				{typ: TInteger, line: 0, col: 4, offset: 4, bytes: "-45"},
				{typ: TFloat, line: 0, col: 8, offset: 8, bytes: "3.14"},
			},
		},
		{
			name:  "keywords",
			input: "null true false",
			checks: []posCheck{
				{typ: TNull, line: 0, col: 0, offset: 0, bytes: "null"},
				{typ: TTrue, line: 0, col: 5, offset: 5, bytes: "true"},
				{typ: TFalse, line: 0, col: 10, offset: 10, bytes: "false"},
			},
		},
		{
			name:  "nested structure",
			input: "a:\n  b:\n    c",
			checks: []posCheck{
				{typ: TLiteral, line: 0, col: 0, offset: 0, bytes: "a"},
				{typ: TColon, line: 0, col: 1, offset: 1, bytes: ":"},
				{typ: TIndent, line: 1, col: 0, offset: 3, bytes: "  "},
				{typ: TLiteral, line: 1, col: 2, offset: 5, bytes: "b"},
				{typ: TColon, line: 1, col: 3, offset: 6, bytes: ":"},
				{typ: TIndent, line: 2, col: 0, offset: 8, bytes: "    "},
				{typ: TLiteral, line: 2, col: 4, offset: 12, bytes: "c"},
			},
		},
		{
			name:  "array in bracketed mode",
			input: "[1, 2, 3]",
			checks: []posCheck{
				{typ: TLSquare, line: 0, col: 0, offset: 0, bytes: "["},
				{typ: TInteger, line: 0, col: 1, offset: 1, bytes: "1"},
				{typ: TComma, line: 0, col: 2, offset: 2, bytes: ","},
				{typ: TInteger, line: 0, col: 4, offset: 4, bytes: "2"},
				{typ: TComma, line: 0, col: 5, offset: 5, bytes: ","},
				{typ: TInteger, line: 0, col: 7, offset: 7, bytes: "3"},
				{typ: TRSquare, line: 0, col: 8, offset: 8, bytes: "]"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks, err := Tokenize(nil, []byte(tt.input))
			if err != nil {
				t.Fatalf("Tokenize error: %v", err)
			}

			for i, check := range tt.checks {
				if i >= len(toks) {
					t.Errorf("check %d: expected token %v but only got %d tokens", i, check.typ, len(toks))
					continue
				}
				tok := toks[i]

				if tok.Type != check.typ {
					t.Errorf("check %d: expected type %v, got %v", i, check.typ, tok.Type)
				}

				if tok.Pos == nil {
					t.Errorf("check %d: token has nil Pos", i)
					continue
				}

				if tok.Pos.I != check.offset {
					t.Errorf("check %d (%v): expected offset %d, got %d", i, check.typ, check.offset, tok.Pos.I)
				}

				line, col := tok.Pos.LineCol()
				if line != check.line {
					t.Errorf("check %d (%v): expected line %d, got %d", i, check.typ, check.line, line)
				}
				if col != check.col {
					t.Errorf("check %d (%v): expected col %d, got %d", i, check.typ, check.col, col)
				}

				if check.bytes != "" && string(tok.Bytes) != check.bytes {
					t.Errorf("check %d (%v): expected bytes %q, got %q", i, check.typ, check.bytes, string(tok.Bytes))
				}
			}
		})
	}
}

type posCheck struct {
	typ    TokenType
	line   int
	col    int
	offset int
	bytes  string // optional: verify token bytes too
}

// TestTokenPositions_Unicode verifies positions with unicode characters.
func TestTokenPositions_Unicode(t *testing.T) {
	// Unicode positions are byte-based, not rune-based
	input := "日本語: value"
	toks, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}

	// "日本語" is 9 bytes (3 chars × 3 bytes each)
	// Colon is at byte offset 9
	// "value" starts at byte offset 11 (after ": ")
	expected := []posCheck{
		{typ: TLiteral, offset: 0, line: 0, col: 0},   // 日本語
		{typ: TColon, offset: 9, line: 0, col: 9},     // :
		{typ: TLiteral, offset: 11, line: 0, col: 11}, // value
	}

	for i, check := range expected {
		if i >= len(toks) {
			t.Errorf("check %d: expected token but only got %d tokens", i, len(toks))
			continue
		}
		tok := toks[i]

		if tok.Type != check.typ {
			t.Errorf("check %d: expected type %v, got %v", i, check.typ, tok.Type)
		}

		if tok.Pos.I != check.offset {
			t.Errorf("check %d (%v): expected offset %d, got %d", i, check.typ, check.offset, tok.Pos.I)
		}

		line, col := tok.Pos.LineCol()
		if line != check.line || col != check.col {
			t.Errorf("check %d (%v): expected (%d,%d), got (%d,%d)", i, check.typ, check.line, check.col, line, col)
		}
	}
}

// TestTokenPositions_EmptyLines verifies positions with empty lines.
func TestTokenPositions_EmptyLines(t *testing.T) {
	input := "a\n\nb"
	toks, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}

	// "a" at line 0, col 0, offset 0
	// "b" at line 2, col 0, offset 3 (after "a\n\n")
	expected := []posCheck{
		{typ: TLiteral, offset: 0, line: 0, col: 0, bytes: "a"},
		{typ: TLiteral, offset: 3, line: 2, col: 0, bytes: "b"},
	}

	for i, check := range expected {
		if i >= len(toks) {
			t.Errorf("check %d: expected token but only got %d tokens", i, len(toks))
			continue
		}
		tok := toks[i]

		if tok.Pos.I != check.offset {
			t.Errorf("check %d: expected offset %d, got %d", i, check.offset, tok.Pos.I)
		}

		line, col := tok.Pos.LineCol()
		if line != check.line || col != check.col {
			t.Errorf("check %d: expected (%d,%d), got (%d,%d)", i, check.line, check.col, line, col)
		}
	}
}
