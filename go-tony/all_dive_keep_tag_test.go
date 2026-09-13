package tony

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func parseT(t *testing.T, s string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(s))
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return n
}

func encodeT(t *testing.T, n *ir.Node) string {
	t.Helper()
	var b strings.Builder
	if err := encode.Encode(n, &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// !all and !dive patch every child of a container; the container is still what it
// was. They rebuilt it without its tag, so a !key(name) list came back unkeyed and
// the next keyed write found an unkeyed list, and !all rebuilt an object from a
// string-keyed map, which lost a merge key (4ynqp7wqh12krg32msn0 item 12).
func TestAllAndDiveKeepTheContainerTag(t *testing.T) {
	doc := parseT(t, "items: !key(name) [{name: a, v: 1}, {name: b, v: 1}]")
	for _, patch := range []string{"items: !all {v: 2}", "items: !dive [{match: {v: 1}, patch: {v: 2}}]"} {
		out, err := Patch(doc, parseT(t, patch))
		if err != nil {
			t.Fatalf("%s: %v", patch, err)
		}
		items := ir.Get(out, "items")
		if !ir.TagHas(items.Tag, "!key") {
			t.Errorf("%s: items tag %q, want the !key(name) it had", patch, items.Tag)
		}
		for _, v := range items.Values {
			if got := ir.Get(v, "v"); got == nil || got.Int64 == nil || *got.Int64 != 2 {
				t.Errorf("%s: element %s not patched: %v", patch, encodeT(t, v), got)
			}
		}
	}

	// A tagged element under !all keeps its tag, and a merge key stays a merge key.
	doc = parseT(t, "m: {<<: {x: 1}, a: !t1 {y: 1}}")
	out, err := Patch(doc, parseT(t, "m: !all {z: 1}"))
	if err != nil {
		t.Fatal(err)
	}
	got := encodeT(t, out)
	if !strings.Contains(got, "<<:") {
		t.Errorf("merge key lost:\n%s", got)
	}
	if !strings.Contains(got, "!t1") {
		t.Errorf("element tag lost:\n%s", got)
	}
}
