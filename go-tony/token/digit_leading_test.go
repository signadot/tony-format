package token

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestDigitLeadingStrings covers the scalars that begin with a digit and read as strings:
// quantities and durations, which every Kubernetes manifest is full of, and versions and
// addresses. Before this the number scanner took the leading digits and left the rest as
// its own token, so the document failed as two adjacent values.
func TestDigitLeadingStrings(t *testing.T) {
	for _, c := range []struct {
		doc  string
		want string // the literal the tokenizer must produce
	}{
		// quantities and durations
		{"k: 30s\n", "30s"},
		{"k: 100m\n", "100m"},
		{"k: 1Gi\n", "1Gi"},
		{"k: 1h30m\n", "1h30m"},
		{"k: 3d\n", "3d"},
		{"k: 10x\n", "10x"},
		{"k: 0m\n", "0m"},
		// a leading zero is only a botched number when the run is all digits
		{"k: 007m\n", "007m"},
		// versions and addresses: three or more dot-separated digit groups
		{"k: 1.2.3\n", "1.2.3"},
		{"k: 192.168.1.1\n", "192.168.1.1"},
		{"k: 1.2.3.4.5\n", "1.2.3.4.5"},
		// negative, which reaches the number scanner by a separate path
		{"k: -30s\n", "-30s"},
		// in an array, and in key position
		{"- 30s\n", "30s"},
		{"30s: v\n", "30s"},
	} {
		toks, err := Tokenize(nil, []byte(c.doc))
		if err != nil {
			t.Errorf("%q: %v", c.doc, err)
			continue
		}
		var got *Token
		for i := range toks {
			if toks[i].Type == TLiteral && string(toks[i].Bytes) == c.want {
				got = &toks[i]
				break
			}
		}
		if got == nil {
			t.Errorf("%q: no TLiteral %q in %v", c.doc, c.want, tokenSummary(toks))
		}
	}
}

// TestDigitLeadingErrors covers the runs that stay errors. A mistyped number must not
// quietly become text, which is the whole reason the string rule is two named shapes
// rather than "anything that is not a number".
func TestDigitLeadingErrors(t *testing.T) {
	for _, c := range []struct {
		doc  string
		want string // the scalar the error must name
	}{
		// Radix notations, held back until number() learns them. "0x1f" means 31 to
		// anyone who writes it, so reading it as text would be the silent misreading
		// this rule set exists to prevent.
		{"k: 0x1f\n", "0x1f"},
		{"k: 0b1010\n", "0b1010"},
		{"k: 0o777\n", "0o777"},
		// mistyped numbers
		{"k: 1_000\n", "1_000"},
		{"k: 3..14\n", "3..14"},
		{"k: 1.\n", "1."},
		{"k: 1.2.\n", "1.2."},
		{"k: -1-2\n", "-1-2"},
	} {
		if _, err := Tokenize(nil, []byte(c.doc)); err == nil {
			t.Errorf("%q: no error", c.doc)
		} else if !errors.Is(err, ErrDigitLeading) {
			t.Errorf("%q: got %v, want ErrDigitLeading", c.doc, err)
		} else if !strings.Contains(err.Error(), `"`+c.want+`"`) {
			t.Errorf("%q: error does not name %q: %v", c.doc, c.want, err)
		}
	}
}

// TestDigitLeadingLeadingZero pins that an all-digit run with a leading zero keeps its own
// message. It is a distinct diagnosis from ErrDigitLeading and a more useful one.
//
// "00.7" is not here: number() checks the leading zero only on the integer path, where
// f+e == 0, so a leading zero in front of a fraction has always been accepted as a float.
// That predates this rule set and is left alone by it.
func TestDigitLeadingLeadingZero(t *testing.T) {
	for _, doc := range []string{"k: 007\n", "k: 0644\n", "k: 0123456\n"} {
		if _, err := Tokenize(nil, []byte(doc)); err == nil {
			t.Errorf("%q: no error", doc)
		} else if !errors.Is(err, ErrNumberLeadingZero) {
			t.Errorf("%q: got %v, want ErrNumberLeadingZero", doc, err)
		}
	}
}

