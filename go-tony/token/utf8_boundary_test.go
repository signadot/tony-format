package token

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestTokenSource_MultiByteRuneAtBufferBoundary asserts that a multi-byte UTF-8
// rune straddling a buffer refill is carried across it rather than decoded in
// place. utf8.DecodeRune reports a truncated sequence as (RuneError, 1), exactly
// as it reports an invalid byte, so a scanner that does not distinguish the two
// fails the whole document with "bad utf8" as soon as a refill lands mid-rune.
//
// Every buffer size is exercised so that each rune in the document straddles a
// boundary at some size; the streamed tokens must match whole-buffer
// tokenization in all of them.
func TestTokenSource_MultiByteRuneAtBufferBoundary(t *testing.T) {
	doc := "a: \"em—dash → check ✓\"\n" + // quoted string
		"b: héllo→wörld\n" + // plain literal
		"c: 'ünïcode'\n" + // single-quoted string
		"# comment — with → runes\n" + // comment
		"d: nullé\n" + // keyword prefix followed by a multi-byte rune
		"e: trueé\n" +
		"f: falseé\n" +
		"g: \"\\u00e9 escaped — mixed\"\n"

	want, err := Tokenize(nil, []byte(doc))
	if err != nil {
		t.Fatalf("whole-buffer Tokenize: %v", err)
	}

	for bufSize := 1; bufSize <= 40; bufSize++ {
		t.Run(fmt.Sprintf("buf%d", bufSize), func(t *testing.T) {
			got := mustStream(t, doc, bufSize)
			g, w := trimTrailingIndent(got), trimTrailingIndent(want)
			if len(g) != len(w) {
				t.Fatalf("token count: got %d, want %d\ngot: %s", len(g), len(w), tokDump(g))
			}
			for i := range w {
				if g[i].Type != w[i].Type || string(g[i].Bytes) != string(w[i].Bytes) {
					t.Errorf("token %d: got %s:%q, want %s:%q",
						i, g[i].Type, g[i].Bytes, w[i].Type, w[i].Bytes)
				}
			}
		})
	}
}

// TestTokenSource_LargeMultiByteDocument covers the shape the bug was reported
// against: a single string value long enough to span several refills of the
// default buffer, with multi-byte runes throughout. Lengths are swept a byte at
// a time so the refill lands at every offset within a rune.
func TestTokenSource_LargeMultiByteDocument(t *testing.T) {
	unit := "em-dash — arrow → check ✓ "
	for pad := 0; pad < 8; pad++ {
		body := strings.Repeat("x", pad) + strings.Repeat(unit, 400)
		doc := "a: \"" + body + "\"\n"
		got := mustStream(t, doc, defaultBufferSize)
		var val string
		for _, tk := range got {
			if tk.Type == TString {
				val = tk.String()
			}
		}
		if val != body {
			t.Fatalf("pad %d: string value truncated or corrupted: got %d bytes, want %d",
				pad, len(val), len(body))
		}
	}
}

// TestBadUTF8ReportedAtOffendingByte asserts that genuinely invalid UTF-8 is
// still rejected, and that the reported position is the bad byte rather than
// the start of the token containing it.
func TestBadUTF8ReportedAtOffendingByte(t *testing.T) {
	// 0xff is not a valid start byte and cannot be the prefix of any sequence.
	doc := []byte("a: \"0123456789\xff\"\n")
	bad := bytes.IndexByte(doc, 0xff)

	if _, err := Tokenize(nil, doc); err == nil {
		t.Fatal("whole-buffer Tokenize: want bad utf8 error, got nil")
	} else {
		te, ok := err.(*TokenizeErr)
		if !ok {
			t.Fatalf("want *TokenizeErr, got %T: %v", err, err)
		}
		if te.Pos.I != bad {
			t.Errorf("position: got offset %d, want %d (the 0xff byte)", te.Pos.I, bad)
		}
	}

	// The same input arriving one byte at a time must still fail: a truncated
	// sequence at the real end of the document is not a refill situation.
	for bufSize := 1; bufSize <= 8; bufSize++ {
		src := NewTokenSource(bytes.NewReader(doc))
		src.bufferSize = bufSize
		var err error
		for err == nil {
			_, err = src.Read()
		}
		if err == io.EOF {
			t.Errorf("buf %d: want bad utf8 error, got clean EOF", bufSize)
		}
	}
}

// TestTruncatedRuneAtEOF asserts that a sequence cut off by the end of the
// document — not by a buffer boundary — is reported rather than silently
// accepted or swallowed as EOF.
func TestTruncatedRuneAtEOF(t *testing.T) {
	// A lone leading byte of a 3-byte sequence, at the end of the value.
	doc := []byte("a: \"abc\xe2\x80\"\n")
	if _, err := Tokenize(nil, doc); err == nil {
		t.Error("whole-buffer Tokenize: want error, got nil")
	}
	for bufSize := 1; bufSize <= 8; bufSize++ {
		src := NewTokenSource(bytes.NewReader(doc))
		src.bufferSize = bufSize
		var err error
		for err == nil {
			_, err = src.Read()
		}
		if err == io.EOF {
			t.Errorf("buf %d: want error, got clean EOF", bufSize)
		}
	}
}

func TestPartialRune(t *testing.T) {
	for _, c := range []struct {
		d    string
		want bool
	}{
		{"", false},
		{"a", false},
		{"—", false},
		{"—x", false},
		{"\xe2", true},     // first byte of a 3-byte sequence
		{"\xe2\x80", true}, // first two bytes of a 3-byte sequence
		{"\xe2\x80\x94", false},
		{"\xff", false},        // not a valid start byte: invalid, not partial
		{"\xe2\x80x", false},   // continuation broken: invalid, not partial
		{"\xf0\x9f\x98", true}, // 4-byte sequence missing its last byte
		{string(utf8.RuneError), false},
	} {
		if got := partialRune([]byte(c.d)); got != c.want {
			t.Errorf("partialRune(%q) = %t, want %t", c.d, got, c.want)
		}
	}
}

func mustStream(t *testing.T, doc string, bufSize int) []Token {
	t.Helper()
	src := NewTokenSource(bytes.NewReader([]byte(doc)))
	src.bufferSize = bufSize
	var got []Token
	for {
		toks, err := src.Read()
		if err == io.EOF {
			return got
		}
		if err != nil {
			t.Fatalf("stream Read (buf %d): %v", bufSize, err)
		}
		for _, tk := range toks {
			tk.Bytes = append([]byte(nil), tk.Bytes...)
			got = append(got, tk)
		}
	}
}

func tokDump(toks []Token) string {
	b := &strings.Builder{}
	for _, tk := range toks {
		fmt.Fprintf(b, "%s:%q ", tk.Type, tk.Bytes)
	}
	return b.String()
}
