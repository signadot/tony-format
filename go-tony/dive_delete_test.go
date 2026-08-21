package tony

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A `!dive` whose patch deletes what it matched killed the process:
//
//	o patch -y '{rules: !dive [{match: {resources: [namespaces]},
//	                            patch: !delete null}]}' role.yaml
//	panic: nil pointer dereference, ir.FromSlice, ir/node.go:261
//
// Dive rebuilds each container from what its children came back as, and a child
// the patch deleted comes back nil -- which is how this library says "no node",
// not an error. Both constructors then dereference it to set the parent link:
// FromSlice does y.Parent = res and FromKeyValsAt does kv.Val.Parent = res.
//
// The delete itself was never broken -- `{name: !delete null}` applied to a
// matched map works, because that one is applied by the ordinary patch function
// and never round trips a nil through Dive -- and neither was matching, since
// `!nullify` in the same position matches the same rule and returns a node.
//
// The patches below are written in block form so the results read as documents.
// Style follows the patch, as it does for any patch: written `[{...}]`, as the
// report's command line was, the result comes back bracketed too.
func TestDiveDeletesWhatItMatched(t *testing.T) {
	tests := []struct {
		name      string
		doc, tony string
		want      string
	}{{
		name: "an element of a list",
		doc: `
rules:
- resources:
  - namespaces
  verbs:
  - get
- resources:
  - pods
  verbs:
  - list`,
		tony: `
rules: !dive
- match:
    resources:
    - namespaces
  patch: !delete null`,
		want: `
rules:
- resources:
  - pods
  verbs:
  - list`,
	}, {
		name: "a field whose value is an object",
		doc: `
spec:
  a:
    drop: yes
  b:
    keep: yes`,
		tony: `
spec: !dive
- match:
    drop: yes
  patch: !delete null`,
		want: `
spec:
  b:
    keep: yes`,
	}, {
		name: "every element there is",
		doc: `
rules:
- resources:
  - namespaces
- resources:
  - pods`,
		tony: `
rules: !dive
- match:
    resources: !all null
  patch: !delete null`,
		want: `rules: []`,
	}, {
		name: "a scalar element",
		doc: `
xs:
- 1
- 2
- 3`,
		tony: `
xs: !dive
- match: 2
  patch: !delete null`,
		want: `
xs:
- 1
- 3`,
	}, {
		// The rules after the one that deleted have nothing left to be about:
		// do() carried the nil forward as the document to match the next rule
		// against.
		name: "a rule after the one that deleted",
		doc: `
rules:
- resources:
  - namespaces
  verbs:
  - get
- resources:
  - pods
  verbs:
  - list`,
		tony: `
rules: !dive
- match:
    resources:
    - namespaces
  patch: !delete null
- match:
    verbs: !all null
  patch:
    seen: true`,
		want: `
rules:
- resources:
  - pods
  seen: true
  verbs:
  - list`,
	}, {
		name: "nested, both levels",
		doc: `
a:
  b:
    drop: yes
  c:
    d:
      drop: yes
    e: keep`,
		tony: `
a: !dive
- match:
    drop: yes
  patch: !delete null`,
		want: `
a:
  c:
    e: keep`,
	}, {
		// The shape the report arrived in, bracketed patch and all.
		name: "as written on a command line",
		doc: `
rules:
- resources: [namespaces]
  verbs: [get]
- resources: [pods]
  verbs: [list]`,
		tony: `rules: !dive [{match: {resources: [namespaces]}, patch: !delete null}]`,
		want: `
rules: [
  {
    resources: [
        pods
      ]
    verbs: [
        list
      ]
  }
]`,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, err := parse.Parse([]byte(test.doc))
			if err != nil {
				t.Fatalf("doc: %s", err)
			}
			patch, err := parse.Parse([]byte(test.tony))
			if err != nil {
				t.Fatalf("patch: %s", err)
			}
			got, err := Patch(doc, patch)
			if err != nil {
				t.Fatalf("patch: %s", err)
			}
			if got == nil {
				t.Fatal("the patch answered with no document")
			}
			if s := strings.TrimSpace(encode.MustString(got)); s != strings.TrimSpace(test.want) {
				t.Errorf("got\n%s\nwant\n%s", s, strings.TrimSpace(test.want))
			}
		})
	}
}

// The whole document deleted from under a dive: Dive answers nil, and Patch wrote
// the parent links onto it without looking.
func TestDiveDeletesTheRoot(t *testing.T) {
	doc, err := parse.Parse([]byte("a: 1"))
	if err != nil {
		t.Fatalf("doc: %s", err)
	}
	patch, err := parse.Parse([]byte("!dive\n- match:\n    a: 1\n  patch: !delete null\n"))
	if err != nil {
		t.Fatalf("patch: %s", err)
	}
	got, err := Patch(doc, patch)
	if err != nil {
		return // refusing is a fine answer; dying is not
	}
	if got != nil {
		t.Errorf("the document was deleted but came back as %s", encode.MustString(got))
	}
}