// TestDigitLeadingNumbers guards the numbers. A digit-leading run is not by itself a
// string: it is one only when the run outruns the number in it and matches a string shape.
func TestDigitLeadingNumbers(t *testing.T) {
	for _, c := range []struct {
		doc  string
		want TokenType
	}{
		{"k: 100\n", TInteger},
		{"k: 0\n", TInteger},
		{"k: -1\n", TInteger},
		{"k: 10000000005\n", TInteger},
		{"k: 0.5\n", TFloat},
		{"k: 1.5\n", TFloat},
		{"k: -2.5\n", TFloat},
		{"k: 1e9\n", TFloat},
		{"k: -0e24\n", TFloat},
	} {
		toks, err := Tokenize(nil, []byte(c.doc))
		if err != nil {
			t.Errorf("%q: %v", c.doc, err)
			continue
		}
		found := false
		for i := range toks {
			if toks[i].Type == c.want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q: no %v in %v", c.doc, c.want, tokenSummary(toks))
		}
	}
}

// TestDigitLeadingAccepts guards the documents that must keep tokenizing whole.
func TestDigitLeadingAccepts(t *testing.T) {
	for _, doc := range []string{
		`k: "30s"` + "\n", // quoted, which is the fix the error message suggests
		"k: '100m'\n",
		"- 1\n- 2\n",
		"[1 2 3]\n",   // ']' closes the run without continuing it
		"{a: 1}\n",    // '}' likewise
		"[1, 2, 3]\n", // ',' likewise
		"[30s, 1Gi]\n",
		"{cpu: 100m, memory: 1Gi}\n",
		// Sparse-array keys written without a space after the colon. getSingleLiteral
		// only chops a trailing ':', so "0:42" arrives whole and would read as a
		// digit-leading scalar if the run were not stopped at the colon.
		"{0:42}\n",
		"{0:3.14}\n",
		"{0:true}\n",
		"{0:null, 1:42}\n",
		"0: hello\n13: other\n",
		// The k8s shape that started this.
		"resources:\n  limits:\n    cpu: 100m\n    memory: 1Gi\n",
		"args:\n- -timeout\n- 30s\n",
	} {
		if _, err := Tokenize(nil, []byte(doc)); err != nil {
			t.Errorf("%q: %v", doc, err)
		}
	}
}

// TestDigitLeadingStreaming checks the buffer-boundary path. The scalar has to be whole
// before the tokenizer can tell a number from a string from a mis-lex, so a run reaching
// the end of the read buffer must ask for more data rather than deciding on a truncated
// view.
func TestDigitLeadingStreaming(t *testing.T) {
	strs := []struct{ doc, want string }{
		{"k: 30s\n", "30s"},
		{"k: 1h30m\n", "1h30m"},
		{"k: 192.168.1.1\n", "192.168.1.1"},
		{"k: -30s\n", "-30s"},
	}
	for _, c := range strs {
		toks, err := streamTokens(c.doc)
		if err != nil {
			t.Errorf("%q: %v", c.doc, err)
			continue
		}
		found := false
		for i := range toks {
			if toks[i].Type == TLiteral && string(toks[i].Bytes) == c.want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q: streamed without TLiteral %q: %v", c.doc, c.want, tokenSummary(toks))
		}
	}
	for _, c := range []struct{ doc, want string }{
		{"k: 0x1f\n", "0x1f"},
		{"k: 3..14\n", "3..14"},
	} {
		_, err := streamTokens(c.doc)
		if !errors.Is(err, ErrDigitLeading) {
			t.Errorf("%q: got %v, want ErrDigitLeading", c.doc, err)
			continue
		}
		if !strings.Contains(err.Error(), `"`+c.want+`"`) {
			t.Errorf("%q: streamed error does not name %q: %v", c.doc, c.want, err)
		}
	}
}

// streamTokens reads doc one byte at a time, so every scalar straddles a buffer boundary.
func streamTokens(doc string) ([]Token, error) {
	src := NewTokenSource(&oneByteReader{r: bytes.NewReader([]byte(doc))})
	var all []Token
	for {
		toks, err := src.Read()
		all = append(all, toks...)
		if err == io.EOF {
			return all, nil
		}
		if err != nil {
			return all, err
		}
	}
}

func tokenSummary(toks []Token) []string {
	out := make([]string, 0, len(toks))
	for i := range toks {
		out = append(out, toks[i].Type.String()+" "+string(toks[i].Bytes))
	}
	return out
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
