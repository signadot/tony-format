package tony

import (
	"bytes"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

type matchTest struct {
	in    string
	match string
	res   bool
}

var matchTests = []matchTest{
	{
		in:    `1`,
		match: `1`,
		res:   true,
	},
	{
		in:    `0`,
		match: `1`,
		res:   false,
	},
	{
		in:    `- 1`,
		match: `- 1`,
		res:   true,
	},
	{
		in:    `[]`,
		match: `[]`,
		res:   true,
	},
	{
		in:    `- 1`,
		match: `- 2`,
		res:   false,
	},
	{
		in:    `- 1`,
		match: `hello`,
		res:   false,
	},
	{
		in:    "a: b\nc: d",
		match: "a: b",
		res:   true,
	},
	{
		in:    "a: b",
		match: "a: b\nc: d",
		res:   false,
	},
	{
		in:    "a: b",
		match: "null",
		res:   true,
	},
	{
		in:    "hello",
		match: "!glob 'h*o'",
		res:   true,
	},
	{
		in:    "hello",
		match: "!not.glob 'h*o'",
		res:   false,
	},
	{
		in:    "a: b\nc: d",
		match: "!and\n- a: b\n- c: d",
		res:   true,
	},
	{
		in:    "a: b\nc: d",
		match: "!not.and\n- a: b\n- c: d",
		res:   false,
	},
	{
		in:    "a: b\nc: d",
		match: "!and\n- a: !glob '*'\n- c: d",
		res:   true,
	},
	{
		in:    "- a: b\n- a: c",
		match: "!key(a)\n- a: b",
		res:   true,
	},
	{
		in:    "- a: b\n- a: c",
		match: "!key(a)\n- a: d",
		res:   false,
	},
	{
		in:    "- a: b\n  b: ccc\n- a: c",
		match: "!key(a)\n- a: b\n  b: !glob 'c*'",
		res:   true,
	},
	{
		in:    "a: b\nc:\n- d:\n    x-foo: 1",
		match: "!subtree.field.glob 'x-*'",
		res:   true,
	},
	{
		in:    "a: b\nc:\n- d:\n    x-foo: 1",
		match: "!not.subtree.field.glob 'x-*'",
		res:   false,
	},
	{
		in:    "a: b\nc: d\ne:\n- 1\n- true\n- 42",
		match: "!subtree 42",
		res:   true,
	},
	{
		in:    "a: b\nc: !mytag d\ne:\n- 1\n- true\n- 42",
		match: "c: !tag.glob my*",
		res:   true,
	},
	{
		in:    "a: b\nc: !mytag d\ne:\n- 1\n- true\n- 42",
		match: "c: !not.tag.glob my*",
		res:   false,
	},
}

func TestMatchY(t *testing.T) {
	for i := range matchTests {
		mt := &matchTests[i]
		doc, err := parse.Parse([]byte(mt.in))
		if err != nil {
			t.Errorf("# could not decode\n%s\n# error %v\n", mt.in, err)
		}
		m, err := parse.Parse([]byte(mt.match))
		if err != nil {
			t.Errorf("# could not decode\n%s\n# error %v\n", mt.match, err)
			return
		}
		buf := bytes.NewBuffer(nil)
		if err := encode.Encode(m, buf); err != nil {
			t.Error(err)
			return
		}
		t.Logf("# match\n---\n%s", buf.String())
		res, err := Match(doc, m)
		if err != nil {
			t.Error(err)
			continue
		}
		if res != mt.res {
			t.Errorf("match %q on %q: got %t want %t", mt.in, mt.match, res, mt.res)
			continue
		}
	}
}
