package token

import (
	"fmt"
	"testing"
	"github.com/signadot/tony-format/go-tony/format"
)

func TestTokenize(t *testing.T) {
	s := `-1-2
- 1
- 2
# comment
# comment trailing
  # comment
  # comment trailing 
  "string"
  "nl\n"
  "\""
  "\\"
  !abc
  !de(f)
  null
  true
  false
  nully
  ''
  ""
  "\u1234"
  "\b\t\r\n\f"
  #"\'" bad escape
  #'\"'
  'z'
  '\''
  "\""
ȡ
{ ∞: 2, x: 0 }
{ ∞∞: -14 }
✓
  https://hello.world/1/2/3
  -3
  -0
  0.01
  0e21
  0E-2
0e+1
|
  a
  b
  c
  d
    e f  
  h
2
|-
  a
  b
  c
false

f: a

  # comment
  # comment
  b: c # after
  c:
  - 1
  - -0e24 # valid json number
|
  a
a,b

-423
--ad0dkd
|
  > 
   had
   dkdk
f[0]
<<: |
  a
spec: {
containers: [{
command: [
  "/app/io-context-server",
  "-tls=secretns=signadot",
  "-port=8443",
],
}]}
{a:b}
`
	toks, err := Tokenize(nil, []byte(s))
	if err != nil {
		t.Error(err)
		return
	}
	for i := range toks {
		fmt.Println(toks[i].Info())
		switch toks[i].Type {
		case TString, TMLit, TLiteral:
			fmt.Printf("`%s`\n---\n%q\n", string(toks[i].Bytes), toks[i].String())
		case TInteger:
			fmt.Printf("%s\n", string(toks[i].Bytes))
		}
	}
}

func TestYQTokenize(t *testing.T) {
	//	s := `
	//
	// ""
	// " "
	// "\""
	// "\n"
	// "a
	// b"
	//
	// "\
	// 1\
	// 2\
	// 3\""
	//
	// ”
	// '
	// adld
	//
	//	\n '
	//
	// ””
	s := `
!and
- a: !glob '*'
- c: d
`
	toks, err := Tokenize(nil, []byte(s), TokenYAML())
	if err != nil {
		t.Errorf("%s: %v", s, err)
		return
	}
	for i := range toks {
		fmt.Println(toks[i].Info())
		switch toks[i].Type {
		case TString, TMLit, TLiteral:
			fmt.Printf("`%s`\n---\n%q\n", string(toks[i].Bytes), toks[i].String())
		case TInteger:
			fmt.Printf("%s\n", string(toks[i].Bytes))
		}
	}
}

func TestTraceNull(t *testing.T) {
	input := `{ "null": null }`
	
	fmt.Printf("Input: %q\n\n", input)
	fmt.Println("Tokenization trace:")
	fmt.Println("==================================================")
	
	toks, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Errorf("Error: %v", err)
		return
	}
	
	fmt.Printf("\nTotal tokens: %d\n\n", len(toks))
	
	for i, t := range toks {
		fmt.Printf("Token %d:\n", i+1)
		fmt.Printf("  Type:  %s\n", t.Type.String())
		fmt.Printf("  Bytes: %q\n", string(t.Bytes))
		fmt.Printf("  Pos:   %s\n", t.Pos.String())
		if t.Type == TString {
			fmt.Printf("  Value: %q\n", t.String())
		} else if t.Type == TNull {
			fmt.Printf("  Value: null\n")
		}
		fmt.Println()
	}
	
	fmt.Println("==================================================")
	fmt.Println("\nCompact format:")
	PrintTokens(toks, "Trace")
	
	fmt.Println("\n==================================================")
	fmt.Println("Testing Balance():")
	balanced, err := Balance(toks, format.TonyFormat)
	if err != nil {
		fmt.Printf("❌ Does NOT balance: %v\n", err)
	} else {
		fmt.Printf("✅ Balances successfully!\n")
		fmt.Printf("Balanced tokens: %d\n", len(balanced))
		PrintTokens(balanced, "Balanced")
	}
}

func TestTraceNullable(t *testing.T) {
	input := `nullable(t)`
	
	fmt.Printf("Input: %q\n\n", input)
	fmt.Println("Tokenization trace:")
	fmt.Println("==================================================")
	
	toks, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Errorf("Error: %v", err)
		return
	}
	
	fmt.Printf("\nTotal tokens: %d\n\n", len(toks))
	
	for i, t := range toks {
		fmt.Printf("Token %d:\n", i+1)
		fmt.Printf("  Type:  %s\n", t.Type.String())
		fmt.Printf("  Bytes: %q\n", string(t.Bytes))
		fmt.Printf("  Pos:   %s\n", t.Pos.String())
		if t.Type == TString {
			fmt.Printf("  Value: %q\n", t.String())
		} else if t.Type == TTag {
			fmt.Printf("  Tag:   %s\n", string(t.Bytes))
		} else if t.Type == TLiteral {
			fmt.Printf("  Literal: %s\n", string(t.Bytes))
		}
		fmt.Println()
	}
	
	fmt.Println("==================================================")
	fmt.Println("\nCompact format:")
	PrintTokens(toks, "Trace")
}

