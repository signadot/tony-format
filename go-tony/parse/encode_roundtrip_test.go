package parse

import (
	"bytes"
	"fmt"
	"testing"
	"unicode/utf8"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/token"
)

// encodeScalarDoc puts body in a nested document as a scalar value with a sibling key that
// sorts AFTER it, so whatever the encoder emits is followed by more document. With no
// following sibling the document simply ends and a mis-encoded scalar goes unnoticed —
// which is why this bug survived: the natural small test case cannot see it.
func encodeScalarDoc(t *testing.T, body string) ([]byte, bool) {
	t.Helper()
	doc := ir.FromMap(map[string]*ir.Node{
		"lvl1": ir.FromMap(map[string]*ir.Node{
			"lvl2": ir.FromMap(map[string]*ir.Node{
				"a_scalar": ir.FromString(body),
				"z_after":  ir.FromString("sibling"),
			}),
		}),
	})
	var b bytes.Buffer
	if err := encode.Encode(doc, &b, encode.EncodeFormat(format.TonyFormat)); err != nil {
		return nil, false
	}
	return b.Bytes(), true
}

// reparseScalar returns what a scalar becomes after encode -> parse: the node found back
// at its key, or an error. The property under test is fidelity, not merely parseability —
// "-1" parsed fine and came back a Number, which is a quieter failure than a broken
// document and was invisible to a test that only checked for a parse error.
func reparseScalar(t *testing.T, body string) (*ir.Node, error) {
	t.Helper()
	out, ok := encodeScalarDoc(t, body)
	if !ok {
		return nil, nil
	}
	n, err := Parse(out)
	if err != nil {
		return nil, fmt.Errorf("%w\n%s", err, out)
	}
	return ir.Get(ir.Get(ir.Get(n, "lvl1"), "lvl2"), "a_scalar"), nil
}

// sameScalar compares a reparsed scalar to what went in — exactly, including trailing
// newlines on multi-line values. This used to trim them: block-literal chomping added one,
// so "one\ntwo\n\n" came back with a third. That is fixed, so the comparison is exact and
// the target has its teeth back on multi-line input.
func sameScalar(in string, v *ir.Node) bool {
	return v.Type == ir.StringType && v.String == in
}

// A string scalar must come back as the same string. The scalars that broke this were bare
// "-" and anything opening with '<', '>' or '|' — the tokenizer claims those bytes in value
// position, so the value was read as a list marker or a block-scalar header and the
// document fell apart at the next key — plus negative numbers, which parsed cleanly and
// came back as Numbers.
func TestEncoderOutputAlwaysReparses(t *testing.T) {
	var bad []string
	try := func(body string) {
		v, err := reparseScalar(t, body)
		if err != nil {
			bad = append(bad, fmt.Sprintf("%q (NeedsQuote=%v) UNPARSEABLE: %v",
				body, token.NeedsQuote(body), err))
			return
		}
		if v == nil {
			return
		}
		if !sameScalar(body, v) {
			bad = append(bad, fmt.Sprintf("%q (NeedsQuote=%v) came back type=%v value=%q",
				body, token.NeedsQuote(body), v.Type, v.String))
		}
	}
	// Every printable ASCII byte, alone and at each end of a word.
	for c := byte(33); c < 127; c++ {
		try(string(c))
		try(string(c) + "x")
		try("x" + string(c))
	}
	for _, s := range []string{
		"- ", "> ", "| ", "# ", "-x", ">x", "|x", "<x", "!x", "&x", "*x", "@x", "`x",
		"true", "false", "null", "0", "12", "1.5", "-1", "", " ", "\t",
		"a b", "a: b", "a\nb", "a\n\nb", "--- a/x", "|-", "|+", ">-",
	} {
		try(s)
	}
	if len(bad) != 0 {
		t.Errorf("%d scalars that do not survive encode -> parse:", len(bad))
		for _, s := range bad {
			t.Errorf("   %s", s)
		}
	}
}

// NeedsQuote mirrors a dispatch that lives in the tokenizer, so it can drift. Rather than
// re-derive that list here, check the property it exists to guarantee: if NeedsQuote says
// a scalar is safe bare, the encoder's output for it must reparse. A new sigil in the
// tokenizer fails this without anyone remembering to update NeedsQuote.
func TestNeedsQuoteAgreesWithRoundTrip(t *testing.T) {
	var corpus []string
	for c := byte(33); c < 127; c++ {
		corpus = append(corpus, string(c), string(c)+"x", "x"+string(c), string(c)+string(c))
	}
	corpus = append(corpus, "true", "false", "null", "0", "-", "-x", "|", "|x", "<", ">",
		"a", "abc", "a.b", "a/b", "a-b", "a_b")
	// Digit-leading scalars, where NeedsQuote has the most to get wrong: some of these
	// are strings written bare, some are numbers that must be quoted to stay strings,
	// and some do not tokenize at all unquoted.
	corpus = append(corpus,
		// strings the tokenizer accepts bare
		"30s", "100m", "1Gi", "1h30m", "3d", "10x", "0m", "007m",
		"1.2.3", "192.168.1.1", "1.2.3.4.5", "-30s",
		// numbers: bare, these come back as Numbers rather than as these strings
		"0", "12", "100", "1.5", "0.5", "-1", "-2.5", "1e9", "-0e24", "10000000005",
		// runs the tokenizer rejects bare
		"0x1f", "0b1010", "0o777", "007", "0644", "1_000", "3..14", "1.", "1.2.", "-1-2",
		// a colon stops a digit-leading run, so these cannot be written bare either
		"30s:x", "1:2", "0:42")
	for _, s := range corpus {
		if token.NeedsQuote(s) {
			continue // claimed unsafe; quoting is always allowed
		}
		v, err := reparseScalar(t, s)
		if err != nil {
			t.Errorf("NeedsQuote(%q) = false, but the encoder's output does not reparse: %v", s, err)
			continue
		}
		if v != nil && !sameScalar(s, v) {
			t.Errorf("NeedsQuote(%q) = false, but it came back type=%v value=%q", s, v.Type, v.String)
		}
	}
}

// FuzzEncodeParseRoundTrip found the first of these in 51 executions. It is cheap and it
// covers the shape no hand-written case list will: arbitrary scalar content followed by
// more document.
func FuzzEncodeParseRoundTrip(f *testing.F) {
	for _, s := range []string{
		"-", ">", "|", "<", "one\ntwo\n\n", "# head\n\ntext\n\n\n",
		"--- a/x\n+++ b/x\n@@ -1 +1 @@\n \tcode\n", "a\n\n \n\t\n", "```\ncode\n```\n\n",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, body string) {
		if !utf8.ValidString(body) {
			return
		}
		v, err := reparseScalar(t, body)
		if err != nil {
			t.Fatalf("encoder emitted something the parser rejects: %v\nbody=%q", err, body)
		}
		if v != nil && !sameScalar(body, v) {
			t.Fatalf("scalar changed on a round trip: %q came back type=%v value=%q",
				body, v.Type, v.String)
		}
	})
}
