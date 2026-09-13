package stream

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// A float goes out so that it reads back as a float. The encoder wrote 1.0 as 1,
// which decodes as an integer, so a stored ratio: 1.0 reached every logd client as
// 1 (4ynqp7wqh12krg32msn0 item 4). Inf and NaN have no number syntax and are
// refused, as encode refuses them.
func TestWriteFloatReadsBackAsAFloat(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want string
	}{
		{1.0, "1.0"},
		{0, "0.0"},
		{-2, "-2.0"},
		{1.5, "1.5"},
		{1e21, "1e+21"},
	} {
		var buf bytes.Buffer
		enc, err := NewEncoder(&buf, WithBrackets())
		if err != nil {
			t.Fatal(err)
		}
		if err := enc.WriteFloat(tc.in); err != nil {
			t.Fatalf("WriteFloat(%v): %v", tc.in, err)
		}
		if got := strings.TrimSpace(buf.String()); got != tc.want {
			t.Errorf("WriteFloat(%v) wrote %q, want %q", tc.in, got, tc.want)
		}
		dec, err := NewDecoder(strings.NewReader(buf.String()), WithBrackets())
		if err != nil {
			t.Fatal(err)
		}
		n, err := ReadDocument(dec)
		if err != nil {
			t.Fatalf("read back %q: %v", buf.String(), err)
		}
		if n.Type != ir.NumberType || n.Float64 == nil || *n.Float64 != tc.in {
			t.Errorf("%q read back as %v (Int64 %v, Float64 %v), want the float %v", buf.String(), n.Type, n.Int64, n.Float64, tc.in)
		}
	}
	for _, bad := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		var buf bytes.Buffer
		enc, _ := NewEncoder(&buf, WithBrackets())
		if err := enc.WriteFloat(bad); err == nil {
			t.Errorf("WriteFloat(%v) wrote %q, want a refusal", bad, buf.String())
		}
	}
}

// An integer in radix notation is read as the parser reads it: its value, with the
// notation as its tag. The decoder parsed in base 10 only, so {a: 0x1f} was
// refused, and a refused document ends the session reading it
// (4ynqp7wqh12krg32msn0 item 5).
func TestDecoderReadsRadixIntegers(t *testing.T) {
	dec, err := NewDecoder(strings.NewReader("{a: 0x1f, b: 0o644, c: 0b101, d: -0x10, e: !mine 0x2a, f: 7}"), WithBrackets())
	if err != nil {
		t.Fatal(err)
	}
	n, err := ReadDocument(dec)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, tc := range []struct {
		key  string
		want int64
		tags []string
	}{
		{"a", 31, []string{ir.HexTag}},
		{"b", 420, []string{ir.OctTag}},
		{"c", 5, []string{ir.BinTag}},
		{"d", -16, []string{ir.HexTag}},
		{"e", 42, []string{ir.HexTag, "!mine"}},
		{"f", 7, nil},
	} {
		v := ir.Get(n, tc.key)
		if v == nil || v.Int64 == nil || *v.Int64 != tc.want {
			t.Errorf("%s: got %v, want %d", tc.key, v, tc.want)
			continue
		}
		for _, tag := range tc.tags {
			if !ir.TagHas(v.Tag, tag) {
				t.Errorf("%s: tag %q, want it to carry %s", tc.key, v.Tag, tag)
			}
		}
		if tc.tags == nil && v.Tag != "" {
			t.Errorf("%s: tag %q, want none", tc.key, v.Tag)
		}
	}
}
