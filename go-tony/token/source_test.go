package token

import (
	"bytes"
	"io"
	"testing"
)

func TestTokenSource_Basic(t *testing.T) {
	input := "key: value\n"
	reader := bytes.NewReader([]byte(input))
	source := NewTokenSource(reader)

	var allTokens []Token
	for {
		tokens, err := source.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read error: %v", err)
		}
		allTokens = append(allTokens, tokens...)
	}

	// Compare with Tokenize
	expected, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}

	if len(allTokens) != len(expected) {
		t.Fatalf("Token count mismatch: got %d, expected %d", len(allTokens), len(expected))
	}

	for i := range allTokens {
		if allTokens[i].Type != expected[i].Type {
			t.Errorf("Token %d type mismatch: got %v, expected %v", i, allTokens[i].Type, expected[i].Type)
		}
		if !bytes.Equal(allTokens[i].Bytes, expected[i].Bytes) {
			t.Errorf("Token %d bytes mismatch: got %q, expected %q", i, allTokens[i].Bytes, expected[i].Bytes)
		}
	}
}

func TestTokenSource_WithIndent(t *testing.T) {
	input := "  key: value\n"
	reader := bytes.NewReader([]byte(input))
	source := NewTokenSource(reader)

	var allTokens []Token
	for {
		tokens, err := source.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read error: %v", err)
		}
		allTokens = append(allTokens, tokens...)
	}

	// Compare with Tokenize
	expected, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}

	if len(allTokens) != len(expected) {
		t.Fatalf("Token count mismatch: got %d, expected %d", len(allTokens), len(expected))
	}

	for i := range allTokens {
		if allTokens[i].Type != expected[i].Type {
			t.Errorf("Token %d type mismatch: got %v, expected %v", i, allTokens[i].Type, expected[i].Type)
		}
		if !bytes.Equal(allTokens[i].Bytes, expected[i].Bytes) {
			t.Errorf("Token %d bytes mismatch: got %q, expected %q", i, allTokens[i].Bytes, expected[i].Bytes)
		}
	}
}

func TestTokenSource_MultilineFolding(t *testing.T) {
	// Test multiline folding - according to Tony spec:
	// "Multiline quoting is permitted for any string whose opening quotation
	// character is the first non whitespace character of the line in which it occurs."
	// "Multiline capable strings may be folded, which can be convenient for entering
	// very long lines in a readable and editable fashion"
	// Multiple multiline capable strings on consecutive indented lines get concatenated.
	// This matches testdata/mls.tony exactly
	input := "key:\n  \"hello\\nworld\"\n  \"...see ya...\\n\"\n"
	reader := bytes.NewReader([]byte(input))
	source := NewTokenSource(reader)

	var allTokens []Token
	for {
		tokens, err := source.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// This used to be logged and broken out of, on the theory that a multiline
			// string spanning a buffer boundary was expected to fail. It is not
			// expected: TokenSource refills across boundaries, and
			// TestMultilineStringAcrossAReadBoundary holds it to that.
			t.Fatalf("read: %v", err)
		}
		allTokens = append(allTokens, tokens...)
	}

	// Compare with Tokenize (if it works)
	// The comparison this test exists to make. It used to be SKIPPED when Tokenize
	// failed, calling that a pre-existing bug -- and the bug was fixed, leaving a skip
	// which now only means "swallow the next regression quietly".
	expected, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}

	if len(allTokens) != len(expected) {
		t.Fatalf("Token count mismatch: got %d, expected %d", len(allTokens), len(expected))
	}

	for i := range allTokens {
		if allTokens[i].Type != expected[i].Type {
			t.Errorf("Token %d type mismatch: got %v, expected %v", i, allTokens[i].Type, expected[i].Type)
		}
		if !bytes.Equal(allTokens[i].Bytes, expected[i].Bytes) {
			t.Errorf("Token %d bytes mismatch: got %q, expected %q", i, allTokens[i].Bytes, expected[i].Bytes)
		}
	}
}

func TestTokenSource_NoTrailingNewline(t *testing.T) {
	// Input without trailing newline - TokenSource should add one
	input := "key: value"
	reader := bytes.NewReader([]byte(input))
	source := NewTokenSource(reader)

	var allTokens []Token
	for {
		tokens, err := source.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read error: %v", err)
		}
		allTokens = append(allTokens, tokens...)
	}

	// Compare with Tokenize (which adds trailing newline)
	expected, err := Tokenize(nil, []byte(input))
	if err != nil {
		t.Fatalf("Tokenize error: %v", err)
	}

	if len(allTokens) != len(expected) {
		t.Fatalf("Token count mismatch: got %d, expected %d", len(allTokens), len(expected))
	}

	for i := range allTokens {
		if allTokens[i].Type != expected[i].Type {
			t.Errorf("Token %d type mismatch: got %v, expected %v", i, allTokens[i].Type, expected[i].Type)
		}
		if !bytes.Equal(allTokens[i].Bytes, expected[i].Bytes) {
			t.Errorf("Token %d bytes mismatch: got %q, expected %q", i, allTokens[i].Bytes, expected[i].Bytes)
		}
	}
}
