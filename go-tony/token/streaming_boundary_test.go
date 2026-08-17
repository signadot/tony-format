package token

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A reader hands out what it has, and a connection has whatever arrived.  Every
// scanner therefore meets its construct cut in half, and the question at that
// point is "may more come?" -- not "is this valid?".  Answering the second
// question there is one defect with several instances, each of them found by
// TestEverySplitReadsLikeTheWholeDocument below (75g1kbpdh12krs09gdn0).

// boundaryReader hands out at most n bytes per Read, the way a connection does.
type boundaryReader struct {
	d []byte
	n int
}

func (c *boundaryReader) Read(p []byte) (int, error) {
	if len(c.d) == 0 {
		return 0, io.EOF
	}
	n := min(min(c.n, len(c.d)), len(p))
	copy(p, c.d[:n])
	c.d = c.d[n:]
	return n, nil
}

// tokensOverReads tokenizes d from a reader which delivers it n bytes at a time.
func tokensOverReads(t *testing.T, d []byte, n int, opts ...TokenOpt) (toks []Token, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%d-byte reads: panic: %v", n, r)
		}
	}()
	src := NewTokenSource(&boundaryReader{d: append([]byte{}, d...), n: n}, opts...)
	for {
		got, err := src.Read()
		toks = append(toks, got...)
		if err == io.EOF {
			return toks, nil
		}
		if err != nil {
			return toks, err
		}
	}
}

// shape renders type and bytes, which is what a reader of the document sees; the
// positions differ between the two paths by design.
func shape(toks []Token) string {
	var b strings.Builder
	for _, tok := range toks {
		b.WriteString(tok.Type.String())
		b.WriteByte('(')
		b.Write(tok.Bytes)
		b.WriteString(") ")
	}
	return b.String()
}

// A document separator is `---` and the newline which ends it.  The guard
// established only that the third dash was in the buffer and then read a fourth
// byte: 4-byte reads PANICKED with a slice out of range, and 3-byte reads came
// back with `---` as a literal, silently merging two documents into one.
func TestDocSeparatorAcrossAReadBoundary(t *testing.T) {
	const doc = "a: 1\n---\nb: 2\n"
	want, err := Tokenize(nil, []byte(doc))
	if err != nil {
		t.Fatalf("whole: %s", err)
	}
	for n := 1; n <= len(doc)+2; n++ {
		got, err := tokensOverReads(t, []byte(doc), n)
		if err != nil {
			t.Errorf("%d-byte reads: %s", n, err)
			continue
		}
		if shape(got) != shape(want) {
			t.Errorf("%d-byte reads:\n got %s\nwant %s", n, shape(got), shape(want))
		}
	}
}

// A merge key is `<<`, and a buffer ending between the two is not an
// unterminated one.
func TestMergeKeyAcrossAReadBoundary(t *testing.T) {
	const doc = "a:\n  <<: |\n    x\n  b: 1\n"
	want, err := Tokenize(nil, []byte(doc))
	if err != nil {
		t.Fatalf("whole: %s", err)
	}
	for n := 1; n <= len(doc)+2; n++ {
		got, err := tokensOverReads(t, []byte(doc), n)
		if err != nil {
			t.Errorf("%d-byte reads: %s", n, err)
			continue
		}
		if shape(got) != shape(want) {
			t.Errorf("%d-byte reads:\n got %s\nwant %s", n, shape(got), shape(want))
		}
	}
}

// A block-style string is the quoted strings at one indent, taken together.
// Whether another follows is a question about the bytes after the one just read,
// and answering "no" when the buffer ended there split one string into two --
// two values where the document has one, with no error.
func TestMultilineStringAcrossAReadBoundary(t *testing.T) {
	const doc = "key:\n  \"hello\"\n  \"world\"\n"
	want, err := Tokenize(nil, []byte(doc))
	if err != nil {
		t.Fatalf("whole: %s", err)
	}
	if !strings.Contains(shape(want), "TMString") {
		t.Fatalf("the document does not hold a multiline string: %s", shape(want))
	}
	for n := 1; n <= len(doc)+2; n++ {
		got, err := tokensOverReads(t, []byte(doc), n)
		if err != nil {
			t.Errorf("%d-byte reads: %s", n, err)
			continue
		}
		if shape(got) != shape(want) {
			t.Errorf("%d-byte reads:\n got %s\nwant %s", n, shape(got), shape(want))
		}
	}
}

// The indent is the structure, so an indent run reaching the buffer end may not
// be the whole indent: reported short, the line is at the wrong depth.
func TestIndentAcrossAReadBoundary(t *testing.T) {
	const doc = "a:\n        b:\n                c: 1\n"
	want, err := Tokenize(nil, []byte(doc))
	if err != nil {
		t.Fatalf("whole: %s", err)
	}
	for n := 1; n <= len(doc)+2; n++ {
		got, err := tokensOverReads(t, []byte(doc), n)
		if err != nil {
			t.Errorf("%d-byte reads: %s", n, err)
			continue
		}
		if shape(got) != shape(want) {
			t.Errorf("%d-byte reads:\n got %s\nwant %s", n, shape(got), shape(want))
		}
	}
}

