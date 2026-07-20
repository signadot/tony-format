package token

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

// TestTokenSource_NoTokenSplitAtBufferBoundary asserts that streaming
// tokenization produces exactly the same tokens as whole-buffer tokenization for
// every token type that reads up to a delimiter — integers, negative integers,
// floats, the null/true/false keywords, and plain literals (including ':' and '.'
// which getSingleLiteral keeps/trims). At small buffer sizes every value straddles
// a read boundary, so a token that fails to signal "need more data" would split.
func TestTokenSource_NoTokenSplitAtBufferBoundary(t *testing.T) {
	doc := "a: 10000000005\n" + // large integer
		"b: -420\n" + // negative integer ('-' can land at a boundary)
		"c: 3.14159\n" + // float
		"d: true\n" +
		"e: false\n" +
		"f: null\n" +
		"g: hello_world\n" + // plain literal
		"h: a.b.c\n" + // literal with dots
		"i: http://x/y\n" + // literal with ':' (chop edge)
		"j: nullable\n" + // keyword prefix but a longer literal
		"k: trueish\n"

	want, err := Tokenize(nil, []byte(doc))
	if err != nil {
		t.Fatalf("whole-buffer Tokenize: %v", err)
	}

	for _, bufSize := range []int{1, 2, 3, 4, 5, 7, 8, 16, 64, 256} {
		t.Run(fmt.Sprintf("buf%d", bufSize), func(t *testing.T) {
			src := NewTokenSource(bytes.NewReader([]byte(doc)))
			src.bufferSize = bufSize
			var got []Token
			for {
				toks, err := src.Read()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("stream Read (buf %d): %v", bufSize, err)
				}
				for _, tk := range toks {
					tk.Bytes = append([]byte(nil), tk.Bytes...)
					got = append(got, tk)
				}
			}
			// Streaming can emit an extra zero-width trailing TIndent at EOF (a
			// benign whitespace artifact, not a split); normalize both.
			g, w := trimTrailingIndent(got), trimTrailingIndent(want)
			if len(g) != len(w) {
				t.Fatalf("token count: got %d, want %d", len(g), len(w))
			}
			for i := range w {
				if g[i].Type != w[i].Type || string(g[i].Bytes) != string(w[i].Bytes) {
					t.Errorf("token %d (buf %d): got %s:%q, want %s:%q",
						i, bufSize, g[i].Type, g[i].Bytes, w[i].Type, w[i].Bytes)
				}
			}
		})
	}
}

func trimTrailingIndent(toks []Token) []Token {
	for len(toks) > 0 && toks[len(toks)-1].Type == TIndent {
		toks = toks[:len(toks)-1]
	}
	return toks
}
