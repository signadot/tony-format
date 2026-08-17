package parse

import (
	"bytes"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
)

// A keep-chomped block scalar keeps the newlines the document HAS.  The
// whole-document reader appended one for itself, and keep preserved it, so
// `k: |+\n  x\n` read as "x\n\n" -- and the encoder wrote that back out, and the
// next read grew it again: "x\n\n\n\n", then "x\n\n\n\n\n\n"
// (75g1kbpdh12krs09gdn0).
func TestKeptLiteralDoesNotGrow(t *testing.T) {
	const src = "k: |+\n  x\n"
	doc := src
	for round := 0; round < 4; round++ {
		node, err := Parse([]byte(doc))
		if err != nil {
			t.Fatalf("round %d: %s", round, err)
		}
		var got string
		for i, f := range node.Fields {
			if f.String == "k" {
				got = node.Values[i].String
			}
		}
		if got != "x\n" {
			t.Fatalf("round %d: value is %q, want %q (document was %q)", round, got, "x\n", doc)
		}
		var b bytes.Buffer
		if err := encode.Encode(node, &b); err != nil {
			t.Fatalf("round %d: encode: %s", round, err)
		}
		doc = b.String()
	}
}

// The same rule, for the trailing newline itself: a document which ends in one
// must not be given a second.
func TestEmptyLiteralAtTheEndOfADocument(t *testing.T) {
	node, err := Parse([]byte("k: |\n"))
	if err != nil {
		t.Fatalf("an empty multiline literal at the end of a document: %s", err)
	}
	for i, f := range node.Fields {
		if f.String == "k" && node.Values[i].String != "" {
			t.Errorf("value is %q, want empty", node.Values[i].String)
		}
	}
}
