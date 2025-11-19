package tony

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

type toolTest struct {
	in, out string
}

var toolTests = []toolTest{
	{
		in:  `1`,
		out: `1`,
	},
	{
		in:  `false`,
		out: `false`,
	},
	{
		in:  `string`,
		out: `string`,
	},
	{
		in:  `[1, 2, 3]`,
		out: "[\n  1\n  2\n  3\n]",
	},
	{
		in:  "f1: a\nf2: b",
		out: "f1: a\nf2: b",
	},
	{
		in:  `[1, [0, 1], 2]`,
		out: "[\n  1\n  [\n    0\n    1\n  ]\n  2\n]",
	},
	//{
	//	in:  `1 # start with 1`,
	//	out: `1 # start with 1`,
	//},
	//{
	//	in:  "# start with 1\n1",
	//	out: "# start with 1\n1",
	//},
	//{
	//	in:  "1\n# start with 1",
	//	out: "1\n# start with 1",
	//},
	//	{
	//		in:  "# f1\nf1: true",
	//		out: "# f1\nf1: true",
	//	},
	//	{
	//		in:  "- # f1\n  f1: true",
	//		out: "- # f1\n  f1: true",
	//	},
	{
		in:  `!eval 1`,
		out: "1",
	},
	{
		in:  `!eval ".[x]"`,
		out: "22",
	},
	{
		in:  `f: !eval .[x]`,
		out: "f: 22",
	},
	{
		in:  `!eval $[x]`,
		out: "\"22\"",
	},
	{
		in: `!eval
f1: !eval $[x]`,
		out: "f1: \"22\"",
	},
	{
		in:  `!eval .[o]`,
		out: "of1: null\nof2:\n- 1\n- two\n- 0.3",
	},
	{
		in:  `!eval $[o]`,
		out: `"{of1: null of2: [1 two 0.3]}"`,
	},
	{
		in: `
!eval
- $[nil]
- .[nil]`,
		out: `
- "null"
- null`,
	},
	{
		in:  `!tovalue.exec "ls /dev/null"`,
		out: "/dev/null",
	},
	{
		in:  `!b64enc.exec "ls /dev/null"`,
		out: "L2Rldi9udWxsCg",
	},
	{
		in: `!tovalue.file testdata/t2-in.yaml`,
		out: `
!yt
metadata:
  labels:
    a: b
    c: !eval d`,
	},
	{
		in: `
field: !script(string) "whereami()"`,

		out: "field: $.field",
	},
	{
		in: `
!script(string) "getpath(whereami())"`,

		out: `getpath(whereami())`,
	},
	{
		in: `
!tovalue.script(string).file testdata/script.xpl`,

		out: `
f1: $
f2: true`,
	},
	{
		in: `
!script(any) "listpath(whereami())"`,

		out: "- listpath(whereami())",
	},
	{
		in: `
!tostring 
- a: b
- c: d`,

		out: `|
  - a: b
  - c: d`,
	},
	{
		in: `
!tovalue.tostring 
- a: b
- c: d`,

		out: "- a: b\n- c: d",
	},
	{
		in: `
!tovalue.tostring 
- a: b
- c: d
- d: !osenv ENV1`,

		out: "- a: b\n- c: d\n- d: ENV2",
	},
}

func TestTool(t *testing.T) {
	tool := DefaultTool()
	tool.Env["nil"] = nil
	tool.Env["x"] = ir.FromInt(22)
	tool.Env["o"] = ir.FromMap(map[string]*ir.Node{
		"of1": ir.Null(),
		"of2": ir.FromSlice([]*ir.Node{ir.FromInt(1), ir.FromString("two"), ir.FromFloat(0.3)}),
	})
	os.Setenv("ENV1", "ENV2")
	for i := range toolTests {
		tt := &toolTests[i]
		yIn, err := parse.Parse([]byte(tt.in))
		if err != nil {
			t.Errorf("couldn't parse `%s`: %v", tt.in, err)
			continue
		}
		yOut, err := tool.Run(yIn)
		if err != nil {
			t.Error(err)
			continue
		}
		buf := bytes.NewBuffer(nil)
		err = encode.Encode(yOut, buf)
		if err != nil {
			t.Errorf("could not encode: %v", err)
			continue
		}
		got := strings.TrimSuffix(buf.String(), "\n")
		want := strings.TrimPrefix(tt.out, "\n")

		t.Logf("%s", got)
		if got != want {
			t.Errorf("got %q want %q", got, want)
			t.Logf("yOut type %s", yOut.Type)
			t.Logf("yOut fields %v", yOut.Fields)
		}
	}
}
