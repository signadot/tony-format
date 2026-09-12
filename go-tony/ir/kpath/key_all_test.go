package kpath

import "testing"

// `(*)` is the wildcard of the keyed kind: every element of a keyed array, by identity.
// It is what a rule over a keyed array is written with (logd retention), and it is
// kind-strict like the other wildcards -- [*] does not denote a keyed element, and (*)
// does not denote a positional one.
func TestKeyAllParsesAndRoundTrips(t *testing.T) {
	for _, tc := range []struct {
		path  string
		want  string
		wild  bool
		kinds []EntryKind
	}{
		{path: "a(*)", want: "a(*)", wild: true, kinds: []EntryKind{FieldEntry, KeyEntry}},
		{path: "a(*).b", want: "a(*).b", wild: true, kinds: []EntryKind{FieldEntry, KeyEntry, FieldEntry}},
		{path: "(*)", want: "(*)", wild: true, kinds: []EntryKind{KeyEntry}},
		{path: "a(x)", want: "a(x)", wild: false, kinds: []EntryKind{FieldEntry, KeyEntry}},
		// A key that is literally `*` is still sayable, quoted, and still canonical.
		{path: `a("*")`, want: `a("*")`, wild: false, kinds: []EntryKind{FieldEntry, KeyEntry}},
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
			var kinds []EntryKind
			for x := kp; x != nil; x = x.Next {
				kinds = append(kinds, x.EntryKind())
			}
			if len(kinds) != len(tc.kinds) {
				t.Fatalf("kinds = %v, want %v", kinds, tc.kinds)
			}
			for i := range kinds {
				if kinds[i] != tc.kinds[i] {
					t.Errorf("kind[%d] = %v, want %v", i, kinds[i], tc.kinds[i])
				}
			}
			// The segment survives the split-and-rejoin every path helper does.
			if got := SplitAll(tc.path); len(got) != len(tc.kinds) {
				t.Errorf("SplitAll = %v, want %d segments", got, len(tc.kinds))
			}
			if parent, last := RSplit(tc.path); Join(parent, last) != tc.want {
				t.Errorf("RSplit/Join = %q + %q, want %q", parent, last, tc.want)
			}
		})
	}
}

func TestKeyAllMatchesByKindOnly(t *testing.T) {
	pat := mustParse(t, "a(*)")
	for _, tc := range []struct {
		target string
		want   bool
	}{
		{"a(x)", true},
		{"a(*)", true},
		{"a[0]", false},
		{"a[*]", false},
		{"a.x", false},
		{"a{0}", false},
	} {
		if got := pat.Matches(mustParse(t, tc.target)); got != tc.want {
			t.Errorf("a(*) matches %s = %v, want %v", tc.target, got, tc.want)
		}
	}
	// A concrete key does not denote the wildcard.
	if mustParse(t, "a(x)").Matches(pat) {
		t.Error("a(x) matches a(*), want not")
	}
	// Equality tells the two apart, and the order is total over them.
	if _, eq := mustParse(t, "a(x)").AncestorOrEqual(pat); eq {
		t.Error("a(x) equal to a(*), want not")
	}
	if c := mustParse(t, "a(x)").Compare(pat); c >= 0 {
		t.Errorf("a(x) compared to a(*) = %d, want negative", c)
	}
	if c := pat.Compare(mustParse(t, "a(x)")); c <= 0 {
		t.Errorf("a(*) compared to a(x) = %d, want positive", c)
	}
	if c := pat.Compare(mustParse(t, "a(*)")); c != 0 {
		t.Errorf("a(*) compared to a(*) = %d, want 0", c)
	}
}
