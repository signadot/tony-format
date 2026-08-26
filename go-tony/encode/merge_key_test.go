package encode

import (
	"bytes"
	"testing"

	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// `<<` is a token in the grammar, not a field name. The encoder used to hand it
// to writeField, which quotes what token.NeedsQuote rejects as a name -- and
// `<<` is not a name a field may have, so it came back `"<<"`. That re-parses
// as an ordinary string field whose name is two angle brackets, and nothing
// downstream can tell it was ever a merge key: the document that went in is not
// the document that comes out (nfs2rkf3h12kr5gth1n0).
func TestMergeKeyRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
	}{
		{"string value", "a: 1\n<<: \"{{ tpl }}\"\nb: 2\n"},
		{"alone", "<<: \"{{ tpl }}\"\n"},
		{"nested in an object", "a:\n  <<: \"{{ tpl }}\"\n  b: 2\n"},
		{"two of them", "<<: \"{{ a }}\"\n<<: \"{{ b }}\"\n"},
		{"object value", "<<:\n  x: 1\n"},
		{"in a list element", "- <<: \"{{ tpl }}\"\n  a: 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node, err := parse.Parse([]byte(tc.doc))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			var buf bytes.Buffer
			if err := Encode(node, &buf, EncodeFormat(format.TonyFormat)); err != nil {
				t.Fatalf("encode: %v", err)
			}
			if buf.String() != tc.doc {
				t.Errorf("round trip: got %q, want %q", buf.String(), tc.doc)
			}
			got, err := parse.Parse(buf.Bytes())
			if err != nil {
				t.Fatalf("encoded document does not parse: %v\noutput: %q", err, buf.String())
			}
			// The spelling is only the symptom: what has to survive is the
			// merge key itself, which the IR carries as a null-typed field.
			if want, got := countMergeKeys(node), countMergeKeys(got); got != want {
				t.Errorf("merge keys: got %d, want %d\noutput: %q", got, want, buf.String())
			}
		})
	}
}

// The other half of the same distinction: a field whose name really is the two
// characters `<<` is a name, it does need quoting, and unquoting it would turn
// it into a merge key it never was.
func TestStringFieldNamedMergeKeyStaysQuoted(t *testing.T) {
	node := ir.FromKeyVals([]ir.KeyVal{
		{Key: ir.FromString(ir.MergeKey), Val: ir.FromString("lit")},
	})
	var buf bytes.Buffer
	if err := Encode(node, &buf, EncodeFormat(format.TonyFormat)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if want := "\"<<\": lit\n"; buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
	got, err := parse.Parse(buf.Bytes())
	if err != nil {
		t.Fatalf("encoded document does not parse: %v", err)
	}
	if n := countMergeKeys(got); n != 0 {
		t.Errorf("a string field came back as %d merge key(s): %q", n, buf.String())
	}
	if f := got.Fields[0]; f.Type != ir.StringType || f.String != ir.MergeKey {
		t.Errorf("field: got type %v %q, want String %q", f.Type, f.String, ir.MergeKey)
	}
}

// Colouring asked `f == ir.MergeKey` after quoting had already made it `"<<"`,
// so the merge colour the LSP maps to a semantic token was never reached.
func TestMergeKeyIsColouredAsOne(t *testing.T) {
	node, err := parse.Parse([]byte("<<: \"{{ tpl }}\"\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	colors := &Colors{
		Default: func(v string, _ ...any) string { return v },
		Map: map[Colorable]func(string, ...any) string{
			{Type: ir.ObjectType, Attr: MergeColor}: func(v string, _ ...any) string {
				return "<merge>" + v + "</merge>"
			},
		},
	}
	if err := Encode(node, &buf, EncodeFormat(format.TonyFormat), EncodeColors(colors)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	if want := "<merge><<</merge>"; !bytes.Contains(buf.Bytes(), []byte(want)) {
		t.Errorf("got %q, want it to contain %q", buf.String(), want)
	}
}

func countMergeKeys(node *ir.Node) int {
	n := 0
	for i, f := range node.Fields {
		if f != nil && f.Type == ir.NullType {
			n++
		}
		if i < len(node.Values) {
			n += countMergeKeys(node.Values[i])
		}
	}
	if len(node.Fields) == 0 {
		for _, v := range node.Values {
			n += countMergeKeys(v)
		}
	}
	return n
}
