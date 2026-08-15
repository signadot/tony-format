package server

import (
	"slices"
	"testing"
)

// A mount path is built a segment at a time and then read back a segment at a
// time -- routing, splitting and prefix matching all work on the fields. The two
// have to agree at every depth.
//
// They did not: kpath.Join read its prefix as a single SEGMENT, so accumulating
// collapsed everything before the last step into one field name. fieldsToKPath
// turned [a b c] into `"a.b".c`, a path of two segments whose first field is
// literally "a.b" -- a different place in the document, reached without an error
// because the result parses and round trips as itself. Anything three levels
// deep was mounted, routed and split against the wrong path.
func TestFieldsToKPathRoundTrip(t *testing.T) {
	for _, fields := range [][]string{
		{},
		{"a"},
		{"a", "b"},
		{"a", "b", "c"},
		{"a", "b", "c", "d", "e"},
		// fields that need quoting: the dot in the name is not a separator
		{"example.com", "tls"},
		{"a", "example.com", "1.2.3"},
		{"with space", "b", "c"},
	} {
		p := fieldsToKPath(fields)
		back, err := pathFields(p)
		if err != nil {
			t.Errorf("%q built %q, which does not parse: %v", fields, p, err)
			continue
		}
		if !slices.Equal(back, fields) {
			t.Errorf("%q built %q, which reads back as %q", fields, p, back)
		}
	}
}

// TestFieldsToKPathSeparates: the built path names as many segments as it was
// given, which is the property the round trip above rests on.
func TestFieldsToKPathSeparates(t *testing.T) {
	p := fieldsToKPath([]string{"a", "b", "c"})
	if p != "a.b.c" {
		t.Errorf("built %q, want %q", p, "a.b.c")
	}
}