// An unterminated double-quoted YAML string was returned as a complete one: the
// closing quote is the only way out of that scan, so falling out of it is the
// missing quote.  `a: "no closing quote` was accepted with the rest of the line as
// its value, in a whole document as much as in a stream.
func TestUnterminatedQuotedYAMLIsAnError(t *testing.T) {
	for _, doc := range []string{
		"a: \"unterminated\n", "a: \"unterminated",
		"a: 'unterminated\n", "a: 'unterminated",
	} {
		if _, err := Tokenize(nil, []byte(doc), TokenYAML()); err == nil {
			t.Errorf("%q: accepted, with no closing quote", doc)
		}
	}
	for _, doc := range []string{"a: \"ok\"\n", "a: 'ok'\n"} {
		if _, err := Tokenize(nil, []byte(doc), TokenYAML()); err != nil {
			t.Errorf("%q: %s", doc, err)
		}
	}
}

// The net: every document in the tree, read at every size a read might come in,
// has to tokenize to what the whole document tokenizes to.  This is the sweep that
// turned up the instances above -- and, in YAML, plain scalars chopped at the
// boundary (`string` read as `str`), a folded scalar cut short at a continuation
// line, and `:9091` split into a colon and a scalar.  A scanner added later gets
// the same treatment for free.
func TestEverySplitReadsLikeTheWholeDocument(t *testing.T) {
	docs := map[string][]byte{
		"a literal holding a bracket": []byte("k: " + strings.Repeat("a", 4090) + "{x}b\n"),
		"a separator":                 []byte("a: 1\n---\nb: 2\n"),
		"a merge key":                 []byte("a:\n  <<: |\n    x\n  b: 1\n"),
		"a block string":              []byte("k:\n  \"a\"\n  \"b\"\n"),
		"a kept literal":              []byte("k: |+\n  x\n\n"),
		"an empty literal":            []byte("k: |\n"),
	}
	sizes := []int{1, 2, 3, 5, 7, 11, 16, 64, 256, 1000, 4095, 4096, 4097}
	for name, d := range docs {
		want, err := Tokenize(nil, d)
		if err != nil {
			t.Fatalf("%s: whole: %s", name, err)
		}
		for _, n := range sizes {
			got, err := tokensOverReads(t, d, n)
			if err != nil {
				t.Errorf("%s in %d-byte reads: %s", name, n, err)
				continue
			}
			if shape(got) != shape(want) {
				t.Errorf("%s in %d-byte reads differs from the whole document", name, n)
			}
		}
	}

	// and the tree's own documents, in each format they are written in, which are
	// the shapes people actually write
	for _, fmt := range []struct {
		ext string
		opt TokenOpt
	}{{".tony", TokenTony()}, {".yaml", TokenYAML()}, {".json", TokenJSON()}} {
		for name, d := range treeDocuments(fmt.ext) {
			want, err := Tokenize(nil, d, fmt.opt)
			if err != nil {
				continue // not a document this can compare against
			}
			for _, n := range readSizesFor(len(d)) {
				got, err := tokensOverReads(t, d, n, fmt.opt)
				if err != nil {
					t.Errorf("%s in %d-byte reads: %s", name, n, err)
					break
				}
				if shape(got) != shape(want) {
					t.Errorf("%s in %d-byte reads differs from the whole document", name, n)
					break
				}
			}
		}
	}
}

// Every split POSITION, not just every size: two reads, cut at each byte in turn.
// Only for the documents small enough to do that in a moment; the sizes above
// cover the rest.
func TestEverySplitPositionReadsLikeTheWholeDocument(t *testing.T) {
	for _, fmt := range []struct {
		ext string
		opt TokenOpt
	}{{".tony", TokenTony()}, {".yaml", TokenYAML()}} {
		for name, d := range treeDocuments(fmt.ext) {
			if len(d) > 2048 {
				continue // the read sizes above cover the larger ones
			}
			want, err := Tokenize(nil, d, fmt.opt)
			if err != nil {
				continue
			}
			for at := 1; at < len(d); at++ {
				got, err := tokensAtSplit(t, d, at, fmt.opt)
				if err != nil {
					t.Errorf("%s split at %d: %s", name, at, err)
					break
				}
				if shape(got) != shape(want) {
					t.Errorf("%s split at %d differs from the whole document", name, at)
					break
				}
			}
		}
	}
}

// readSizesFor keeps the sweep quick: a one-byte read says everything it has to
// say on a small document, and saying it again over a megabyte adds only minutes.
func readSizesFor(size int) []int {
	if size > 1<<16 {
		return []int{4095, 4096, 4097}
	}
	return []int{1, 2, 3, 5, 7, 11, 16, 64, 256, 1000, 4095, 4096, 4097}
}

// treeDocuments answers the documents of one extension in the repository, for the
// sweeps to read every way a reader might deliver them.
func treeDocuments(ext string) map[string][]byte {
	out := map[string][]byte{}
	filepath.Walk("../", func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ext) {
			return nil
		}
		if d, err := os.ReadFile(p); err == nil {
			out[p] = d
		}
		return nil
	})
	return out
}

// chunksReader delivers a fixed list of chunks, then io.EOF.
type chunksReader struct{ chunks [][]byte }

func (r *chunksReader) Read(p []byte) (int, error) {
	for len(r.chunks) > 0 && len(r.chunks[0]) == 0 {
		r.chunks = r.chunks[1:]
	}
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[0])
	r.chunks[0] = r.chunks[0][n:]
	return n, nil
}

func tokensAtSplit(t *testing.T, d []byte, at int, opts ...TokenOpt) (toks []Token, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("split at %d: panic: %v", at, r)
		}
	}()
	src := NewTokenSource(&chunksReader{chunks: [][]byte{d[:at], d[at:]}}, opts...)
	for {
		got, e := src.Read()
		toks = append(toks, got...)
		if e == io.EOF {
			return toks, nil
		}
		if e != nil {
			return toks, e
		}
	}
}
