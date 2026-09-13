package encode

import (
	"bytes"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A string ending in a blank line, or in trailing space, is written as a |+ literal,
// whose kept newline ends the document's last line. The document's own closing
// newline was written after it regardless, and read back as one more line of the
// value: "x\n\n" came back "x\n\n\n", and grew again on every pass. Mid-document the
// kept newline was already respected (pyhfz6h6); at the end of the document it was
// not (4ynqp7wqh12krg32msn0 item 16).
func TestKeepLiteralEndingTheDocumentRoundTrips(t *testing.T) {
	for _, s := range []string{"x\n\n", "x \n", " \n", "a\r\n", "one\ntwo\n\n"} {
		for name, doc := range map[string]*ir.Node{
			"last value":   ir.FromMap(map[string]*ir.Node{"a": ir.FromInt(1), "z": ir.FromString(s)}),
			"root":         ir.FromString(s),
			"mid":          ir.FromMap(map[string]*ir.Node{"a": ir.FromString(s), "z": ir.FromInt(1)}),
			"last element": ir.FromSlice([]*ir.Node{ir.FromInt(1), ir.FromString(s)}),
		} {
			for _, comments := range []bool{false, true} {
				var b bytes.Buffer
				if err := Encode(doc, &b, EncodeComments(comments)); err != nil {
					t.Fatalf("%s %q: encode: %v", name, s, err)
				}
				back, err := parse.Parse(b.Bytes(), parse.ParseComments(comments))
				if err != nil {
					t.Fatalf("%s %q: parse back %q: %v", name, s, b.String(), err)
				}
				var got string
				switch name {
				case "root":
					got = back.String
				case "last element":
					got = back.Values[1].String
				case "mid":
					got = ir.Get(back, "a").String
				default:
					got = ir.Get(back, "z").String
				}
				if got != s {
					t.Errorf("%s %q comments=%v: encoded %q, read back %q", name, s, comments, b.String(), got)
				}
			}
		}
	}
}
