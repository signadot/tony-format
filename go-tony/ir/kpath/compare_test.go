package kpath

import "testing"

// Compare is an order, and every keyed segment compared equal to every other: a(x)
// and a(y) were one path to anything sorting by it, and `..` against a key fell
// through to the same 0 (addsgv1yh12kszdxmdn0). A key orders by its text, `..`
// sits after every key, and every other kind keeps the place it had.
func TestCompareOrdersEverySegmentKind(t *testing.T) {
	// Ascending. The kinds other than keys and `..` are in the order Compare has
	// always given them.
	order := []string{
		"a", "b",
		"[0]", "[1]",
		"{0}", "{1}",
		"(x)", "(y)",
		"..",
		"{*}",
		"[*]",
		"*",
	}
	for i, si := range order {
		for j, sj := range order {
			pi, err := Parse(si)
			if err != nil {
				t.Fatalf("parse %q: %v", si, err)
			}
			pj, err := Parse(sj)
			if err != nil {
				t.Fatalf("parse %q: %v", sj, err)
			}
			want := 0
			switch {
			case i < j:
				want = -1
			case i > j:
				want = 1
			}
			if got := pi.Compare(pj); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", si, sj, got, want)
			}
		}
	}
}

// The segment that differs decides, wherever it is.
func TestCompareKeysDeeperInAPath(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"a(x).b", "a(y).a", -1},
		{"a(y).a", "a(x).b", 1},
		{"a(x).b", "a(x).b", 0},
		{"a(x).b", "a(x).c", -1},
		{"a..b", "a..c", -1},
		{"a(x).b", "a..b", -1},
		{"a..b", "a(x).b", 1},
	} {
		pa, err := Parse(tc.a)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.a, err)
		}
		pb, err := Parse(tc.b)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.b, err)
		}
		if got := pa.Compare(pb); got != tc.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
