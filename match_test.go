package tony

import (
	"bytes"
	"testing"

	"github.com/signadot/tony-format/tony/encode"
	"github.com/signadot/tony-format/tony/parse"
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
	{
		in:    "a: b\nc: d",
		match: "!let\nlet:\n- idMatch: \"b\"\nin:\n  a: .idMatch",
		res:   true,
	},
	{
		in:    "a: b\nc: d",
		match: "!let\nlet:\n- idMatch: \"e\"\nin:\n  a: .idMatch",
		res:   false,
	},
	{
		in:    "a: b\nc: d",
		match: "!let\nlet:\n- idMatch: \"b\"\n- otherMatch: \"d\"\nin:\n  a: .idMatch\n  c: .otherMatch",
		res:   true,
	},
	{
		in:    "a: .something",
		match: "!let\nlet:\n- idMatch: \"b\"\nin:\n  a: \\.something",
		res:   true,
	},
	{
		in:    "a: \\.something",
		match: "!let\nlet:\n- idMatch: \"b\"\nin:\n  a: \\\\.something",
		res:   true,
	},
	{
		in:    "a: b",
		match: "!let\nlet:\n- idMatch: \"b\"\nin:\n  a: \\.idMatch",
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

type trimTest struct {
	doc    string
	match  string
	result string
}

var trimTests = []trimTest{
	{
		doc:    "a: b\nc: d\ne: f",
		match:  "a: b\nc: d",
		result: "a: b\nc: d",
	},
	{
		doc:    "a: b\nc: d",
		match:  "a: b",
		result: "a: b",
	},
	{
		doc:    "a: b\nc: d",
		match:  "c: d",
		result: "c: d",
	},
	{
		doc:    "a:\n  x: 1\n  y: 2\nb: 3",
		match:  "a:\n  x: 1\nb: 3",
		result: "a:\n  x: 1\nb: 3",
	},
	{
		doc:    "- a: 1\n- b: 2\n- c: 3",
		match:  "- a: 1\n- c: 3",
		result: "- a: 1\n- c: 3",
	},
	{
		doc:    "a: b",
		match:  "a: b\nc: null",
		result: "a: b",
	},
	{
		doc:    "a: b",
		match:  "a: b",
		result: "a: b",
	},
	{
		doc:    "hello",
		match:  "hello",
		result: "hello",
	},
	{
		doc:    "42",
		match:  "42",
		result: "42",
	},
}

func TestTrim(t *testing.T) {
	for i, tt := range trimTests {
		doc, err := parse.Parse([]byte(tt.doc))
		if err != nil {
			t.Errorf("test %d: could not parse doc: %v\n%s", i, err, tt.doc)
			continue
		}
		match, err := parse.Parse([]byte(tt.match))
		if err != nil {
			t.Errorf("test %d: could not parse match: %v\n%s", i, err, tt.match)
			continue
		}
		expected, err := parse.Parse([]byte(tt.result))
		if err != nil {
			t.Errorf("test %d: could not parse expected result: %v\n%s", i, err, tt.result)
			continue
		}

		result := Trim(match, doc)

		// Compare by encoding both and checking string equality
		var resultBuf, expectedBuf bytes.Buffer
		if err := encode.Encode(result, &resultBuf); err != nil {
			t.Errorf("test %d: could not encode result: %v", i, err)
			continue
		}
		if err := encode.Encode(expected, &expectedBuf); err != nil {
			t.Errorf("test %d: could not encode expected: %v", i, err)
			continue
		}

		resultStr := resultBuf.String()
		expectedStr := expectedBuf.String()

		if resultStr != expectedStr {
			t.Errorf("test %d: trim mismatch\nDoc: %s\nMatch: %s\nGot: %s\nWant: %s",
				i, tt.doc, tt.match, resultStr, expectedStr)
		}

		// Also verify tag is preserved
		if doc.Tag != "" && result.Tag != doc.Tag {
			t.Errorf("test %d: tag not preserved, got %q want %q", i, result.Tag, doc.Tag)
		}
	}
}
