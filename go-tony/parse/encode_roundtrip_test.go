package parse

import (
	"bytes"
	"fmt"
	"strings"
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

// sameScalar compares a reparsed scalar to what went in.
//
// Multi-line values are compared with trailing newlines trimmed. Block-literal chomping
// does not preserve them exactly — "one\ntwo\n\n" comes back with a third newline — which
// is a real defect, tracked separately, in a different part of the encoder from the quoting
// this file is about. Trimming keeps that known gap from masking a NEW multi-line failure:
// any difference in content still fails here.
func sameScalar(in string, v *ir.Node) bool {
	if v.Type != ir.StringType {
		return false
	}
	if !strings.Contains(in, "\n") {
		return v.String == in
	}
	return strings.TrimRight(v.String, "\n") == strings.TrimRight(in, "\n")
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
		// A correctly encoded U+FFFD is rejected by the scanners as bad utf8 -- they test
		// r == utf8.RuneError without the accompanying sz == 1. Tracked in
		// prfzxpa2h12ks3vscxn0; excluded here so it does not mask quoting failures.
		if strings.ContainsRune(body, utf8.RuneError) {
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
