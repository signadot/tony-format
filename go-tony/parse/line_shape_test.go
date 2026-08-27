package parse

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/ir"
)

// A line ends at its last non-space character or at a comment, and a block
// literal's opening line is a line. It used to be the one place in the format that
// was strict about this, and strict about the wrong thing: `| ` and `| # why` were
// refused where `k: v ` and `k: v # why` are accepted, and the refusal came out as
// `unexpected ""` (0y342gdzh12ks0vkgxn0, 6ykv73beh12krzeygsn0).
func TestBlockLiteralOpeningLineIsALine(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{"plain", "k: |\n  a\n  b\n", "a\nb\n"},
		{"trailing space", "k: | \n  a\n  b\n", "a\nb\n"},
		{"a comment", "k: | # why\n  a\n  b\n", "a\nb\n"},
		{"a comment after chomp", "k: |- # why\n  a\n  b\n", "a\nb"},
		{"a comment after keep", "k: |+ # why\n  a\n  b\n", "a\nb\n"},
		{"space and a comment", "k: |   # why\n  a\n", "a\n"},
		{"crlf", "k: |\r\n  a\r\n", "a\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []struct {
				name string
				opt  ParseOption
			}{{"tony", ParseTony()}, {"yaml", ParseYAML()}} {
				n, err := Parse([]byte(tc.src), mode.opt)
				if err != nil {
					t.Errorf("%s: %v", mode.name, err)
					continue
				}
				if got := n.Values[0].String; got != tc.want {
					t.Errorf("%s: got %q, want %q", mode.name, got, tc.want)
				}
			}
		})
	}
}

// The opening line still ends where a line ends: anything which is not whitespace,
// a comment or the newline is refused, and the message names the byte.
func TestBlockLiteralOpeningLineStillRefusesJunk(t *testing.T) {
	for _, src := range []string{"k: |x\n  a\n", "k: |-x\n  a\n", "k: | x\n  a\n"} {
		if _, err := Parse([]byte(src), ParseTony()); err == nil {
			t.Errorf("%q was accepted", src)
		} else if !strings.Contains(err.Error(), "unexpected x") {
			t.Errorf("%q: error %q does not name the byte", src, err)
		}
	}
}

// The spec's own leading-space example is written `| ` and did not parse. It also
// claimed the content's trailing whitespace is stripped, and it is kept -- which is
// what YAML does, so the implementation was right and the document wrong.
func TestSpecLeadingSpaceExampleParses(t *testing.T) {
	n, err := Parse([]byte("|  \n   <   \n  ^ leading space\n"), ParseTony())
	if err != nil {
		t.Fatalf("the documented example does not parse: %v", err)
	}
	if want := " <   \n^ leading space\n"; n.String != want {
		t.Errorf("got %q, want %q", n.String, want)
	}
}

// A section holding only comments is skipped as a blank one is. Refusing it made
// adding prose to an empty section turn an accepted file into a rejected one, which
// is the opposite of what "a comment is not data" means everywhere else
// (ry96wwdvh12ks04gg5n0).
func TestMultiSkipsASectionWithNoValue(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      int
	}{
		{"only a comment", "a: 1\n---\n# only prose here\n---\nb: 2\n", 2},
		{"blank, as before", "a: 1\n---\n\n---\nb: 2\n", 2},
		{"several comments", "a: 1\n---\n# one\n# two\n---\nb: 2\n", 2},
		{"a trailing separator", "a: 1\n---\nb: 2\n---\n", 2},
		{"a trailing separator and prose", "a: 1\n---\nb: 2\n---\n# done\n", 2},
		{"a leading separator", "---\na: 1\n", 1},
		{"nothing but prose", "# only prose\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// the comment setting is not the variable: both refused it identically
			for _, comments := range []bool{false, true} {
				nodes, err := ParseMulti([]byte(tc.src), ParseComments(comments))
				if err != nil {
					t.Errorf("comments=%v: %v", comments, err)
					continue
				}
				if len(nodes) != tc.want {
					t.Errorf("comments=%v: %d documents, want %d", comments, len(nodes), tc.want)
				}
			}
		})
	}
}

