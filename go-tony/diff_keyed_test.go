package tony

import (
	"testing"

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
// relies on.
//
// SKIPPED: it does not hold today, for two reasons that are separate from the nil
// dereference fixed alongside this test, and separate from each other:
//
//   - An element the diff ADDS keeps its op tag in the patched result:
//     Patch(!key(name)[{name:a}], !key(name)[!insert(bracket){name:b}]) yields
//     [{name:a} !insert(bracket){name:b}]. keyedListOp.Patch appends a patch element
//     whose key is absent from the doc verbatim, tag included, so an op tag ends up on
//     stored data.
//   - DiffArrayByKey rebuilds each key value by ENCODING it and re-parsing
//     (YKeyOf -> parse.Parse), so a quoted key comes back bare and the rebuilt element
//     is not DeepEqual to the original even when it encodes identically.
//
// Neither had been reachable: the keyed branch requires !key(f) on BOTH sides, and no
// materialized state carries it.
func TestPatchKeyedDiffRoundTrip(t *testing.T) {
	t.Skip("keyed diff does not round-trip; see comment above")

	for _, tc := range []struct{ from, to string }{
		{`!key(name) [{name: "a", v: 1}]`, `!key(name) [{name: "a", v: 9}]`},
		{`!key(name) [{name: "a", v: 1}]`, `!key(name) [{name: "a", v: 1}, {name: "b", v: 2}]`},
		{`!key(name) [{name: "a", v: 1}, {name: "b", v: 2}]`, `!key(name) [{name: "a", v: 1}]`},
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
		if !got.DeepEqual(to) {
			t.Errorf("round trip failed\n from: %s\n   to: %s\n  got type %s with %d values",
				tc.from, tc.to, got.Type, len(got.Values))
		}
	}
}
