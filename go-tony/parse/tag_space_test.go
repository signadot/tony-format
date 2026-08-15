package parse

import (
	"strings"
	"testing"
)

// TestTwoTagsIsAnError: a node takes one tag, composed with '.', and the grammar
// has no form in which two sit side by side:
//
//	<tag-content> ::= <single-tag> [ '.' <single-tag> ]...
//
// The parser used to accept the side-by-side form and keep the LAST tag,
// discarding the first. `!raw !let {...}` therefore parsed as a bare !let: the
// escape saying "this subtree is data" vanished, and what a store received was
// an instruction. Refusing beats dropping -- a tag silently discarded takes the
// meaning of the document with it.
func TestTwoTagsIsAnError(t *testing.T) {
	for _, tc := range []struct {
		name, doc, wantComposed string
	}{
		{"the escape and an operator", `!raw !let {let: [], in: {}}`, "!raw.let"},
		{"two match operators", `!not !has-path state`, "!not.has-path"},
		{"as a field's value", `{f: !not !has-path state}`, "!not.has-path"},
		{"across a newline", "!not\n!has-path state", "!not.has-path"},
		{"tags with arguments", `!t1 !key(name) [{name: a}]`, "!t1.key(name)"},
		{"data tags, which are no different", `!a !b 1`, "!a.b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.doc))
			if err == nil {
				t.Fatalf("%q parsed; one of its tags would have been dropped", tc.doc)
			}
			if !strings.Contains(err.Error(), tc.wantComposed) {
				t.Errorf("the error does not name the composed spelling %q: %v", tc.wantComposed, err)
			}
		})
	}
}

// TestComposedTagsStillParse: the '.' form is the one the grammar has, and it
// keeps every label.
func TestComposedTagsStillParse(t *testing.T) {
	for _, tc := range []struct{ doc, want string }{
		{`!raw.let {let: [], in: {}}`, "!raw.let"},
		{`!not.has-path state`, "!not.has-path"},
		{`!t1.key(name) [{name: a}]`, "!t1.key(name)"},
		{`!a.b.c 1`, "!a.b.c"},
	} {
		t.Run(tc.doc, func(t *testing.T) {
			n, err := Parse([]byte(tc.doc))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !strings.Contains(n.Tag, strings.TrimPrefix(tc.want, "!")) {
				t.Fatalf("tag is %q, want it to hold %q", n.Tag, tc.want)
			}
		})
	}
}
