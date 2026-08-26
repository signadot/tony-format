package mergeop_test

import (
	"strings"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
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

// The binding list had the hole the BODY had already been closed for: an unbound
// .[name] in it expanded to null and said nothing.
//
// A top-level let's bindings were the one list nothing checked. A NESTED let's
// were checked, by the enclosing let's walk over its expanded body -- so the same
// misspelling was refused one level in and accepted at the top, which is the level
// everything is written at.
//
// It is worse as a patch than as a match, and the match was bad enough: a null
// pattern matched every document there is, while a null patch is WRITTEN, so the
// field the body meant to set gets nulled out.
//
// A cycle was not silent, it was fatal -- `let: [{a: .[a]}]` expanded itself until
// the stack ran out and took the process down.
func TestLetRefusesABindingItCannotEvaluate(t *testing.T) {
	for _, tc := range []struct {
		name, pattern, want string
	}{{
		// matched everything; as a patch, wrote {x: null}
		name:    "a name nobody binds",
		pattern: `!let {let: [{v: .[nope]}], in: {x: .[v]}}`,
		want:    `does not bind .[nope], named by binding "v"`,
	}, {
		name:    "a name nobody binds, inside a binding's value",
		pattern: `!let {let: [{v: {inner: .[nope]}}], in: {x: .[v]}}`,
		want:    `does not bind .[nope]`,
	}, {
		// fatal error: stack overflow
		name:    "a binding which names itself",
		pattern: `!let {let: [{a: .[a]}], in: {x: .[a]}}`,
		want:    "cycle: a -> a",
	}, {
		name:    "two bindings which name each other",
		pattern: `!let {let: [{a: .[b]}, {b: .[a]}], in: {x: .[a]}}`,
		want:    "cycle: a -> b -> a",
	}, {
		name:    "a cycle three long",
		pattern: `!let {let: [{a: .[b]}, {b: .[c]}, {c: .[a]}], in: {x: .[a]}}`,
		want:    "cycle: a -> b -> c -> a",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			doc := mustParseNode(t, `{x: 0}`)
			pat := mustParseNode(t, tc.pattern)

			// Both directions: the match answered true on every document, the patch
			// wrote the null into the one it was given.
			if ok, err := tony.Match(doc, pat); err == nil {
				t.Errorf("match: no error, matched=%v; want an error saying %q", ok, tc.want)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("match: error %q does not say %q", err, tc.want)
			}
			got, err := tony.Patch(doc, pat)
			if err == nil {
				t.Fatalf("patch: no error, result %s; want an error saying %q", encode.MustString(got), tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("patch: error %q does not say %q", err, tc.want)
			}
		})
	}
}

// What a binding list MAY say. A binding names another of its own let's bindings
// and order does not come into it, which is why the refusal above is a cycle check
// and not a rule about writing them in dependency order.
//
// The nested cases are the ones that were refused: the enclosing let walked into an
// inner binding list knowing nothing about what that let binds, so `.[a]` beside
// `{a: 5}` was reported as a name nobody binds.
func TestLetBindingsMayNameTheirOwn(t *testing.T) {
	for _, tc := range []struct {
		name, pattern string
		want          int64
	}{{
		name:    "a later binding names an earlier one",
		pattern: `!let {let: [{a: 5}, {b: .[a]}], in: {x: .[b]}}`,
		want:    5,
	}, {
		name:    "an earlier binding names a later one",
		pattern: `!let {let: [{b: .[a]}, {a: 5}], in: {x: .[b]}}`,
		want:    5,
	}, {
		name:    "both in one binding item",
		pattern: `!let {let: [{a: 5, b: .[a]}], in: {x: .[b]}}`,
		want:    5,
	}, {
		name:    "a chain of three",
		pattern: `!let {let: [{a: 5}, {b: .[a]}, {c: .[b]}], in: {x: .[c]}}`,
		want:    5,
	}, {
		name:    "a nested let's binding names its own sibling",
		pattern: `!let {let: [{o: 1}], in: !let {let: [{a: 5}, {b: .[a]}], in: {x: .[b]}}}`,
		want:    5,
	}, {
		name:    "a nested let's binding names an outer one",
		pattern: `!let {let: [{o: 5}], in: !let {let: [{b: .[o]}], in: {x: .[b]}}}`,
		want:    5,
	}, {
		// Where both scopes bind the name, the ENCLOSING one wins: a binding list
		// is read in the scope around it, and the inner `a` shadows only for the
		// inner BODY. So this list reads differently from the identical one at top
		// level, where `b` gets the sibling 5 because there is no enclosing scope
		// to get it from. Pinned as it stands rather than changed here -- nothing
		// in this fix touches it.
		name:    "an outer name beats the inner let's own binding of it",
		pattern: `!let {let: [{a: 9}], in: !let {let: [{a: 5}, {b: .[a]}], in: {x: .[b]}}}`,
		want:    9,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tony.Patch(mustParseNode(t, `{x: 0}`), mustParseNode(t, tc.pattern))
			if err != nil {
				t.Fatalf("patch: %v", err)
			}
			x, err := got.GetPath("$.x")
			if err != nil || x == nil {
				t.Fatalf("x is gone: %v\n%s", err, encode.MustString(got))
			}
			if x.Int64 == nil || *x.Int64 != tc.want {
				t.Errorf("x = %s, want %d", encode.MustString(x), tc.want)
			}
		})
	}
}
