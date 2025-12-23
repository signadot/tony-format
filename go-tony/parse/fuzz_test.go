package parse

import (
	"bytes"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
)

func FuzzParse(f *testing.F) {
	// Seed with various valid inputs
	seeds := []string{
		// Primitives
		`null`,
		`true`,
		`false`,
		`42`,
		`3.14`,
		`-1e10`,
		`""`,
		`"hello"`,
		`hello`,

		// Arrays
		`[]`,
		`[1, 2, 3]`,
		`[a, b, c]`,
		`[[nested], [arrays]]`,

		// Objects
		`{}`,
		`{foo: bar}`,
		`{a: 1, b: 2}`,
		`{nested: {object: value}}`,

		// Tags
		`!tag value`,
		`!tag.chain value`,
		`!tag(arg) value`,
		`!tag(a,b,c) value`,
		`!irtype ""`,
		`!or [a, b]`,
		`!and [a, b]`,
		`!not null`,

		// Mixed
		`{users: [{name: alice}, {name: bob}]}`,
		`!schema {define: {x: !irtype ""}, accept: .[x]}`,

		// Strings with special chars
		`"with\nnewline"`,
		`"with\ttab"`,
		`"with \"quotes\""`,

		// Block strings
		`|
  line1
  line2`,
		`>
  folded
  text`,

		// Comments
		`# comment
value`,
		`value # trailing`,

		// Merge syntax
		`{<<: base, extra: value}`,

		// Raw references
		`.[ref]`,
		`$[expr]`,

		// Edge cases
		`---`,
		`...`,
	}

	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		// Primary target: parse should not panic
		node, err := Parse(data)
		if err != nil {
			return // parse errors are expected for random input
		}
		if node == nil {
			return // empty input can return nil node
		}

		// Secondary: if parse succeeds, encode should not panic
		var buf bytes.Buffer
		err = encode.Encode(node, &buf)
		if err != nil {
			return // encode errors are acceptable
		}

		// Tertiary: round-trip parse should not panic
		Parse(buf.Bytes())
	})
}
