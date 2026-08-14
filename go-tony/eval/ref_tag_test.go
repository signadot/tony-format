package eval_test

import (
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/parse"
)

// match is the whole path a pattern travels -- expansion and then matching --
// because the tag was lost between the two and each half was correct on its own.
func match(t *testing.T, doc, pattern string) bool {
	t.Helper()
	d, err := parse.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse %q: %v", doc, err)
	}
	p, err := parse.Parse([]byte(pattern))
	if err != nil {
		t.Fatalf("parse %q: %v", pattern, err)
	}
	got, err := tony.Match(d, p)
	if err != nil {
		t.Fatalf("match %q against %q: %v", pattern, doc, err)
	}
	return got
}

// TestRefKeepsItsTag: expanding .[var] REPLACES the node it stands for, and the
// node being replaced is the one wearing the tag. Dropping it turned !not .[x]
// into .[x] -- the opposite verdict, returned cleanly with no error, which is
// why it went unnoticed (issue cv90ehkvh12krm4sfxn0).
//
// !let is the natural way to write "this field differs from that value", and the
// differs half was the half that lied.
func TestRefKeepsItsTag(t *testing.T) {
	const doc = "{base: abc123, state: open}"

	for _, tc := range []struct {
		name, pattern string
		want          bool
	}{
		{"a bare reference still matches", `!let {let: [{tip: abc123}], in: {base: .[tip]}}`, true},
		{"a literal !not still negates", `{base: !not zzz999}`, true},
		{"!not over a reference that differs", `!let {let: [{tip: zzz999}], in: {base: !not .[tip]}}`, true},
		{"!not over a reference that matches", `!let {let: [{tip: abc123}], in: {base: !not .[tip]}}`, false},

		// Every operator worn over a reference has the same shape.
		{"!glob over a reference that matches", `!let {let: [{p: "abc*"}], in: {base: !glob .[p]}}`, true},
		{"!glob over a reference that differs", `!let {let: [{p: "zzz*"}], in: {base: !glob .[p]}}`, false},
		{"!irtype over a reference of the same kind", `!let {let: [{t: ""}], in: {base: !irtype .[t]}}`, true},
		{"!irtype over a reference of another kind", `!let {let: [{t: 0}], in: {base: !irtype .[t]}}`, false},
		{"!or over a reference to a list of alternatives", `!let {let: [{alts: [abc123, zzz]}], in: {base: !or .[alts]}}`, true},
		{"!or over a reference whose alternatives all differ", `!let {let: [{alts: [yyy, zzz]}], in: {base: !or .[alts]}}`, false},

		// A reference standing for a whole object, not a scalar.
		{"a reference to an object", `!let {let: [{o: {base: abc123}}], in: .[o]}`, true},
		{"!not over a reference to an object", `!let {let: [{o: {base: zzz}}], in: !not .[o]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := match(t, doc, tc.pattern); got != tc.want {
				t.Fatalf("%s matched %v, want %v", tc.pattern, got, tc.want)
			}
		})
	}
}

// TestRefTagComposesWithTheValuesOwn: the reference's tag goes ON TOP of whatever
// the bound value wears rather than replacing it, so !not over a value that is
// itself a !glob means not-glob -- what the two operators spelled in one place
// would mean. Overwriting would silently discard the value's own operator.
func TestRefTagComposesWithTheValuesOwn(t *testing.T) {
	const doc = "{base: abc123}"

	for _, tc := range []struct {
		name, pattern string
		want          bool
	}{
		{"the value's own !glob applies", `!let {let: [{p: !glob "abc*"}], in: {base: .[p]}}`, true},
		{"the value's own !glob can fail", `!let {let: [{p: !glob "zzz*"}], in: {base: .[p]}}`, false},
		{"!not over a matching !glob", `!let {let: [{p: !glob "abc*"}], in: {base: !not .[p]}}`, false},
		{"!not over a failing !glob", `!let {let: [{p: !glob "zzz*"}], in: {base: !not .[p]}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := match(t, doc, tc.pattern); got != tc.want {
				t.Fatalf("%s matched %v, want %v", tc.pattern, got, tc.want)
			}
		})
	}
}
