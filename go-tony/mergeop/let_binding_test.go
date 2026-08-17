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
			name:    "a name no let binds, from inside a nested one",
			pattern: `!let {let: [{a: 1}], in: !let {let: [{b: 2}], in: {sha: .[nope]}}}`,
			want:    "does not bind .[nope]",
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

// A let inside a let is the ordinary shape, and it works the way a reader of the
// two lines expects: a binding list is evaluated in the ENCLOSING scope, the body
// in the inner one, and an inner name shadows an outer one of the same name.
//
// It used to lie. The outer expansion ran over the whole body, including the inner
// let's, and eval.ExpandIR resolves a name it does not know to NULL -- so the
// inner's references were blanked before its op ever ran, and since a null pattern
// matches anything, the nested match passed on every document there is.
func TestLetNests(t *testing.T) {
	doc := mustParseNode(t, `{sha: aaa111}`)

	for _, tc := range []struct {
		name, pattern string
		matches       bool
	}{
		{
			name:    "the inner binding is what the body sees",
			pattern: `!let {let: [{a: 1}], in: !let {let: [{b: aaa111}], in: {sha: .[b]}}}`,
			matches: true,
		},
		{
			// the case the old code could not tell from the one above
			name:    "and it can fail to match",
			pattern: `!let {let: [{a: 1}], in: !let {let: [{b: zzz}], in: {sha: .[b]}}}`,
			matches: false,
		},
		{
			name:    "an inner body may use an outer binding",
			pattern: `!let {let: [{o: aaa111}], in: !let {let: [{b: 1}], in: {sha: .[o]}}}`,
			matches: true,
		},
		{
			name:    "an inner BINDING is evaluated in the outer scope",
			pattern: `!let {let: [{o: aaa111}], in: !let {let: [{b: .[o]}], in: {sha: .[b]}}}`,
			matches: true,
		},
		{
			name:    "an inner binding shadows an outer one",
			pattern: `!let {let: [{t: zzz}], in: !let {let: [{t: aaa111}], in: {sha: .[t]}}}`,
			matches: true,
		},
		{
			name:    "and the outer value does not leak past the shadow",
			pattern: `!let {let: [{t: aaa111}], in: !let {let: [{t: zzz}], in: {sha: .[t]}}}`,
			matches: false,
		},
		{
			// the binding reads the outer t, then shadows it for the body
			name:    "a shadowing binding may read what it shadows",
			pattern: `!let {let: [{t: aaa111}], in: !let {let: [{t: .[t]}], in: {sha: .[t]}}}`,
			matches: true,
		},
		{
			name:    "three deep",
			pattern: `!let {let: [{a: 1}], in: !let {let: [{b: 2}], in: !let {let: [{c: aaa111}], in: {sha: .[c]}}}}`,
			matches: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := tony.Match(doc, mustParseNode(t, tc.pattern))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tc.matches {
				t.Errorf("matched=%v, want %v", ok, tc.matches)
			}
		})
	}
}
