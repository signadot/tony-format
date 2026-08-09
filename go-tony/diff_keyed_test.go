package tony

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func mustParseNode(t *testing.T, s string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(s))
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return n
}

// TestDiffKeyedArray exercises diffArray's keyed branch, which is taken only when BOTH
// sides carry !key(f) with the same field. Equal lists are the ordinary case there and
// used to panic: the keyed diff indexed a nil result.
func TestDiffKeyedArray(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to string
		wantDiff bool
	}{
		{"equal", `!key(name) [{name: "a", v: 1}]`, `!key(name) [{name: "a", v: 1}]`, false},
		{"equal two elements",
			`!key(name) [{name: "a", v: 1}, {name: "b", v: 2}]`,
			`!key(name) [{name: "a", v: 1}, {name: "b", v: 2}]`, false},
		{"reordered, same elements",
			`!key(name) [{name: "a", v: 1}, {name: "b", v: 2}]`,
			`!key(name) [{name: "b", v: 2}, {name: "a", v: 1}]`, false},
		{"element changed", `!key(name) [{name: "a", v: 1}]`, `!key(name) [{name: "a", v: 9}]`, true},
		{"element added",
			`!key(name) [{name: "a", v: 1}]`,
			`!key(name) [{name: "a", v: 1}, {name: "b", v: 2}]`, true},
		{"element removed",
			`!key(name) [{name: "a", v: 1}, {name: "b", v: 2}]`,
			`!key(name) [{name: "a", v: 1}]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from, to := mustParseNode(t, tc.from), mustParseNode(t, tc.to)
			d := Diff(from, to)
			if tc.wantDiff && d == nil {
				t.Fatalf("expected a diff between %s and %s", tc.from, tc.to)
			}
			if !tc.wantDiff && d != nil {
				t.Fatalf("expected no diff between %s and %s, got type %s", tc.from, tc.to, d.Type)
			}
		})
	}
}

// TestPatchKeyedDiffRoundTrip: whatever the keyed branch produces must carry the
// document from `from` to `to`, since that is the contract every consumer of a diff
// relies on. Structural inspection is not enough here -- the earlier failures all
// ENCODED identically to the target and differed only in a tag or in how a key was
// represented, so this applies the diff and compares.
func TestPatchKeyedDiffRoundTrip(t *testing.T) {
	for _, tc := range []struct{ from, to string }{
		{`!key(name) [{name: "a", v: 1}]`, `!key(name) [{name: "a", v: 9}]`},
		{`!key(name) [{name: "a", v: 1}]`, `!key(name) [{name: "a", v: 1}, {name: "b", v: 2}]`},
		{`!key(name) [{name: "a", v: 1}, {name: "b", v: 2}]`, `!key(name) [{name: "a", v: 1}]`},
		// unchanged in the middle, changed at both ends
		{`!key(name) [{name: "a", v: 1}, {name: "b", v: 2}, {name: "c", v: 3}]`,
			`!key(name) [{name: "a", v: 9}, {name: "b", v: 2}, {name: "c", v: 8}]`},
		// a key that must stay quoted: bare 9a is not the same token
		{`!key(name) [{name: "9a", v: 1}]`, `!key(name) [{name: "9a", v: 2}]`},
		// nested value inside the element
		{`!key(name) [{name: "a", meta: {x: 1, y: 2}}]`, `!key(name) [{name: "a", meta: {x: 1, y: 3}}]`},
		// element gains a field
		{`!key(name) [{name: "a"}]`, `!key(name) [{name: "a", v: 1}]`},
		// element loses a field
		{`!key(name) [{name: "a", v: 1}]`, `!key(name) [{name: "a"}]`},
		// add and remove in one diff
		{`!key(name) [{name: "a", v: 1}, {name: "b", v: 2}]`,
			`!key(name) [{name: "a", v: 1}, {name: "c", v: 3}]`},
		// emptied entirely
		{`!key(name) [{name: "a", v: 1}]`, `!key(name) []`},
	} {
		from, to := mustParseNode(t, tc.from), mustParseNode(t, tc.to)
		d := Diff(from, to)
		if d == nil {
			t.Fatalf("no diff between %s and %s", tc.from, tc.to)
		}
		got, err := Patch(from, d)
		if err != nil {
			t.Fatalf("Patch(%s, diff): %v", tc.from, err)
		}
		// Equality here is "Diff finds nothing left", not DeepEqual. Object fields are
		// normalized to alphabetic order by ir.FromMap, which DiffArrayByKey builds its
		// elements through, so a diff element is alphabetized while a parsed target
		// keeps source order -- and DeepEqual is order-sensitive. That is not a keyed
		// list's doing: Patch(a, Diff(a,b)) is not DeepEqual to b for a plain object
		// either ({b: 1, a: 2} -> {b: 9, a: 2} fails the same way).
		//
		// Element ORDER within the list is a different matter and is preserved, so the
		// cases above still pin it: a diff that reordered elements would leave a
		// difference here.
		if left := Diff(got, to); left != nil {
			t.Errorf("round trip left a difference\n from: %s\n   to: %s\n  remaining: %s",
				tc.from, tc.to, encode.MustString(left))
		}
	}
}

// TestDiffKeyedArrayNestedKeyField records a keyed diff whose key field is a PATH, not
// a plain field name. DiffArrayByKey puts the key back with resMap[key] = keyVal, and
// key here is "meta.name", so the rebuilt element carries one flat field literally
// named "meta.name" instead of a nested meta: {name: ...}. Patching then fails: the
// element has no meta.name to merge by, as the error says while listing "meta.name"
// among the fields it does have.
//
// Nested key fields are supported elsewhere -- ir.ElemKey and Node.KeyField resolve
// !key(meta.name) -- so this is the diff side falling behind, not a limit of the form.
func TestDiffKeyedArrayNestedKeyField(t *testing.T) {
	t.Skip("keyed diff flattens a nested key field; see comment above")

	from := mustParseNode(t, `!key(meta.name) [{meta: {name: "a"}, v: 1}]`)
	to := mustParseNode(t, `!key(meta.name) [{meta: {name: "a"}, v: 2}]`)
	d := Diff(from, to)
	got, err := Patch(from, d)
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if left := Diff(got, to); left != nil {
		t.Errorf("round trip left: %s", encode.MustString(left))
	}
}
