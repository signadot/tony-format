package parse

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
)

// A comment on the line of a closing bracket is the line comment of the collection
// the bracket closes (docs/tony.md, Comments). It was carried up to the enclosing
// collection instead, which wrote it on a line of its own after its own close, where
// the parser refused it as trailing material (4ynqp7wqh12krg32msn0 item 18).
func TestCommentOnAClosingBracketBelongsToWhatItCloses(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		at        func(*ir.Node) *ir.Node // the collection the comment closes
	}{
		{"list in a list", "[\n  [\n    1\n  ] # c\n]\n", func(n *ir.Node) *ir.Node { return n.Values[0] }},
		{"object in a list", "[\n  {\n    a: 1\n  } # c\n]\n", func(n *ir.Node) *ir.Node { return n.Values[0] }},
		{"list under a field", "a: [\n  [\n    1\n  ] # c\n]\n", func(n *ir.Node) *ir.Node { return ir.Get(n, "a").Values[0] }},
		{"object value", "{\n  a: [\n    1\n  ] # c\n}\n", func(n *ir.Node) *ir.Node { return ir.Get(n, "a") }},
		{"one-line object", "[\n  {a: 1} # c\n]\n", func(n *ir.Node) *ir.Node { return n.Values[0] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := Parse([]byte(tc.src), ParseComments(true))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			at := ir.Uncomment(tc.at(n))
			if at.Comment == nil || len(at.Comment.Lines) != 1 || at.Comment.Lines[0] != " # c" {
				var lines []string
				if at.Comment != nil {
					lines = at.Comment.Lines
				}
				t.Errorf("the closed collection's line comment is %q, want [\" # c\"]", lines)
			}
			if n.Comment != nil && at != n {
				t.Errorf("the enclosing collection carries %q", n.Comment.Lines)
			}

			var b strings.Builder
			if err := encode.Encode(n, &b, encode.EncodeComments(true)); err != nil {
				t.Fatalf("encode: %v", err)
			}
			back, err := Parse([]byte(b.String()), ParseComments(true))
			if err != nil {
				t.Fatalf("the encoding does not parse: %v\n%s", err, b.String())
			}
			var b2 strings.Builder
			_ = encode.Encode(back, &b2, encode.EncodeComments(true))
			if b2.String() != b.String() {
				t.Errorf("not stable:\n%s\nthen\n%s", b.String(), b2.String())
			}
		})
	}
}
