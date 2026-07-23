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
