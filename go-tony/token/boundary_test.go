package token

import (
	"bytes"
	"fmt"
	"io"
	"strings"
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

// TestTokenSource_TagAtBufferBoundary asserts that a tag straddling a refill is
// carried across it. The scan for a tag's end has to signal "I ran out of
// buffer" like every other scan: emitting the tag where the buffer happens to
// end cuts it in two, and because both halves are well formed (a shorter tag
// and a literal) the document decodes to something different with no error
// raised anywhere.
func TestTokenSource_TagAtBufferBoundary(t *testing.T) {
	const bufSize = 4096
	// Sweep the tag across the refill boundary a byte at a time.
	for pad := bufSize - 16; pad < bufSize; pad++ {
		doc := "a: " + strings.Repeat("x", pad) + "\nb: !mytag 1\n"
		want, err := Tokenize(nil, []byte(doc))
		if err != nil {
			t.Fatalf("pad %d: whole-buffer Tokenize: %v", pad, err)
		}
		got := mustStream(t, doc, bufSize)
		if g, w := firstTag(got), firstTag(want); g != w {
			t.Errorf("pad %d: tag: streaming %q, whole-buffer %q", pad, g, w)
		}
	}
}

// TestTokenSource_BlockStringInLargeDocument asserts that a quoted string at the
// start of a line survives in a document larger than the buffer. That string
// goes through mString, which used to rewrite the scan's error and so destroy
// the ErrUnterminated the tokenizer tests for when deciding to refill.
func TestTokenSource_BlockStringInLargeDocument(t *testing.T) {
	const bufSize = 4096
	for pad := bufSize - 16; pad < bufSize; pad++ {
		doc := "a: " + strings.Repeat("x", pad) + "\n\"a string value\"\nb: 1\n"
		want, err := Tokenize(nil, []byte(doc))
		if err != nil {
			t.Fatalf("pad %d: whole-buffer Tokenize: %v", pad, err)
		}
		got := mustStream(t, doc, bufSize)
		if g, w := countType(got, TString), countType(want, TString); g != w {
			t.Errorf("pad %d: got %d TString, want %d", pad, g, w)
		}
	}
}

// TestTokenSource_MLitAtEndOfDocument asserts that a multiline literal which is
// the last construct in a document is emitted. A multiline literal ends where
// its indentation ends, so the scan asks for the bytes after it — and at the
// end of a document those never come. Without a terminal pass the token, and
// everything after it, was dropped with no error, at every buffer size
// including one holding the whole document.
func TestTokenSource_MLitAtEndOfDocument(t *testing.T) {
	docs := []string{
		"a: |\n  one\n  two\n",
		"a: 1\nb: |\n  content line\n",
		"a: |\n  one\nb: 2\n", // literal mid-document: the case that always worked
	}
	for _, doc := range docs {
		want, err := Tokenize(nil, []byte(doc))
		if err != nil {
			t.Fatalf("whole-buffer Tokenize %q: %v", doc, err)
		}
		for _, bufSize := range []int{1, 4, 16, 4096, 1 << 20} {
			got := mustStream(t, doc, bufSize)
			g, w := trimTrailingIndent(got), trimTrailingIndent(want)
			if len(g) != len(w) {
				t.Errorf("%q buf %d: token count: got %d, want %d\ngot:  %s\nwant: %s",
					doc, bufSize, len(g), len(w), tokDump(g), tokDump(w))
				continue
			}
			// Compare decoded values, not raw bytes: Tokenize appends a newline
			// to its input unconditionally while the stream supplies one only
			// when the document lacks it, so a multiline literal at the end of
			// the document carries one more raw newline in the whole-buffer
			// token. The value it decodes to is the same either way.
			for i := range w {
				if g[i].Type != w[i].Type || g[i].String() != w[i].String() {
					t.Errorf("%q buf %d: token %d: got %s:%q, want %s:%q",
						doc, bufSize, i, g[i].Type, g[i].String(), w[i].Type, w[i].String())
				}
			}
		}
	}
}

func firstTag(toks []Token) string {
	for _, tk := range toks {
		if tk.Type == TTag {
			return string(tk.Bytes)
		}
	}
	return ""
}

func countType(toks []Token, tt TokenType) int {
	n := 0
	for _, tk := range toks {
		if tk.Type == tt {
			n++
		}
	}
	return n
}
