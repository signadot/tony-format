package kpath

import "testing"

// `..` is the any-depth segment. It is a QUERY segment: it names the nodes at any
// depth rather than a step to one, so a path holding one cannot be a stored path,
// and the things which keep stored paths refuse it rather than pretend.
//
// The spelling was free by an accident worth stating: `a..x` parsed as a field
// whose name is EMPTY, and the canonical way to write that field is `a.""`, which
// is what String emits for it. So nothing canonical changed meaning.
func TestDescendParsesAndRoundTrips(t *testing.T) {
	for _, tc := range []struct {
		path  string
		want  string
		wild  bool
		kinds []EntryKind
	}{
		{path: "..x", want: "..x", wild: true, kinds: []EntryKind{DescendEntry, FieldEntry}},
		{path: "a..x", want: "a..x", wild: true, kinds: []EntryKind{FieldEntry, DescendEntry, FieldEntry}},
		{path: "a..", want: "a..", wild: true, kinds: []EntryKind{FieldEntry, DescendEntry}},
		{path: "a..b.c", want: "a..b.c", wild: true},
		{path: "a..[0]", want: "a..[0]", wild: true},
		{path: "a.b", want: "a.b", wild: false, kinds: []EntryKind{FieldEntry, FieldEntry}},
		// An empty field is still sayable, and still canonical, in quotes.
		{path: `a."".x`, want: `a."".x`, wild: false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			kp, err := Parse(tc.path)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := kp.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
			if got := kp.HasWild(); got != tc.wild {
				t.Errorf("HasWild() = %v, want %v", got, tc.wild)
			}
			if tc.kinds == nil {
				return
			}
			var kinds []EntryKind
			for x := kp; x != nil; x = x.Next {
				kinds = append(kinds, x.EntryKind())
			}
			if len(kinds) != len(tc.kinds) {
				t.Fatalf("kinds %v, want %v", kinds, tc.kinds)
			}
			for i := range kinds {
				if kinds[i] != tc.kinds[i] {
					t.Errorf("segment %d kind %v, want %v", i, kinds[i], tc.kinds[i])
				}
			}
		})
	}
}

// A descent spans depths, and segment matching asks about one segment against
// one. It answers no rather than a plausible yes: whoever matches a path holding
// a descent has to walk it.
func TestDescendDoesNotSegmentMatch(t *testing.T) {
	desc, _ := Parse("..")
	field, _ := Parse("x")
	all, _ := Parse("*")
	for _, tc := range []struct {
		name     string
		pat, tgt *KPath
	}{
		{"descent against a field", desc, field},
		{"field against a descent", field, desc},
		{"wildcard against a descent", all, desc},
		{"descent against a descent", desc, desc},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if segmentMatches(tc.pat, tc.tgt) {
				t.Errorf("%q matched %q as one segment", tc.pat, tc.tgt)
			}
		})
	}
}
