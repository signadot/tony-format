package tony

import (
	"strings"
	"testing"

	"github.com/tony-format/tony/encode"
	"github.com/tony-format/tony/ir"
	"github.com/tony-format/tony/parse"
)

type pathTest struct {
	Path  string
	Doc   string
	Res   string
	NoGet bool
}

var pathTests = []pathTest{
	{
		Path: "$",
		Doc:  "null",
		Res:  "null",
	},
	{
		Path: "$.f",
		Doc:  "f: 1",
		Res:  "1",
	},

	{
		Path: "$[0]",
		Doc:  "[1,2,3]",
		Res:  "1",
	},

	{
		Path: "$",
		Doc:  "[1,2,3]",
		Res:  "[\n  1\n  2\n  3\n]",
	},

	{
		Path: "$[1].f",
		Doc:  "[0, {\"f\": 2, \"g\": 3}]",
		Res:  "2",
	},

	{
		Path: "$.f[3]",
		Doc:  `{"a": [1,2], "f": [0,1,2,"three"]}`,
		Res:  "three",
	},

	{
		Path: "$.'f[3]'[2]",
		Doc:  `{"a": [1,2], "f[3]": [0,1,2,"three"]}`,
		Res:  "2",
	},

	{
		Path: "$.'$f[\\'3]'[2]",
		Doc:  `{"a": [1,2], "$f['3]": [0,1,2,"three"]}`,
		Res:  "2",
	},

	{
		NoGet: true,
		Path:  "$[*]",
		Doc:   "[1,2,3]",
		Res:   "- 1\n- 2\n- 3",
	},

	{
		NoGet: true,
		Path:  "$.a[*]",
		Doc:   "b: [1,2,3]",
		Res:   "[]",
	},

	{
		NoGet: true,
		Path:  "$.b[*]",
		Doc:   "b: [1,2,3]",
		Res:   "- 1\n- 2\n- 3",
	},
	{
		NoGet: true,
		Path:  "$.c.d.a",
		Doc:   "a: b\nc:\n  d: 2\n  a: 3",
		Res:   "[]",
	},
	{
		NoGet: true,
		Path:  "$...a",
		Doc:   "a: b\nc:\n  d: 2\n  a: 3",
		Res:   "- b\n- 3",
	},
	{
		NoGet: true,
		Path:  "$.c...a",
		Doc:   "a: b\nc:\n  d: 2\n  a: 3",
		Res:   "- 3",
	},
	{
		NoGet: true,
		Path:  "$.c...x",
		Doc:   "a: b\nc:\n  d: 2\n  a: 3",
		Res:   "[]",
	},
}

func TestPathGet(t *testing.T) {
	for i := range pathTests {
		pathTest := &pathTests[i]
		if pathTest.NoGet {
			continue
		}
		node, err := parse.Parse([]byte(pathTest.Doc))
		if err != nil {
			t.Errorf("# doc\n%s\n---\n# %v\n", pathTest.Doc, err)
			continue
		}
		t.Logf("decoded %q", pathTest.Doc)
		res, err := node.GetPath(pathTest.Path)
		if err != nil {
			t.Error(err)
			continue
		}
		pp, err := ir.ParsePath(pathTest.Path)
		if err != nil {
			t.Error(err)
			continue
		}
		t.Logf("got path %q -> %q", pathTest.Path, pp.String())

		if res == nil {
			t.Error("no result")
			continue
		}
		out := strings.TrimSpace(encode.MustString(res))
		if out != pathTest.Res {
			t.Errorf("got %q want %q", out, pathTest.Res)
			continue
		}
	}
}

func TestPathList(t *testing.T) {
	for i := range pathTests {
		pathTest := &pathTests[i]
		in, err := parse.Parse([]byte(pathTest.Doc))
		if err != nil {
			t.Errorf("# doc\n%s\n---\n# %v\n", pathTest.Doc, err)
			continue
		}
		if !pathTest.NoGet {
			get, err := in.GetPath(pathTest.Path)
			if err != nil {
				t.Error(err)
			}
			lst, err := in.ListPath(nil, pathTest.Path)
			if (get == nil) != (lst == nil) {
				t.Errorf("got != lst on %s for %q %t %t", pathTest.Path,
					pathTest.Doc, get == nil, lst == nil)
				continue
			}
			if get == nil {
				continue
			}
			if len(lst) != 1 {
				t.Errorf("listed too many: %s for %q", pathTest.Path, pathTest.Doc)
				continue
			}
			gs, ls := encode.MustString(get), encode.MustString(lst[0])
			if gs != ls {
				t.Errorf("# get\n%s---\n# lst\n%s", gs, ls)
			}
			continue
		}
		pp, err := ir.ParsePath(pathTest.Path)
		if err != nil {
			t.Error(err)
			continue
		}
		t.Logf("parsed %q to %q\n", pathTest.Path, pp.String())
		lst, err := in.ListPath(nil, pathTest.Path)
		if err != nil {
			t.Error(err)
			continue
		}
		ls := encode.MustString(ir.FromSlice(lst))
		if err != nil {
			t.Error(err)
			continue
		}
		if ls != pathTest.Res {
			t.Errorf("# list gave\n%s\n---\n# want\n%s", ls, pathTest.Res)
		}
	}
}
