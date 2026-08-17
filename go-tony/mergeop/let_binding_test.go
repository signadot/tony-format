package mergeop_test

import (
	"strings"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// !let binds names and matches with them, and the two ways it went wrong were the
// two ways a match can lie: it brought the process down on a shape it should have
// refused, and it matched EVERYTHING on a name it did not know.
func TestLetRefusesWhatItCannotBind(t *testing.T) {
	doc := mustParseNode(t, `{sha: aaa111}`)

	for _, tc := range []struct {
		name, pattern string
		want          string // substring of the error, or "" for no error
		matches       bool
	}{
		{
			// panic: index out of range [0] with length 0
			name:    "a binding that binds nothing",
			pattern: `!let {let: [{}], in: {sha: aaa111}}`,
			want:    "binds nothing",
		},
		{
			name:    "a binding which is not an object",
			pattern: `!let {let: [3], in: {sha: aaa111}}`,
			want:    "a binding is an object",
		},
		{
			// matched every document there is, silently: an unbound reference
			// expanded to null, and a null pattern matches anything
			name:    "a name the let does not bind",
			pattern: `!let {let: [{t: x}], in: {sha: .[nope]}}`,
			want:    "does not bind .[nope]",
		},
		{
			// the outer expansion reaches the inner body and blanks its names,
			// which used to make the inner match pass vacuously
			name:    "a nested let, whose names this one cannot know",
			pattern: `!let {let: [{a: 1}], in: !let {let: [{b: zzz}], in: {sha: .[b]}}}`,
			want:    "does not bind .[b]",
		},
		{
			name:    "a bound name that matches",
			pattern: `!let {let: [{t: aaa111}], in: {sha: .[t]}}`,
			matches: true,
		},
		{
			name:    "a bound name that does not",
			pattern: `!let {let: [{t: zzz}], in: {sha: .[t]}}`,
			matches: false,
		},
		{
			// every field binds; reading Fields[0] dropped the rest in silence
			name:    "the second field of a binding",
			pattern: `!let {let: [{a: zzz, b: aaa111}], in: {sha: .[b]}}`,
			matches: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pat := mustParseNode(t, tc.pattern)
			ok, err := tony.Match(doc, pat)
			if tc.want != "" {
				if err == nil {
					t.Fatalf("no error, matched=%v; want an error saying %q", ok, tc.want)
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Errorf("error %q does not say %q", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tc.matches {
				t.Errorf("matched=%v, want %v", ok, tc.matches)
			}
		})
	}
}

func mustParseNode(t *testing.T, s string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(s))
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return n
}
