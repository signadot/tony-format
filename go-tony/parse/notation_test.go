package parse

import (
	"bytes"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/ir"
)

func encodeAs(t *testing.T, n *ir.Node, f format.Format) (string, error) {
	t.Helper()
	var b bytes.Buffer
	err := encode.Encode(n, &b, encode.EncodeFormat(f))
	return strings.TrimRight(b.String(), "\n"), err
}

// A number written in a non-decimal notation keeps that notation through encode -> parse,
// because the notation rides as a presentation tag rather than as part of the value.
func TestNotationRoundTrip(t *testing.T) {
	for _, src := range []string{
		"k: 0x1f", "k: 0o644", "k: 0b1010", "k: -0x1f", "k: 0xdeadbeef",
		"k: 1e9", "k: 2.5e-3", "k: -1e9",
		// no notation to keep
		"k: 31", "k: 420", "k: 1.5", "k: 100.0", "k: -1",
	} {
		n, err := Parse([]byte(src + "\n"))
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		got, err := encodeAs(t, n, format.TonyFormat)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if got != src {
			t.Errorf("%q came back as %q", src, got)
		}
	}
}

// Notation is not the value. 0x1f is 31: it compares equal, hashes equal, and a diff
// between them is about the notation rather than about the number.
func TestNotationIsNotValue(t *testing.T) {
	for _, c := range []struct{ a, b string }{
		{"k: 0x1f", "k: 31"},
		{"k: 0o644", "k: 420"},
		{"k: 0b1010", "k: 10"},
		{"k: -0x1f", "k: -31"},
		{"k: 1e9", "k: 1000000000.0"},
	} {
		na, err := Parse([]byte(c.a + "\n"))
		if err != nil {
			t.Fatalf("%q: %v", c.a, err)
		}
		nb, err := Parse([]byte(c.b + "\n"))
		if err != nil {
			t.Fatalf("%q: %v", c.b, err)
		}
		va, vb := ir.Get(na, "k"), ir.Get(nb, "k")
		// The tag differs, which is the point; the value must not.
		sa, sb := va.Clone(), vb.Clone()
		sa.Tag, sb.Tag = ir.StripPresentation(sa.Tag), ir.StripPresentation(sb.Tag)
		if !sa.DeepEqual(sb) {
			t.Errorf("%q and %q are not the same value", c.a, c.b)
		}
		if sa.Hash() != sb.Hash() {
			t.Errorf("%q and %q hash apart: %d vs %d", c.a, c.b, sa.Hash(), sb.Hash())
		}
		if !ir.IsPresentation(strings.SplitN(va.Tag, ".", 2)[0]) && va.Tag != "" {
			t.Errorf("%q carries %q, which is not a presentation tag", c.a, va.Tag)
		}
	}
}

// JSON has no radix notation, so the notation is dropped and the decimal value written.
// Without that the tag reaches writeTagIfPresent and the encode fails outright with
// "cannot encode tags in json".
func TestNotationDroppedInJSON(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"k: 0x1f", `31`},
		{"k: 0o644", `420`},
		{"k: 0b1010", `10`},
		{"k: -0x1f", `-31`},
	} {
		n, err := Parse([]byte(c.src + "\n"))
		if err != nil {
			t.Fatalf("%q: %v", c.src, err)
		}
		got, err := encodeAs(t, n, format.JSONFormat)
		if err != nil {
			t.Errorf("%q: %v", c.src, err)
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%q encoded to JSON as %q, want it to contain %q", c.src, got, c.want)
		}
		if strings.Contains(got, "0x") || strings.Contains(got, "0o") || strings.Contains(got, "0b") {
			t.Errorf("%q leaked a radix prefix into JSON: %q", c.src, got)
		}
	}
}

// YAML reads the prefixed forms, so they are kept there. That is the point of writing
// 0o644 rather than 0644: to a YAML 1.1 reader a bare 0644 is 420 and under the YAML 1.2
// core schema it is 644, while 0o644 is 420 to both.
func TestNotationKeptInYAML(t *testing.T) {
	for _, src := range []string{"k: 0x1f", "k: 0o644", "k: 0b1010"} {
		n, err := Parse([]byte(src + "\n"))
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		got, err := encodeAs(t, n, format.YAMLFormat)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if got != src {
			t.Errorf("%q encoded to YAML as %q", src, got)
		}
	}
}

// A key cannot carry a tag, so a key written in a non-decimal notation has nowhere to
// record it and would come back silently as decimal. docs/tony.md restricts integer keys
// to base-10, so these are refused rather than quietly rewritten.
func TestRadixKeyRejected(t *testing.T) {
	for _, src := range []string{
		"{0x1f: v}\n",
		"{0o10: v}\n",
		"{0b1: v}\n",
	} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%q parsed; a radix integer key should be refused", src)
		}
	}
	// The decimal forms of the same keys are fine.
	for _, src := range []string{"{31: v}\n", "{8: v}\n", "0: hello\n13: other\n"} {
		if _, err := Parse([]byte(src)); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}

// A notation tag composes with a tag the document already carries rather than replacing
// it, and the value's own tag survives the round trip.
func TestNotationComposesWithTag(t *testing.T) {
	n, err := Parse([]byte("k: !mytag 0x1f\n"))
	if err != nil {
		t.Fatal(err)
	}
	v := ir.Get(n, "k")
	if !ir.TagHas(v.Tag, ir.HexTag) {
		t.Errorf("tag %q lost the notation", v.Tag)
	}
	if !ir.TagHas(v.Tag, "!mytag") {
		t.Errorf("tag %q lost the document's own tag", v.Tag)
	}
	if got := ir.StripPresentation(v.Tag); got != "!mytag" {
		t.Errorf("StripPresentation(%q) = %q, want %q", v.Tag, got, "!mytag")
	}
	got, err := encodeAs(t, n, format.TonyFormat)
	if err != nil {
		t.Fatal(err)
	}
	if got != "k: !mytag 0x1f" {
		t.Errorf("came back as %q", got)
	}
}
