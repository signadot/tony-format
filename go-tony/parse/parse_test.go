package parse

import (
	"os"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
)

type parseTest struct {
	in string
	e  error
}

func TestParseOK(t *testing.T) {
	pts := []parseTest{
		{
			in: `null`,
		},
		{
			in: `true`,
		},
		{
			in: `false`,
		},
		{
			in: `22`,
		},
		{
			in: `1e14`,
		},
		{
			in: `"hello"`,
		},
		{
			in: `hello`,
		},
		{
			in: `|
  z
`,
		},
		{
			in: `|
  z
`,
		},
		{
			in: `[a,b]`,
		},
		{
			in: `[a]`,
		},
		{
			in: `[[]]`,
		},
		{
			in: `[a,[b,[c]]]`,
		},
		{
			in: `[[[a],b],c]`,
		},
		{
			in: `!tag a`,
		},
		{
			in: `!tag []`,
		},
		{
			in: `[!tag a]`,
		},
		{
			in: "# comment\n[!tag a]",
		},
		{
			in: "# comment\n[0, !tag a, 1]",
		},
		{
			in: "# comment\n# again\n[0, !tag a, 1]",
		},
		{
			in: `
		- 0 # comment
		- z`,
		},
		{
			in: `
- 0
- # head
  # head 2
  - -42
  - 42 #line
`,
		},
		{
			in: `{}`,
		},
		{
			in: `!tag {}`,
		},
		{
			in: "# comment\n!tag {a: {}}",
		},
		{
			in: "{a: b}",
		},
		{
			in: "{a: b\nc: d\n}",
		},
		{
			in: `{ a: { b: 9 } c: {d: 8} }`,
		},
		{
			in: `{
		a: {b: 9}
		c: {d: 8}
		}`,
		},
		{
			in: `{
		  a: {
		    b: 9
		  }
		  c: {
		    d: 8
		  }
		}`,
		},
		{
			in: `{
		a: {
		  b: 9 }
		c: {
		  d: 8 }
		}`,
		},
		{
			in: `{"a": [1,2], "f[0]": [0,1,2,"three"]}`,
		},
		{
			in: "[0, {\"f\": 2, \"g\": 3}]",
		},
		{
			in: "a: b\nc:\n  d: 2\n  a: 3",
		},
		{
			in: `a: b
c:
  d: e
`,
		},
		{
			in: `
a: b
c:
  e: f`,
		},
		{
			in: `
a:
  b: c
c:
  e: f`,
		},
		{
			in: `
- - a
- - b`,
		},
		{
			in: `
ytool:
  tag: $ytool
  sources:
  - dir: source
  #- exec: kustomize build ../../../../sandboxes/deploy/operator/overlays/production
  #- url: https://raw.githubusercontent.com/signadot/hotrod/refs/heads/main/k8s/base/driver.yaml
  #- dir: source/list.yaml

  #destDir: out
`,
		},
		{
			in: `
"hello"
'yo'`,
		},
	}
	for i := range pts {
		pt := &pts[i]
		node, err := Parse([]byte(pt.in), ParseComments(false))
		if err != nil {
			t.Errorf("# doc\n%s\n# error %v", pt.in, err)
			return
		}
		encode.Encode(node, os.Stdout, encode.EncodeColors(encode.NewColors()), encode.EncodeComments(true))
		t.Logf("\n%s\n", encode.MustString(node))
	}
}

func TestParseMulti(t *testing.T) {
	in := `
doc1: true
---
doc2: false
---
doc3:
- 1
- 2
`
	nodes, err := ParseMulti([]byte(in))
	if err != nil {
		t.Fatalf("ParseMulti failed: %v", err)
	}

	if len(nodes) != 3 {
		t.Fatalf("expected 3 documents, got %d", len(nodes))
	}

	// Verify content
	if nodes[0].Fields[0].String != "doc1" {
		t.Errorf("doc1 key mismatch")
	}
	if nodes[1].Fields[0].String != "doc2" {
		t.Errorf("doc2 key mismatch")
	}
	if len(nodes[2].Values[0].Values) != 2 {
		t.Errorf("doc3 array length mismatch")
	}
}

func TestBadParse(t *testing.T) {
}
