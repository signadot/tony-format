package mergeop_test

import (
	"strings"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
)

// The case the operators were asked for: a quota entry, and "used is at least
// 80% of limit" written in the pattern rather than computed by the source.
func TestCompareQuota(t *testing.T) {
	for _, tc := range []struct {
		doc  string
		want bool
	}{
		{`{limit: 550, used: 471, month: "2026-09"}`, true},
		// 0.8 * 550 is exactly 440: the boundary is ON it, not beside it
		{`{limit: 550, used: 440}`, true},
		{`{limit: 550, used: 439}`, false},
		{`{limit: 550, used: 439.99}`, false},
		{`{limit: 550.0, used: 440}`, true},
	} {
		got, err := tony.Match(mustParseNode(t, tc.doc),
			mustParseNode(t, `{used: !ge(0.8).get-path(root) limit}`))
		if err != nil {
			t.Fatalf("%s: %v", tc.doc, err)
		}
		if got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.doc, got, tc.want)
		}
	}
}

func TestCompareMatches(t *testing.T) {
	for _, tc := range []struct {
		doc, pattern string
		want         bool
	}{
		{`3`, `!lt 4`, true},
		{`4`, `!lt 4`, false},
		{`4`, `!le 4`, true},
		{`5`, `!le 4`, false},
		{`5`, `!gt 4`, true},
		{`4`, `!gt 4`, false},
		{`4`, `!ge 4`, true},
		{`3`, `!ge 4`, false},
		{`-1`, `!lt 0`, true},

		// by value across int and float, where equality is type-exact
		{`3`, `!lt 3.5`, true},
		{`3.0`, `!le 3`, true},
		{`3.0`, `!ge 3`, true},
		{`3.0`, `3`, false},
		// exact, not float: 0.1 + 0.2 rounding does not decide anything here,
		// and the float 0.1 is a hair above the decimal 1/10
		{`0.1`, `!le 0.1`, true},
		{`0.1`, `!gt(0.1) 1`, true},

		// a scale on a literal is the same arithmetic
		{`40`, `!ge(0.5) 80`, true},
		{`39`, `!ge(0.5) 80`, false},

		// a node which is not a number fails the pattern
		{`"3"`, `!lt 4`, false},
		{`null`, `!lt 4`, false},
		{`{a: 1}`, `!lt 4`, false},
		{`"3"`, `!not.ge 4`, true},

		// composition
		{`{a: {b: 5}}`, `!at(a.b).ge 3`, true},
		{`{a: {b: 2}}`, `!at(a.b).ge 3`, false},
		{`{a: {}}`, `!at(a.b).ge 3`, false},
		{`{a: {}}`, `!not.at(a.b).ge 3`, true},
		{`{xs: [4, 5, 6]}`, `!at(xs[*]).gt 3`, true},
		{`{xs: [4, 2, 6]}`, `!at(xs[*]).gt 3`, false},

		// a path relative to the node met reads below it
		{`{spec: {replicas: 3}, status: {replicas: 2}}`,
			`{status: {replicas: !lt.get-path(root) spec.replicas}}`, true},
		{`{spec: {replicas: 3}, status: {replicas: 3}}`,
			`{status: {replicas: !lt.get-path(root) spec.replicas}}`, false},
	} {
		got, err := tony.Match(mustParseNode(t, tc.doc), mustParseNode(t, tc.pattern))
		if err != nil {
			t.Errorf("%s against %s: %v", tc.doc, tc.pattern, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s against %s: got %v, want %v", tc.doc, tc.pattern, got, tc.want)
		}
	}
}

// An operand which cannot be compared against is an error, whether that is
// found where the pattern is built or when the match runs.
func TestCompareErrors(t *testing.T) {
	for _, tc := range []struct {
		doc, pattern, want string
	}{
		{`3`, `!lt "4"`, "expects a number"},
		{`3`, `!lt null`, "expects a number"},
		{`3`, `!lt {a: 1}`, "expects a number"},
		{`3`, `!lt.glob "4*"`, "got !glob"},
		{`3`, `!lt.list-path(root) xs`, "got !list-path"},
		{`3`, `!lt(x) 4`, "scale is not a number"},
		{`3`, `!lt(1,2) 4`, "at most 1 arg"},
		{`"x"`, `!lt "2026-10-01"`, "not an RFC 3339 time"},
		{`"x"`, `!lt "yesterday"`, "not an RFC 3339 time"},
		{`"x"`, `!lt(0.5) "2026-10-01T00:00:00Z"`, "a time has no scale"},
		{`{at: "2026-10-01T00:00:00Z", due: "2026-10-02T00:00:00Z"}`,
			`{at: !lt(0.5).get-path(root) due}`, "has no scale"},
		{`{xs: [1]}`, `{xs: [!lt.get-path(root) xs[*]]}`, "set of nodes"},
		// the decided reading: a missing operand is an error, not a no-match
		{`{used: 3}`, `{used: !lt.get-path(root) limit}`, "names nothing"},
		{`{used: 3, limit: "x"}`, `{used: !lt.get-path(root) limit}`, "not an RFC 3339 time"},
		{`{used: 3, limit: null}`, `{used: !lt.get-path(root) limit}`, "not a number"},
	} {
		_, err := tony.Match(mustParseNode(t, tc.doc), mustParseNode(t, tc.pattern))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s against %s: got %v, want an error saying %q", tc.doc, tc.pattern, err, tc.want)
		}
	}
}

