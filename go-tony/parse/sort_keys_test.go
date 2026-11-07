package parse

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/token"
)

// SortKeys answers string-keyed objects in key order, at every depth; an object
// with a merge key keeps the order it was written in; a comment moves with its
// value. Without the option the written order is kept (q47aszzwh12kr7x6xxn0).
func TestSortKeys(t *testing.T) {
	// want is written in sorted order and parsed without SortKeys, so the two are
	// compared as the encoder lays them out.
	for _, tc := range []struct {
		name, src, want string
		opts            []ParseOption
	}{
		{"nested", "b: 1\na:\n  z: [{y: 1, x: 2}]\n  c: 3\n",
			"a:\n  c: 3\n  z: [{x: 2, y: 1}]\nb: 1\n", nil},
		{"merge key keeps order", "b: 1\n<<: m\na: {d: 1, c: 2}\n",
			"b: 1\n<<: m\na: {c: 2, d: 1}\n", nil},
		{"comments move with values", "b: 1 # by b\n# on a\na: 2 # by a\n# after\n",
			"# on a\na: 2 # by a\nb: 1 # by b\n# after\n", []ParseOption{ParseComments(true)}},
		{"json", `{"b": 1, "a": 2}`, `{"a": 2, "b": 1}`, []ParseOption{ParseJSON()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := Parse([]byte(tc.src), append(tc.opts, SortKeys())...)
			if err != nil {
				t.Fatal(err)
			}
			checkParents(t, n)
			w, err := Parse([]byte(tc.want), tc.opts...)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := encoded(t, n), encoded(t, w); got != want {
				t.Errorf("got\n%s\nwant\n%s", got, want)
			}
		})
	}

	if n, err := Parse([]byte("b: 1\na: 2\n")); err != nil || n.Fields[0].String != "b" {
		t.Errorf("without SortKeys the written order is not kept: %v", err)
	}

	docs, err := ParseMulti([]byte("b: 1\na: 2\n---\nd: 1\nc: 2\n"), SortKeys())
	if err != nil || len(docs) != 2 || docs[0].Fields[0].String != "a" || docs[1].Fields[0].String != "c" {
		t.Errorf("ParseMulti does not sort each document: %v", err)
	}

	// The key and value nodes are moved, not rebuilt: each has its position.
	pos := map[*ir.Node]*token.Pos{}
	n, err := Parse([]byte("b: 1\na: 2\n"), SortKeys(), ParsePositions(pos))
	if err != nil {
		t.Fatal(err)
	}
	for i := range n.Fields {
		if pos[n.Fields[i]] == nil || pos[n.Values[i]] == nil {
			t.Errorf("field %q lost its position", n.Fields[i].String)
		}
	}
}

func encoded(t *testing.T, n *ir.Node) string {
	t.Helper()
	var buf strings.Builder
	if err := encode.Encode(n, &buf, encode.EncodeComments(true)); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// checkParents fails t where a node's Parent or ParentIndex does not name its
// place in the tree.
func checkParents(t *testing.T, n *ir.Node) {
	t.Helper()
	for i, v := range n.Values {
		if v.Parent != n || v.ParentIndex != i {
			t.Errorf("value %d of %s: parent %p index %d, want %p %d", i, n.Type, v.Parent, v.ParentIndex, n, i)
		}
		if i < len(n.Fields) && (n.Fields[i].Parent != n || n.Fields[i].ParentIndex != i) {
			t.Errorf("field %q: parent index %d, want %d", n.Fields[i].String, n.Fields[i].ParentIndex, i)
		}
		checkParents(t, v)
	}
}