func TestKeywordPrefixBug(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []struct {
			typ   TokenType
			bytes string
		}
	}{
		{
			name:  "nullable should be literal",
			input: `nullable(t)`,
			expected: []struct {
				typ   TokenType
				bytes string
			}{
				{TLiteral, "nullable(t)"},
			},
		},
		{
			name:  "null should be keyword",
			input: `null`,
			expected: []struct {
				typ   TokenType
				bytes string
			}{
				{TNull, "null"},
			},
		},
		{
			name:  "nullify should be literal",
			input: `nullify`,
			expected: []struct {
				typ   TokenType
				bytes string
			}{
				{TLiteral, "nullify"},
			},
		},
		{
			name:  "truest should be literal",
			input: `truest`,
			expected: []struct {
				typ   TokenType
				bytes string
			}{
				{TLiteral, "truest"},
			},
		},
		{
			name:  "true should be keyword",
			input: `true`,
			expected: []struct {
				typ   TokenType
				bytes string
			}{
				{TTrue, "true"},
			},
		},
		{
			name:  "falsely should be literal",
			input: `falsely`,
			expected: []struct {
				typ   TokenType
				bytes string
			}{
				{TLiteral, "falsely"},
			},
		},
		{
			name:  "false should be keyword",
			input: `false`,
			expected: []struct {
				typ   TokenType
				bytes string
			}{
				{TFalse, "false"},
			},
		},
		{
			name:  "null with bracket",
			input: `null]`,
			expected: []struct {
				typ   TokenType
				bytes string
			}{
				{TNull, "null"},
				{TRSquare, "]"},
			},
		},
		{
			name:  "null with brace",
			input: `null}`,
			expected: []struct {
				typ   TokenType
				bytes string
			}{
				{TNull, "null"},
				{TRCurl, "}"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks, err := Tokenize(nil, []byte(tt.input))
			if err != nil {
				t.Errorf("Error tokenizing %q: %v", tt.input, err)
				return
			}
			
			// Filter out TIndent tokens for comparison
			var filtered []Token
			for i := range toks {
				if toks[i].Type != TIndent {
					filtered = append(filtered, toks[i])
				}
			}
			
			if len(filtered) != len(tt.expected) {
				t.Errorf("Expected %d tokens, got %d", len(tt.expected), len(filtered))
				for i, tok := range filtered {
					t.Logf("  Token %d: %s %q", i, tok.Type, string(tok.Bytes))
				}
				return
			}
			
			for i, exp := range tt.expected {
				if filtered[i].Type != exp.typ {
					t.Errorf("Token %d: expected type %s, got %s", i, exp.typ, filtered[i].Type)
				}
				if string(filtered[i].Bytes) != exp.bytes {
					t.Errorf("Token %d: expected bytes %q, got %q", i, exp.bytes, string(filtered[i].Bytes))
				}
			}
		})
	}
}

func TestCommentParsingIssue(t *testing.T) {
	input := `a: b
c:
  d:
  - 1
  - 2
# comment 1
# comment 2
  # comment 3
  f: g`
	
	fmt.Printf("Input:\n%s\n\n", input)
	fmt.Println("Tokenization trace:")
	fmt.Println("==================================================")
	
	toks, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Errorf("Error tokenizing: %v", err)
		return
	}
	
	fmt.Printf("\nTotal tokens: %d\n\n", len(toks))
	
	for i, t := range toks {
		fmt.Printf("Token %d:\n", i+1)
		fmt.Printf("  Type:  %s\n", t.Type.String())
		fmt.Printf("  Bytes: %q\n", string(t.Bytes))
		if t.Pos != nil {
			fmt.Printf("  Pos:   %s\n", t.Pos.String())
		}
		if t.Type == TComment {
			fmt.Printf("  Comment content: %q\n", string(t.Bytes))
		}
		fmt.Println()
	}
	
	fmt.Println("==================================================")
	fmt.Println("\nCompact format:")
	PrintTokens(toks, "Trace")
	
	fmt.Println("\n==================================================")
	fmt.Println("Testing Balance():")
	balanced, err := Balance(toks, format.TonyFormat)
	if err != nil {
		fmt.Printf("❌ Does NOT balance: %v\n", err)
	} else {
		fmt.Printf("✅ Balances successfully!\n")
		fmt.Printf("Balanced tokens: %d\n", len(balanced))
		PrintTokens(balanced, "Balanced")
	}
}
