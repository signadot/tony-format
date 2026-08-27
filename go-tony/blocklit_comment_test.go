package tony

import (
	"bytes"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A block literal's opening line may carry a comment. openLine started accepting one
// -- a line ends at its last non-space character or at a comment, and the opening line
// is a line -- but nothing made a token of it, so the comment was read and thrown away.
// With -c, where a comment on the line above survives, which is what made it look like
// a decision rather than an omission.
//
// It is a LINE comment on the literal, not a head comment on its content. Both can be
// written at once
//
//	# what the value is
//	| # how it is written
//	  ...
//
// and a head comment is a wrapper node while a line comment is a field of the node it
// is on, so reading the second as a head comment would merge it into the first and
// lose a position the format lets a writer use.
func TestBlockLit_OpeningLineComment(t *testing.T) {
	tests := []struct {
		name string
		in   string
		// want is the document as `o v -c` writes it. Equal to in wherever the
		// input is already in normal form, which is the point of most of these.
		want string
		// str is the string the literal holds, which no comment may alter.
		str string
		// noComments is the document with comments off.
		noComments string
	}{{
		name:       "a comment on the opening line",
		in:         "| # hello\n  I am an ape\n  null ape.\n",
		want:       "| # hello\n  I am an ape\n  null ape.\n",
		str:        "I am an ape\nnull ape.\n",
		noComments: "|\n  I am an ape\n  null ape.\n",
	}, {
		name:       "a head comment and a line comment, kept apart",
		in:         "# what the value is\n| # how it is written\n  ape\n  ape\n",
		want:       "# what the value is\n| # how it is written\n  ape\n  ape\n",
		str:        "ape\nape\n",
		noComments: "|\n  ape\n  ape\n",
	}, {
		name:       "on a field's value",
		in:         "k: | # why\n  ape\n  ape\n",
		want:       "k: | # why\n  ape\n  ape\n",
		str:        "ape\nape\n",
		noComments: "k: |\n  ape\n  ape\n",
	}, {
		name:       "with a chomp indicator",
		in:         "|- # why\n  ape\n  ape\n",
		want:       "|- # why\n  ape\n  ape\n",
		str:        "ape\nape",
		noComments: "|-\n  ape\n  ape\n",
	}, {
		name: "a content line that starts with #",
		in:   "| # hello\n  # not a comment\n  ape\n",
		want: "| # hello\n  # not a comment\n  ape\n",
		str:  "# not a comment\nape\n",
	}, {
		name: "the space before the # is the comment's, as it is everywhere else",
		in:   "|   # hello\n  ape\n  ape\n",
		want: "|   # hello\n  ape\n  ape\n",
		str:  "ape\nape\n",
	}, {
		name: "no comment",
		in:   "|\n  ape\n  ape\n",
		want: "|\n  ape\n  ape\n",
		str:  "ape\nape\n",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, err := parse.Parse([]byte(test.in), parse.ParseComments(true))
			if err != nil {
				t.Fatalf("parse: %s", err)
			}
			if s := literalString(t, doc); s != test.str {
				t.Errorf("the literal holds %q, want %q", s, test.str)
			}
			got, err := encString(doc, true)
			if err != nil {
				t.Fatalf("encode: %s", err)
			}
			if got != test.want {
				t.Errorf("wrote\n%q\nwant\n%q", got, test.want)
			}
			// Written once is written twice: o v -w writes its answer back over
			// the file, so a form that is not its own normal form drifts.
			again, err := parse.Parse([]byte(got), parse.ParseComments(true))
			if err != nil {
				t.Fatalf("reparse: %s", err)
			}
			got2, err := encString(again, true)
			if err != nil {
				t.Fatalf("re-encode: %s", err)
			}
			if got2 != got {
				t.Errorf("a second pass wrote\n%q\nafter\n%q", got2, got)
			}
			if test.noComments == "" {
				return
			}
			off, err := parse.Parse([]byte(test.in))
			if err != nil {
				t.Fatalf("parse without comments: %s", err)
			}
			gotOff, err := encString(off, false)
			if err != nil {
				t.Fatalf("encode without comments: %s", err)
			}
			if gotOff != test.noComments {
				t.Errorf("with comments off wrote\n%q\nwant\n%q", gotOff, test.noComments)
			}
		})
	}
}

// encString encodes a document the way `o v` and `o v -c` write it.
func encString(doc *ir.Node, comments bool) (string, error) {
	buf := &bytes.Buffer{}
	if err := encode.Encode(doc, buf, encode.EncodeComments(comments)); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// literalString finds the one string in the document and answers what it holds.
func literalString(t *testing.T, doc *ir.Node) string {
	t.Helper()
	var found *ir.Node
	err := doc.Visit(func(n *ir.Node, isPost bool) (bool, error) {
		if !isPost && n.Type == ir.StringType && found == nil {
			found = n
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("visit: %s", err)
	}
	if found == nil {
		t.Fatal("no string in the document")
	}
	return found.String
}

// The comment is on the literal and stays there: it is not a head comment, so it does
// not merge with one, and it does not become a comment on the enclosing field.
func TestBlockLit_OpeningLineCommentIsALineComment(t *testing.T) {
	doc, err := parse.Parse([]byte("# above\n| # beside\n  ape\n"), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	if doc.Type != ir.CommentType {
		t.Fatalf("the head comment did not wrap the value; got %s", doc.Type)
	}
	if want := []string{"# above"}; !equalLines(doc.Lines, want) {
		t.Errorf("the head comment holds %q, want %q", doc.Lines, want)
	}
	val := doc.Values[0]
	if val.Comment == nil {
		t.Fatal("the opening line's comment is not on the literal")
	}
	if want := []string{" # beside"}; !equalLines(val.Comment.Lines, want) {
		t.Errorf("the line comment holds %q, want %q", val.Comment.Lines, want)
	}
}

func equalLines(a, b []string) bool {
	return strings.Join(a, "\x00") == strings.Join(b, "\x00")
}
