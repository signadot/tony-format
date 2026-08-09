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

// leftover reports what a round trip failed to reproduce, ignoring presentation.
//
// Presentation tags -- !bracket and friends -- record how a value was WRITTEN, and
// ir/tags.go names them a category that "patching, raw matching" drop first: an object
// a patch introduces onto an absent document comes back without the braces the patch
// was written with. That is the same normalization logd's own snapshot round trip
// asserts (snapshot_test compares against a node with its formatting tag cleared). So
// a presentation-only difference is not a failure of the diff to carry the data, and
// this compares what the data is.
func leftover(a, b *ir.Node) *ir.Node {
	return Diff(stripPresentationDeep(a.Clone()), stripPresentationDeep(b.Clone()))
}

func stripPresentationDeep(n *ir.Node) *ir.Node {
	if n == nil {
		return nil
	}
	n.Tag = ir.StripPresentation(n.Tag)
	for _, f := range n.Fields {
		stripPresentationDeep(f)
	}
	for _, v := range n.Values {
		stripPresentationDeep(v)
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
		if left := leftover(got, to); left != nil {
			t.Errorf("round trip left a difference\n from: %s\n   to: %s\n  remaining: %s",
				tc.from, tc.to, encode.MustString(left))
		}
	}
}

// TestDiffKeyedArrayNestedKeyField covers a key field that is a PATH rather than a
// plain field name. Both readers of a key resolve one -- ir.ElemKey and YKeyOf's
// GetPath -- so the diff has to put it back the same shape it read it from, nested
// rather than as a flat field literally named "meta.name".
func TestDiffKeyedArrayNestedKeyField(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to string
	}{
		{"value changed",
			`!key(meta.name) [{meta: {name: "a"}, v: 1}]`,
			`!key(meta.name) [{meta: {name: "a"}, v: 2}]`},
		{"sibling under the key's own parent changed",
			`!key(meta.name) [{meta: {name: "a", rev: 1}, v: 1}]`,
			`!key(meta.name) [{meta: {name: "a", rev: 2}, v: 1}]`},
		{"element added",
			`!key(meta.name) [{meta: {name: "a"}, v: 1}]`,
			`!key(meta.name) [{meta: {name: "a"}, v: 1}, {meta: {name: "b"}, v: 2}]`},
		{"element removed",
			`!key(meta.name) [{meta: {name: "a"}, v: 1}, {meta: {name: "b"}, v: 2}]`,
			`!key(meta.name) [{meta: {name: "a"}, v: 1}]`},
		{"three levels deep",
			`!key(a.b.name) [{a: {b: {name: "x"}}, v: 1}]`,
			`!key(a.b.name) [{a: {b: {name: "x"}}, v: 2}]`},
		{"unchanged",
			`!key(meta.name) [{meta: {name: "a"}, v: 1}]`,
			`!key(meta.name) [{meta: {name: "a"}, v: 1}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from, to := mustParseNode(t, tc.from), mustParseNode(t, tc.to)
			d := Diff(from, to)
			if d == nil {
				// No change: nothing to apply, and nothing to check beyond that.
				if left := Diff(from, to); left != nil {
					t.Fatalf("Diff disagreed with itself")
				}
				return
			}
			got, err := Patch(from, d)
			if err != nil {
				t.Fatalf("Patch(%s, %s): %v", tc.from, encode.MustString(d), err)
			}
			if left := leftover(got, to); left != nil {
				t.Errorf("round trip left a difference\n from: %s\n   to: %s\n diff: %s\n left: %s",
					tc.from, tc.to, encode.MustString(d), encode.MustString(left))
			}
		})
	}
}
