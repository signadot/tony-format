package parse

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/token"
)

// A tag ends at whitespace OR where the flow grammar it sits inside resumes.
//
// The tag production was YAML's -- any non-whitespace unicode -- so a structural
// character following a tag became part of its NAME. Three of these four shapes
// were wrong and the first was wrong SILENTLY: `!delete,` names no operator, an
// unknown tag is stored as data, and the field was not deleted
// (pkj422gkh12kr24gj1n0).
func TestTagEndsAtAStructuralCharacter(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"a comma follows", `{a !delete, b: 1}`},
		{"a brace closes", `{a !delete}`},
		{"a bracket closes", `[a, !delete]`},
		{"the spelling that always worked", `{a !delete }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := Parse([]byte(tc.src), ParseTony())
			if err != nil {
				t.Fatalf("%s: %v", tc.src, err)
			}
			out := encode.MustString(n)
			if strings.Contains(out, "delete,") || strings.Contains(out, "delete}") ||
				strings.Contains(out, "delete]") {
				t.Errorf("a structural character was absorbed into the tag:\n%s", out)
			}
		})
	}
}

// A component is where a bracket or a comma is legitimate content, so only depth 0
// terminates: `!get-path(a[0])` is an addressing form with no other spelling.
func TestTagComponentKeepsItsBracketsAndCommas(t *testing.T) {
	for _, tc := range []struct{ src, wantTag string }{
		{`{x: !get-path(a[0]) null}`, "!get-path(a[0])"},
		{`{x: !get-path(a[0])}`, "!get-path(a[0])"},
		{`{x: !tag(a,b) null}`, "!tag(a,b)"},
		{`{x: !key(sku) []}`, "!key(sku)"},
	} {
		n, err := Parse([]byte(tc.src), ParseTony())
		if err != nil {
			t.Fatalf("%s: %v", tc.src, err)
		}
		if got := encode.MustString(n); !strings.Contains(got, tc.wantTag) {
			t.Errorf("%s: lost the component, got:\n%s", tc.src, got)
		}
	}
}

// Whitespace still ends a tag at EVERY depth, deliberately. `!tag(a, b)` reads as
// `!tag(a,` in PyYAML -- silently -- so accepting it here would make a document
// which YAML misreads rather than one it refuses. Rejecting is the loud half.
func TestWhitespaceEndsATagAtAnyDepth(t *testing.T) {
	if _, err := Parse([]byte(`{x: !tag(a, b) null}`), ParseTony()); err == nil {
		t.Error("a space inside a tag component was accepted; YAML would read the tag truncated")
	}
}

// An unmatched '(' never returns to depth 0, so the scan runs to whitespace as it
// always did rather than inventing a terminator.
//
// Asserted on the TOKEN: the tag is scanned whole, and what happens to it after --
// ir.TagArgs finds no closing paren and keeps only the head, so the document encodes
// as `!t1` and the component is dropped silently -- is a separate question about a
// malformed tag, and predates this.
func TestUnmatchedParenDegradesToWhitespace(t *testing.T) {
	toks, err := token.Tokenize(nil, []byte(`{x: !t1(unclosed null}`))
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, tk := range toks {
		if tk.Type == token.TTag {
			tags = append(tags, string(tk.Bytes))
		}
	}
	if len(tags) != 1 || tags[0] != "!t1(unclosed" {
		t.Errorf("tags %q, want [\"!t1(unclosed\"] -- the scan should run to whitespace", tags)
	}
}

// A tag with no value is a tagged null, which the key-set sugar already emitted for
// `{a !delete }`. With the tag ending at the bracket too, the same thing written
// against one had nothing to balance and the bracket read as unopened.
func TestTagWithNoValueIsATaggedNull(t *testing.T) {
	for _, src := range []string{`{x: !t1}`, `[!t1]`, `{x: !t1, y: 1}`} {
		if _, err := Parse([]byte(src), ParseTony()); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}
}

// The key-set sugar emits a PAIR, and not counting it made a comma after a sugared
// key read as "is not a key" -- with no tag in sight.
func TestKeySetSugarCountsItsPair(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want int
	}{
		{`{a, b: 1}`, 2},
		{`{a, b}`, 2},
		{`{a b c}`, 3},
		{`{a: 1, b}`, 2},
	} {
		n, err := Parse([]byte(tc.src), ParseTony())
		if err != nil {
			t.Errorf("%s: %v", tc.src, err)
			continue
		}
		if len(n.Fields) != tc.want {
			t.Errorf("%s: %d fields, want %d", tc.src, len(n.Fields), tc.want)
		}
	}
}
