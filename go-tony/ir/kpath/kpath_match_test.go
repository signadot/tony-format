package kpath

import "testing"

func mustParse(t *testing.T, s string) *KPath {
	t.Helper()
	kp, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return kp
}

// TestMatches covers the wildcard-aware pattern matcher requested in issue
// 61766xadh: a wildcard segment denotes any concrete segment of the same kind,
// concrete segments compare by value, depth must be equal, and matching is
// one-directional and reflexive.
func TestMatches(t *testing.T) {
	cases := []struct {
		pattern, target string
		want            bool
	}{
		// The issue's table (equal-depth denotation).
		{"review.seq[*]", "review.seq[2]", true},
		{"review.seq[*].onDone", "review.seq[2].onDone", true},
		{"review.*", "review.onDone", true},
		{"review.seq[2]", "review.seq[2]", true}, // concrete == concrete
		{"review", "review", true},               // reflexive
		{"review.seq[*]", "review.seq[*]", true}, // reflexive with wildcard
		{"{*}", "{7}", true},                     // sparse wildcard
		// Depth must match for Matches (denotation, not prefix).
		{"review.seq[2]", "review.seq[2].onDone", false},
		{"review", "review.seq[2]", false},
		{"review.seq[*]", "review.seq[2].onDone", false},
		// Concrete pattern does not match a differing concrete.
		{"review.seq[3]", "review.seq[2]", false},
		{"review.onStart", "review.onDone", false},
		// One-directional: a concrete pattern does not match a wildcard target.
		{"review.seq[2]", "review.seq[*]", false},
		{"review.onDone", "review.*", false},
		// Kind-strict: a dense wildcard never matches a keyed / sparse element.
		{"review.seq[*]", "review.seq(lint)", false},
		{"review.seq[*]", "review.seq{2}", false},
		{"review.*", "review[2]", false},
	}
	for _, tc := range cases {
		p := mustParse(t, tc.pattern)
		o := mustParse(t, tc.target)
		if got := p.Matches(o); got != tc.want {
			t.Errorf("(%q).Matches(%q) = %v, want %v", tc.pattern, tc.target, got, tc.want)
		}
	}
}

// TestMatchesPrefix covers the ancestor-or-equal form: the pattern may be shorter
// than the target and still denote it.
func TestMatchesPrefix(t *testing.T) {
	cases := []struct {
		pattern, target string
		want            bool
	}{
		{"review.seq[*]", "review.seq[2].onDone", true},
		{"review.seq[*]", "review.seq[2]", true}, // equal depth still a prefix
		{"review.*", "review.onDone.at", true},
		{"review", "review.seq[2].onDone", true},
		{"review.seq[3]", "review.seq[2].onDone", false}, // wrong concrete
		{"review.seq[*]", "review.other[2]", false},      // diverges before wildcard
		{"review.seq[*].onDone", "review.seq[2]", false}, // pattern longer than target
	}
	for _, tc := range cases {
		p := mustParse(t, tc.pattern)
		o := mustParse(t, tc.target)
		if got := p.MatchesPrefix(o); got != tc.want {
			t.Errorf("(%q).MatchesPrefix(%q) = %v, want %v", tc.pattern, tc.target, got, tc.want)
		}
	}
}

// TestHasWild distinguishes the whole-path HasWild from the head-segment-only
// Wild — the ask-2 confusion in the issue.
func TestHasWild(t *testing.T) {
	cases := []struct {
		path              string
		hasWild, headWild bool
	}{
		{"review.seq[*]", true, false},   // wildcard is the tail, not the head
		{"review.seq[2]", false, false},  // no wildcard anywhere
		{"*.seq[2]", true, true},         // head wildcard
		{"[*].onDone", true, true},       // head wildcard
		{"review.*.onDone", true, false}, // interior wildcard
	}
	for _, tc := range cases {
		kp := mustParse(t, tc.path)
		if got := kp.HasWild(); got != tc.hasWild {
			t.Errorf("(%q).HasWild() = %v, want %v", tc.path, got, tc.hasWild)
		}
		if got := kp.Wild(); got != tc.headWild {
			t.Errorf("(%q).Wild() = %v, want %v (head-segment only)", tc.path, got, tc.headWild)
		}
	}
}

// TestMatchesDescend covers `..` in a pattern, which matches zero or more target
// segments of any kind — the meaning ListKPath gives it (17hj5ygkh12ks7j7n5n0).
// The cross-check against a real document is TestDescendMatchesWhatListFinds in
// package ir; this is the path arithmetic.
func TestMatchesDescend(t *testing.T) {
	cases := []struct {
		pattern, target string
		want            bool
	}{
		// The issue's case: a pattern that finds a node must say it denotes it.
		{"verse-dev..finding", "verse-dev.drift.finding", true},
		// Zero segments: `..` includes the position itself.
		{"a..x", "a.x", true},
		{"a..x", "a.b.x", true},
		{"a..x", "a.b.c.d.x", true},
		{"..x", "x", true},
		{"..x", "a.b.x", true},
		// Of any kind: a descent crosses arrays and sparse arrays as it does objects.
		{"a..x", "a[0].x", true},
		{"a..x", "a{7}.b.x", true},
		{"a..[0]", "a.b[0]", true},
		// A trailing `..` names the node and everything under it.
		{"a..", "a", true},
		{"a..", "a.b.c", true},
		{"..", "", true},
		{"..", "a.b", true},
		// The rest of the pattern still has to match.
		{"a..x", "a.b.y", false},
		{"a..x", "b.x", false},
		{"a..x", "a.x.b", false},   // x is not the last segment
		{"a..x.y", "a.b.x", false}, // pattern outruns the target
		{"a..[0]", "a.b(0)", false},
		// Wildcards and descents compose.
		{"a..*", "a.b.c", true},
		{"a..b[*]", "a.q.b[3]", true},
		// Several descents.
		{"a..b..c", "a.x.b.y.z.c", true},
		{"a..b..c", "a.b.c", true},
		{"a..b..c", "a.x.c.y.b", false},
		// A target holding a `..` is a query, not a path: nothing denotes it.
		{"a.b", "a..b", false},
		{"a..b", "a..b", false},
		{"a..", "a..", false},
		{"..", "..", false},
	}
	for _, tc := range cases {
		p := mustParse(t, tc.pattern)
		o := mustParse(t, tc.target)
		if got := p.Matches(o); got != tc.want {
			t.Errorf("(%q).Matches(%q) = %v, want %v", tc.pattern, tc.target, got, tc.want)
		}
	}
}

// TestMatchesPrefixDescend covers `..` under the prefix rule: the pattern must be
// consumed, the target need not be, and a `..` still pending where the target ends
// consumes nothing and holds.
func TestMatchesPrefixDescend(t *testing.T) {
	cases := []struct {
		pattern, target string
		want            bool
	}{
		{"a..x", "a.b.x.c.d", true},
		{"a..x", "a.x", true},
		{"a..", "a", true}, // the descent consumes nothing and holds
		{"a..", "a.b.c", true},
		{"a..x", "a.b.y", false},   // the segment after the descent must still match
		{"a..x.y", "a.b.x", false}, // pattern outruns the target
		{"b..x", "a.b.x", false},
		{"a.b", "a..b.c", false}, // a query is not a path, at any depth
	}
	for _, tc := range cases {
		p := mustParse(t, tc.pattern)
		o := mustParse(t, tc.target)
		if got := p.MatchesPrefix(o); got != tc.want {
			t.Errorf("(%q).MatchesPrefix(%q) = %v, want %v", tc.pattern, tc.target, got, tc.want)
		}
	}
}
