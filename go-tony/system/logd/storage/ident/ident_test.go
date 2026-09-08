package ident

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func node(t *testing.T, src string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %s", src, err)
	}
	return n
}

// One binding renders (f=v); several render <{...}> with the fields sorted, so the same
// identity is spelled the same way whichever order it was declared or written in.
func TestCanonicalSpelling(t *testing.T) {
	for _, tc := range []struct {
		elem   string
		fields []string
		want   string
	}{
		{`{sku: A, qty: 3}`, []string{"sku"}, `(sku=A)`},
		{`{sku: "a b", qty: 3}`, []string{"sku"}, `(sku="a b")`},
		{`{n: 42}`, []string{"n"}, `(n=42)`},
		{`{n: "42"}`, []string{"n"}, `(n="42")`},
		{`{on: true}`, []string{"on"}, `(on=true)`},
		{`{name: jane, n: 2}`, []string{"name", "n"}, `<{n: 2, name: jane}>`},
		{`{name: jane, n: 2}`, []string{"n", "name"}, `<{n: 2, name: jane}>`},
		{`{sku: !important A}`, []string{"sku"}, `(sku=A)`},
		{`{v: a=b}`, []string{"v"}, `(v=a=b)`},
	} {
		n, ok := Of(node(t, tc.elem), tc.fields)
		if !ok {
			t.Errorf("%s under %v: no name", tc.elem, tc.fields)
			continue
		}
		if got := n.Field(); got != tc.want {
			t.Errorf("%s under %v: spelled %q, want %q", tc.elem, tc.fields, got, tc.want)
		}
		back, isName, err := Parse(n.Field())
		if err != nil || !isName {
			t.Errorf("%q does not read back as a name: %v", n.Field(), err)
			continue
		}
		if back.Field() != n.Field() {
			t.Errorf("%q reads back as %q", n.Field(), back.Field())
		}
		if !back.Binds(tc.fields) {
			t.Errorf("%q does not bind %v after reading back", n.Field(), tc.fields)
		}
	}
}

// A number and the string of its digits are different names: the name is the only place
// the key's type survives.
func TestTypeSurvivesInTheName(t *testing.T) {
	a, _ := Of(node(t, `{n: 42}`), []string{"n"})
	b, _ := Of(node(t, `{n: "42"}`), []string{"n"})
	if a.Field() == b.Field() {
		t.Fatalf("42 and \"42\" spell the same name %q", a.Field())
	}
}

// What is not a name: a null key, a missing key, a key that is a container, an element
// that is not an object.
func TestWhatIsNotAName(t *testing.T) {
	for _, tc := range []struct {
		elem   string
		fields []string
	}{
		{`{sku: null, qty: 3}`, []string{"sku"}},
		{`{qty: 3}`, []string{"sku"}},
		{`{sku: {a: 1}}`, []string{"sku"}},
		{`{sku: [1]}`, []string{"sku"}},
		{`[1, 2]`, []string{"sku"}},
		{`{name: jane}`, []string{"name", "n"}},
	} {
		if n, ok := Of(node(t, tc.elem), tc.fields); ok {
			t.Errorf("%s under %v named %q, and should have no name", tc.elem, tc.fields, n.Field())
		}
	}
}

// A field that is spelled as a name but binds a different identity is a query against
// it, not a name under it.
func TestBindsIsExact(t *testing.T) {
	n, _, err := Parse(`(status=open)`)
	if err != nil {
		t.Fatal(err)
	}
	if n.Binds([]string{"sku"}) {
		t.Error("(status=open) binds the identity sku")
	}
	partial, _, err := Parse(`<{name: jane}>`)
	if err != nil {
		t.Fatal(err)
	}
	if partial.Binds([]string{"n", "name"}) {
		t.Error("a partial composite binds the whole identity")
	}
}

// An ordinary field is not a name and is not an error; a field spelled as a name that
// does not read as one is.
func TestParseTellsOrdinaryFromMalformed(t *testing.T) {
	for _, f := range []string{"plain", "9x", "a.b", "()", "(", "<>"} {
		if _, ok, err := Parse(f); ok && err == nil {
			t.Errorf("%q parsed as a name", f)
		}
	}
	for _, f := range []string{"(sku)", "(=A)", "(sku=null)", "<{sku: null}>", "<{a: 1, a: 2}>"} {
		_, ok, err := Parse(f)
		if !ok || err == nil {
			t.Errorf("%q: ok=%v err=%v, want a malformed name", f, ok, err)
		}
	}
}

// Stamp fills a missing key in from the name and refuses one that disagrees: the name is
// authoritative.
func TestStampIsAuthoritative(t *testing.T) {
	n, _, _ := Parse(`(sku=A)`)
	elem := node(t, `{qty: 3}`)
	if err := n.Stamp(elem); err != nil {
		t.Fatal(err)
	}
	if got := ir.Get(elem, "sku"); got == nil || got.String != "A" {
		t.Errorf("sku not filled in: %v", elem)
	}
	if err := n.Stamp(node(t, `{sku: B}`)); err == nil || !strings.Contains(err.Error(), "authoritative") {
		t.Errorf("an element disagreeing with its name was accepted: %v", err)
	}
	if err := n.Stamp(node(t, `{sku: A, qty: 1}`)); err != nil {
		t.Errorf("an element agreeing with its name was refused: %v", err)
	}
}

// New refuses the punctuation the name is made of in a field name.
func TestFieldPunctuation(t *testing.T) {
	for _, f := range []string{"a=b", "a<b", "a>b", ""} {
		if _, err := New(Binding{Field: f, Value: ir.FromString("x")}); err == nil {
			t.Errorf("field %q accepted", f)
		}
	}
}
