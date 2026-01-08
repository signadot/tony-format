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
		{
			in: `{"null": null}`,
		},
		{
			in: `{ "null": null }`,
		},
		{
			in: `object(t): !and
- .[object]
- !all.t null`,
		},
		{
			in: `define:
  "null": !irtype null
  object(t): !and
  - .[object]
  - !all.t null`,
		},
		{
			in: `
#
# Tony Format base schema definitions.
#
context: tony-format/context
  
signature:
  name: tony-base
define:
  bool: !irtype true
  "null": !irtype null
  number: !irtype 1
  int: !and
  - .[number]
  - int: !not null
  object: !irtype {}
  object(t): !and
  - .[object]
  - !all.t null`,
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

func TestParseMultiWithNullKey(t *testing.T) {
	in := `
define:
  "null": !irtype null
---
define:
  "null": !irtype null
  object(t): !and
  - .[object]
  - !all.t null
`
	nodes, err := ParseMulti([]byte(in))
	if err != nil {
		t.Fatalf("ParseMulti failed: %v", err)
	}

	if len(nodes) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(nodes))
	}
}

func TestBadParse(t *testing.T) {
}

// TestParseLeadingDocSep tests that Parse handles YAML files starting with ---
// This is issue #106: controller-gen produces YAML files that start with ---
func TestParseLeadingDocSep(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{
			name: "leading doc sep",
			in:   "---\nkey: value",
		},
		{
			name: "leading doc sep with newline",
			in:   "---\n\nkey: value",
		},
		{
			name: "multiple leading doc seps",
			in:   "---\n---\nkey: value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := Parse([]byte(tt.in), ParseYAML())
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
			if node == nil {
				t.Fatal("Parse returned nil node")
			}
			// Verify the content was parsed correctly
			if len(node.Fields) == 0 || node.Fields[0].String != "key" {
				t.Errorf("Expected key 'key', got fields: %v", node.Fields)
			}
		})
	}
}

// TestAssociateCommentsEmptyValues tests that associateComments handles
// comment nodes with empty Values arrays without panicking.
// This is issue #105: panic when parsing build.tony with comments
func TestAssociateCommentsEmptyValues(t *testing.T) {
	// This test verifies the fix for issue #105
	// The exact input that triggers the issue may vary, but the fix ensures
	// that empty Values arrays are handled gracefully
	inputs := []string{
		"# comment\n[]",
		"# comment\n{}",
		"# comment only",
		"[]\n# trailing comment",
	}

	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			// This should not panic
			_, err := Parse([]byte(in), ParseComments(true))
			// We don't check the error - we just want to ensure no panic
			_ = err
		})
	}
}

func TestParseBaseTony(t *testing.T) {
	data, err := os.ReadFile("../schema/base.tony")
	if err != nil {
		t.Skipf("base.tony not found: %v", err)
		return
	}
	
	node, err := Parse(data)
	if err != nil {
		t.Fatalf("Failed to parse base.tony: %v", err)
	}
	
	if node == nil {
		t.Fatal("Parsed node is nil")
	}
	
	t.Logf("Successfully parsed base.tony")
}

func TestParseSchemaTony(t *testing.T) {
	data, err := os.ReadFile("../schema/schema.tony")
	if err != nil {
		t.Skipf("schema.tony not found: %v", err)
		return
	}
	
	nodes, err := ParseMulti(data)
	if err != nil {
		t.Fatalf("Failed to parse schema.tony: %v", err)
	}
	
	if len(nodes) == 0 {
		t.Fatal("No nodes parsed from schema.tony")
	}
	
	t.Logf("Successfully parsed schema.tony: %d documents", len(nodes))
}
