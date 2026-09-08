package server

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A watch decides whether a commit reaches its path by projecting the stored delta onto
// it (api.ProjectDelta), and the projection matches segments the way the read does: a
// digit-first key like "9reprokind" is spelled quoted in a path and stored bare, and the
// two must meet -- otherwise a watcher's birth event misses its own path and is silently
// filtered out.
func TestTheProjectionMatchesQuotedSegments(t *testing.T) {
	cases := []struct {
		name  string
		delta string // the committed patch
		path  string // the watched path
		want  bool   // the delta reaches the path
	}{
		{"digitFirst hit", `{verse: {vote: {"9reprokind": {alice: "yes"}}}}`, `verse.vote."9reprokind"`, true},
		{"digitFirst sibling miss", `{verse: {vote: {"9reprokind": {alice: "yes"}}}}`, `verse.vote."8otherkind"`, false},
		{"letterFirst hit", `{verse: {vote: {reprokind: {alice: "yes"}}}}`, `verse.vote.reprokind`, true},
		{"letterFirst sibling miss", `{verse: {vote: {reprokind: {alice: "yes"}}}}`, `verse.vote.otherkind`, false},
		{"ancestor watch hit", `{verse: {vote: {"9reprokind": {alice: "yes"}}}}`, `verse.vote`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at, _, ok := api.ProjectDelta(mustParse(tc.delta), tc.path)
			if got := ok && at != nil; got != tc.want {
				t.Fatalf("ProjectDelta(%s, %q) reaches = %v, want %v", tc.delta, tc.path, got, tc.want)
			}
		})
	}
}