// A dangling ':' is in the grammar nowhere -- the key set is `{a b c}`, bracketed,
// with no colon -- and it was accepted in exactly one position, as the last pair of
// a document, where it produced a null the author had not written. So `p:` parsed
// while it was last and stopped parsing the moment a sibling was written below it
// (7ba8gz2eh12ksbwxe5n0).
func TestDanglingColonIsRefused(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"as the last pair", "a: 1\np:\n"},
		{"as the last pair, no trailing newline", "a: 1\np:"},
		{"tagged, as the last pair", "a: 1\np: !delete\n"},
		{"as the whole document", "p:\n"},
		{"nested, as the last pair", "a:\n  b: 1\n  p:\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.src), ParseTony())
			if err == nil {
				t.Fatalf("%q was accepted", tc.src)
			}
			// the message names the requirement and both spellings that work. It
			// says "must be followed by a value" rather than "with no value",
			// which would be untrue of `a: !delete` -- a tag is not a value.
			for _, want := range []string{"must be followed by a value", "null", "key set"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not say %q", err, want)
				}
			}
		})
	}
}

// YAML mode reads what YAML writes, and in YAML a `key:` with nothing after it is
// an ordinary null.
func TestDanglingColonIsYAML(t *testing.T) {
	for _, src := range []string{"a: 1\np:\n", "a: 1\np:", "p:\n"} {
		n, err := Parse([]byte(src), ParseYAML())
		if err != nil {
			t.Errorf("%q: YAML mode refused it: %v", src, err)
		} else if out := encode.MustString(n); !strings.Contains(out, "null") {
			t.Errorf("%q: got %q, want a null", src, out)
		}
	}
}

// and what a writer produces for either spelling is the one the format defines
func TestNullValueRoundTrips(t *testing.T) {
	n, err := Parse([]byte("a: 1\np: null\n"), ParseTony())
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := encode.Encode(n, &b, encode.EncodeFormat(format.TonyFormat)); err != nil {
		t.Fatal(err)
	}
	if got, want := b.String(), "a: 1\np: null\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ParseComments decides what is IN a document, never whether there is one.
//
// A document holding only comments holds no value, and every other input holding
// none already answered (nil, nil). With comments on, this one answered a comment
// node with no child -- a shape the rest of the library refuses: ir_json calls it a
// malformed head comment, and every uncomment site unwraps exactly one value or
// nothing. So the same bytes were a document or not depending on a parse option.
func TestNoValueIsNoDocumentEitherWay(t *testing.T) {
	for _, src := range []string{
		"",
		"\n",
		"   \n",
		"# just a comment\n",
		"# just a comment",
		"# one\n# two\n",
		"\n# after a blank line\n",
	} {
		for _, comments := range []bool{false, true} {
			n, err := Parse([]byte(src), ParseTony(), ParseComments(comments))
			if err != nil {
				t.Errorf("%q comments=%v: %v", src, comments, err)
				continue
			}
			if n != nil {
				t.Errorf("%q comments=%v: got a %v with %d values, want no document",
					src, comments, n.Type, len(n.Values))
			}
		}
	}
}

// And a document which holds one is unaffected: the comments ride on it as before.
func TestAValueWithCommentsIsStillADocument(t *testing.T) {
	n, err := Parse([]byte("# lead\na: 1 # why\n"), ParseTony(), ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	if n == nil {
		t.Fatal("no document")
	}
	if n.Type != ir.CommentType || len(n.Values) != 1 {
		t.Fatalf("got %v with %d values, want a comment wrapping one value", n.Type, len(n.Values))
	}
	if got := n.Lines; len(got) != 1 || got[0] != "# lead" {
		t.Errorf("head comment lines %v, want [# lead]", got)
	}
}
