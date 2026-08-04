package token

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestDigitLeading covers the scalars that begin with a digit and do not finish as a
// number. Before ErrDigitLeading the number scanner took the leading digits and left the
// rest as its own token, so these failed later as two adjacent values, with a message
// naming neither the scalar nor the cause.
func TestDigitLeading(t *testing.T) {
	for _, c := range []struct {
		doc  string
		want string // the scalar the error must name
	}{
		// durations and quantities, the shapes that motivated this
		{"k: 30s\n", "30s"},
		{"k: 100m\n", "100m"},
		{"k: 1Gi\n", "1Gi"},
		{"k: 1h30m\n", "1h30m"},
		{"k: 3d\n", "3d"},
		// versions and addresses
		{"k: 1.2.3\n", "1.2.3"},
		{"k: 192.168.1.1\n", "192.168.1.1"},
		// notations tony does not have
		{"k: 0x1f\n", "0x1f"},
		{"k: 1_000\n", "1_000"},
		// malformed numbers
		{"k: 3..14\n", "3..14"},
		{"k: 1.\n", "1."},
		// negative, which reaches the number scanner by a separate path
		{"k: -30s\n", "-30s"},
		{"k: -1-2\n", "-1-2"},
		// in an array, where the old message was "unseparated array elements"
		{"- 30s\n", "30s"},
		// the scalar is named even when a key separator follows it
		{"30s: v\n", "30s"},
	} {
		toks, err := Tokenize(nil, []byte(c.doc))
		if err == nil {
			t.Errorf("%q: no error, got %d tokens", c.doc, len(toks))
			continue
		}
		if !errors.Is(err, ErrDigitLeading) {
			t.Errorf("%q: got %v, want ErrDigitLeading", c.doc, err)
			continue
		}
		if !strings.Contains(err.Error(), `"`+c.want+`"`) {
			t.Errorf("%q: error does not name %q: %v", c.doc, c.want, err)
		}
	}
}

// TestDigitLeadingAccepts guards the cases that must keep tokenizing. A digit-leading run
// is not by itself a problem: it is only one when the run outruns the number in it.
func TestDigitLeadingAccepts(t *testing.T) {
	for _, doc := range []string{
		"k: 100\n",
		"k: 0\n",
		"k: 0.5\n",
		"k: 1.5\n",
		"k: -2.5\n",
		"k: 1e9\n",
		"k: -0e24\n",
		"k: 10000000005\n",
		`k: "30s"` + "\n", // quoted, which is the fix the message suggests
		"k: '100m'\n",
		"- 1\n- 2\n",
		"[1 2 3]\n",   // ']' closes the run without continuing it
		"{a: 1}\n",    // '}' likewise
		"[1, 2, 3]\n", // ',' likewise
		// Sparse-array keys written without a space after the colon. getSingleLiteral
		// only chops a trailing ':', so "0:42" arrives whole and would read as a
		// digit-leading scalar if the run were not stopped at the colon.
		"{0:42}\n",
		"{0:3.14}\n",
		"{0:true}\n",
		"{0:null, 1:42}\n",
		"0: hello\n13: other\n",
	} {
		if _, err := Tokenize(nil, []byte(doc)); err != nil {
			t.Errorf("%q: %v", doc, err)
		}
	}
}

// TestDigitLeadingStreaming checks the buffer-boundary path. The scalar has to be whole
// before the tokenizer can tell a number from a mis-lex, so a run reaching the end of the
// read buffer must ask for more data rather than reporting on a truncated view — and must
// name the entire scalar once it has it.
func TestDigitLeadingStreaming(t *testing.T) {
	for _, c := range []struct {
		doc  string
		want string
	}{
		{"k: 30s\n", "30s"},
		{"k: 1h30m\n", "1h30m"},
		{"k: 192.168.1.1\n", "192.168.1.1"},
		{"k: -30s\n", "-30s"},
	} {
		// oneByteReader forces a boundary at every byte of the scalar.
		src := NewTokenSource(&oneByteReader{r: bytes.NewReader([]byte(c.doc))})
		var err error
		for {
			_, err = src.Read()
			if err != nil {
				break
			}
		}
		if err == io.EOF {
			t.Errorf("%q: streamed to EOF without an error", c.doc)
			continue
		}
		if !errors.Is(err, ErrDigitLeading) {
			t.Errorf("%q: got %v, want ErrDigitLeading", c.doc, err)
			continue
		}
		if !strings.Contains(err.Error(), `"`+c.want+`"`) {
			t.Errorf("%q: streamed error does not name %q: %v", c.doc, c.want, err)
		}
	}
}

// oneByteReader yields one byte per Read, so every scalar straddles a buffer boundary.
// iotest.OneByteReader is the same idea; this keeps the dependency local.
type oneByteReader struct{ r io.Reader }

func (o *oneByteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return o.r.Read(p[:1])
}
