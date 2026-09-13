package stream

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// The decoder reads a document as the parser does. A tag followed by a line
// comment lost the tag, since the tag was held in the call that returned the
// comment; a line comment after a key, before its value, was hung on the previous
// sibling, where the parser gives it to the value (p478tacqh12krg32msn0 item 18).
func TestDecoderPlacesTagsAndCommentsAsTheParserDoes(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		at        func(*ir.Node) *ir.Node // the value the tag and comment belong to
		sibling   func(*ir.Node) *ir.Node // a value that must carry nothing, or nil
	}{
		{"tag, comment, scalar", "{a: !t # c\n  1}\n",
			func(n *ir.Node) *ir.Node { return ir.Get(n, "a") }, nil},
		{"tag, comment, object", "{a: !t # c\n  {x: 1}}\n",
			func(n *ir.Node) *ir.Node { return ir.Get(n, "a") }, nil},
		{"tag, comment, scalar, then a sibling", "{a: !t # c\n  1, b: 2}\n",
			func(n *ir.Node) *ir.Node { return ir.Get(n, "a") },
			func(n *ir.Node) *ir.Node { return ir.Get(n, "b") }},
		{"comment after a key, before its value", "{a: 1, k: # c\n  {nested: 1}}\n",
			func(n *ir.Node) *ir.Node { return ir.Get(n, "k") },
			func(n *ir.Node) *ir.Node { return ir.Get(n, "a") }},
		{"comment after an int key", "{0: 1, 1: # c\n  {nested: 1}}\n",
			func(n *ir.Node) *ir.Node { return n.Values[1] },
			func(n *ir.Node) *ir.Node { return n.Values[0] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, err := parse.Parse([]byte(tc.src), parse.ParseComments(true))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			dec, err := NewDecoder(strings.NewReader(tc.src), WithBrackets())
			if err != nil {
				t.Fatal(err)
			}
			got, err := ReadDocument(dec)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			wantAt, gotAt := ir.Uncomment(tc.at(want)), ir.Uncomment(tc.at(got))
			// The parser adds !bracket for the braces; the decoder does not. Every
			// other tag has to agree.
			if w, g := ir.TagRemove(wantAt.Tag, ir.BracketTag), gotAt.Tag; w != g {
				t.Errorf("tag %q, want %q", g, w)
			}
			if wantAt.Comment == nil {
				t.Fatalf("the parser put no line comment on the value; test is wrong")
			}
			if gotAt.Comment == nil || strings.Join(gotAt.Comment.Lines, "\n") != strings.Join(wantAt.Comment.Lines, "\n") {
				var lines []string
				if gotAt.Comment != nil {
					lines = gotAt.Comment.Lines
				}
				t.Errorf("line comment %q, want %q", lines, wantAt.Comment.Lines)
			}
			if tc.sibling != nil {
				if s := ir.Uncomment(tc.sibling(got)); s.Comment != nil {
					t.Errorf("the sibling carries %q", s.Comment.Lines)
				}
			}
		})
	}
}

// A tag followed by a head comment keeps the tag on the value the comment heads.
func TestDecoderKeepsATagAcrossAHeadComment(t *testing.T) {
	dec, _ := NewDecoder(strings.NewReader("[!t # c\n  1]\n"), WithBrackets())
	got, err := ReadDocument(dec)
	if err != nil {
		t.Fatal(err)
	}
	v := ir.Uncomment(got.Values[0])
	if v.Tag != "!t" {
		t.Errorf("tag %q, want !t", v.Tag)
	}
	if got.Values[0].Type != ir.CommentType {
		t.Errorf("the comment is not the value's head comment: %v", got.Values[0].Type)
	}
}