// A comparison is a question about a document, so a patch cannot use one.
func TestCompareDoesNotPatch(t *testing.T) {
	_, err := tony.Patch(mustParseNode(t, `{a: 1}`), mustParseNode(t, `{a: !lt 4}`))
	if err == nil {
		t.Errorf("a patch with !lt applied")
	}
}

// An RFC 3339 string is a time, and times order by instant rather than as text.
func TestCompareTimes(t *testing.T) {
	for _, tc := range []struct {
		doc, pattern string
		want         bool
	}{
		{`"2026-09-30T23:59:59Z"`, `!lt "2026-10-01T00:00:00Z"`, true},
		{`"2026-10-01T00:00:00Z"`, `!lt "2026-10-01T00:00:00Z"`, false},
		{`"2026-10-01T00:00:00Z"`, `!le "2026-10-01T00:00:00Z"`, true},
		{`"2026-10-01T00:00:01Z"`, `!gt "2026-10-01T00:00:00Z"`, true},

		// one instant in two offsets: as text, "02" > "00" and this would fail
		{`"2026-10-01T02:00:00+02:00"`, `!le "2026-10-01T00:00:00Z"`, true},
		{`"2026-10-01T02:00:00+02:00"`, `!ge "2026-10-01T00:00:00Z"`, true},
		{`"2026-10-01T01:00:00+02:00"`, `!lt "2026-10-01T00:00:00Z"`, true},
		// a fraction of a second: as text, "." < "Z" and this would hold
		{`"2026-10-01T00:00:00.5Z"`, `!gt "2026-10-01T00:00:00Z"`, true},

		// against another field
		{`{at: "2026-10-01T00:00:00Z", due: "2026-10-02T00:00:00Z"}`,
			`{at: !lt.get-path(root) due}`, true},
		{`{at: "2026-10-03T00:00:00Z", due: "2026-10-02T00:00:00Z"}`,
			`{at: !lt.get-path(root) due}`, false},

		// a node of the other kind fails the pattern
		{`"not a time"`, `!lt "2026-10-01T00:00:00Z"`, false},
		{`1`, `!lt "2026-10-01T00:00:00Z"`, false},
		{`"2026-09-30T00:00:00Z"`, `!lt 4`, false},
		{`null`, `!lt "2026-10-01T00:00:00Z"`, false},
	} {
		got, err := tony.Match(mustParseNode(t, tc.doc), mustParseNode(t, tc.pattern))
		if err != nil {
			t.Errorf("%s against %s: %v", tc.doc, tc.pattern, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s against %s: got %v, want %v", tc.doc, tc.pattern, got, tc.want)
		}
	}
}
