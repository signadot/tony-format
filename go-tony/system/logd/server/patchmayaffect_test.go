package server

import "testing"

// TestPatchMayAffect_QuotedSegments guards the read-amplification pre-filter
// against the quoted-segment regression: segment matching must mirror the
// authoritative read (splitKPath + verbatim field.String comparison), NOT
// kpath.Parse, which strips quotes. A digit-first decision id like "9reprokind"
// is stored quoted; unquoting it here makes the watcher's birth event miss its
// own path and get silently filtered out — the §8 stall.
func TestPatchMayAffect_QuotedSegments(t *testing.T) {
	cases := []struct {
		name  string
		delta string // the committed patch
		path  string // the watched path
		want  bool
	}{
		// Digit-first (quoted) key: the write reaches the watched path — must NOT filter.
		{
			name:  "digitFirst hit",
			delta: `{verse: {vote: {"9reprokind": {alice: "yes"}}}}`,
			path:  `verse.vote."9reprokind"`,
			want:  true,
		},
		// Digit-first sibling: a different quoted key, genuinely untouched — filter it.
		{
			name:  "digitFirst sibling miss",
			delta: `{verse: {vote: {"9reprokind": {alice: "yes"}}}}`,
			path:  `verse.vote."8otherkind"`,
			want:  false,
		},
		// Letter-first (unquoted) key: the pre-existing passing case.
		{
			name:  "letterFirst hit",
			delta: `{verse: {vote: {reprokind: {alice: "yes"}}}}`,
			path:  `verse.vote.reprokind`,
			want:  true,
		},
		{
			name:  "letterFirst sibling miss",
			delta: `{verse: {vote: {reprokind: {alice: "yes"}}}}`,
			path:  `verse.vote.otherkind`,
			want:  false,
		},
		// Watch the whole vote subtree: any child write reaches it.
		{
			name:  "ancestor watch hit",
			delta: `{verse: {vote: {"9reprokind": {alice: "yes"}}}}`,
			path:  `verse.vote`,
			want:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			delta := mustParse(tc.delta)
			if got := patchMayAffect(delta, tc.path); got != tc.want {
				t.Fatalf("patchMayAffect(%s, %q) = %v, want %v", tc.delta, tc.path, got, tc.want)
			}
		})
	}
}

// TestPatchMayAffect_OpTagFallThrough covers the branch the doc comment leans on and the
// existing cases never reach: an op tag ANYWHERE along the path must fall through to the
// authoritative recompute, because an operation can change a subtree that structural
// navigation would say it misses. The scoped watcher's soundness rests on this -- the
// filter is allowed to be conservative, never to be wrong.
//
// Presentation tags are not operations and must not trigger it, or the filter never
// filters anything.
func TestPatchMayAffect_OpTagFallThrough(t *testing.T) {
	cases := []struct {
		name  string
		delta string
		path  string
		want  bool
		why   string
	}{
		{
			name:  "!replace at an ancestor",
			delta: `{a: !replace {from: {b: 1}, to: {c: 2}}}`,
			path:  "a.b",
			want:  true,
			why:   "the replacement removes a.b, which no structural walk of the patch shows",
		},
		{
			name:  "!delete at an ancestor",
			delta: `{a: !delete {b: 1}}`,
			path:  "a.b",
			want:  true,
			why:   "deleting a takes a.b with it",
		},
		{
			name:  "!key at an ancestor",
			delta: `{items: !key(sku) [{sku: "A", q: 1}]}`,
			path:  "items.whatever",
			want:  true,
			why:   "an identity merge places elements by key, not by structure",
		},
		{
			name:  "!arraydiff at an ancestor",
			delta: `{a: !arraydiff {0: !insert 1}}`,
			path:  "a.b",
			want:  true,
			why:   "an array diff rewrites positions the walk cannot follow",
		},
		{
			name:  "op tag at the watched path itself",
			delta: `{a: {b: !delete {x: 1}}}`,
			path:  "a.b",
			want:  true,
			why:   "the op is exactly at the path",
		},
		{
			name:  "op tag on a sibling only",
			delta: `{a: {sib: !delete {x: 1}}}`,
			path:  "a.b",
			want:  false,
			why:   "a plain merge that never reaches a.b; the op is off the path",
		},
		{
			name:  "presentation tag is not an op",
			delta: `{a: {sib: {x: 1}}}`,
			path:  "a.b",
			want:  false,
			why:   "!bracket and friends must not defeat the filter",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			patch := mustParse(tc.delta)
			if got := patchMayAffect(patch, tc.path); got != tc.want {
				t.Errorf("patchMayAffect(%s, %q) = %v, want %v -- %s",
					tc.delta, tc.path, got, tc.want, tc.why)
			}
		})
	}
}
