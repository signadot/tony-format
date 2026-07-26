// Self-contained reproduction for the three streaming boundary/termination
// bugs. Drop into go-tony/token/ and run:
//
//	go test ./token/ -run TestStreamingBoundaryBug -v
//
// All three fail on the tree as of the utf8-straddle fix.
package token

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func bbStream(doc string, bufSize int) ([]Token, error) {
	src := NewTokenSource(bytes.NewReader([]byte(doc)))
	src.bufferSize = bufSize
	var got []Token
	for {
		toks, err := src.Read()
		if err == io.EOF {
			return got, nil
		}
		if err != nil {
			return got, err
		}
		for _, tk := range toks {
			tk.Bytes = append([]byte(nil), tk.Bytes...)
			got = append(got, tk)
		}
	}
}

func bbTag(toks []Token) string {
	for _, tk := range toks {
		if tk.Type == TTag {
			return string(tk.Bytes)
		}
	}
	return ""
}

// Bug 1: a tag straddling a buffer refill is emitted truncated (silently) or
// fails the document when '!' is the last byte of the buffer. The scan in
// TokenizeOne's '!' case has no "ran to the buffer end, need more data" exit.
func TestStreamingBoundaryBug_Tag(t *testing.T) {
	for pad := 4080; pad < 4095; pad++ {
		doc := "a: " + strings.Repeat("x", pad) + "\nb: !mytag 1\n"
		want, err := Tokenize(nil, []byte(doc))
		if err != nil {
			t.Fatalf("pad=%d: whole-buffer: %v", pad, err)
		}
		got, err := bbStream(doc, 4096)
		if err != nil {
			t.Errorf("pad=%d: streaming: %v", pad, err)
			continue
		}
		if bbTag(got) != bbTag(want) {
			t.Errorf("pad=%d: tag: streaming %q, whole-buffer %q",
				pad, bbTag(got), bbTag(want))
		}
	}
}

// Bug 2: a block-style (line-start) quoted string in a document larger than
// the buffer fails with "multiline string". mString rewrites the inner error,
// destroying the ErrUnterminated that TokenizeOne uses to request a refill.
func TestStreamingBoundaryBug_BlockString(t *testing.T) {
	for pad := 4080; pad < 4095; pad++ {
		doc := "a: " + strings.Repeat("x", pad) + "\n\"a string value\"\n"
		if _, err := Tokenize(nil, []byte(doc)); err != nil {
			t.Fatalf("pad=%d: whole-buffer: %v", pad, err)
		}
		if _, err := bbStream(doc, 4096); err != nil {
			t.Errorf("pad=%d: streaming: %v", pad, err)
		}
	}
}

// Bug 3: a multiline literal that is the last construct in the document is
// dropped entirely, along with everything after it, at every buffer size --
// including one large enough to hold the whole document. Not a boundary bug:
// there is no terminal "no more data is coming" pass, so scanLinesStreaming
// keeps asking for data that will never arrive.
func TestStreamingBoundaryBug_MLitAtEndOfDocument(t *testing.T) {
	docs := []string{
		"a: |\n  one\n  two\n",
		"a: 1\nb: |\n  content line\n",
	}
	for _, doc := range docs {
		want, err := Tokenize(nil, []byte(doc))
		if err != nil {
			t.Fatalf("whole-buffer: %v", err)
		}
		for _, bufSize := range []int{4, 4096, 1 << 20} {
			got, err := bbStream(doc, bufSize)
			if err != nil {
				t.Errorf("buf=%d: %v", bufSize, err)
				continue
			}
			var wantMLit, gotMLit int
			for _, tk := range want {
				if tk.Type == TMLit {
					wantMLit++
				}
			}
			for _, tk := range got {
				if tk.Type == TMLit {
					gotMLit++
				}
			}
			if gotMLit != wantMLit {
				t.Errorf("buf=%d %q: got %d TMLit, want %d (streaming produced %d tokens, whole-buffer %d)",
					bufSize, doc, gotMLit, wantMLit, len(got), len(want))
			}
		}
	}
}
