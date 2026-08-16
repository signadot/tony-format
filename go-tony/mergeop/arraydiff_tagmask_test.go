package mergeop_test

import (
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// TestArrayDiffFindsItsOpBehindALeadingLabel: an arraydiff element's operation is
// the first label of its tag chain this package KNOWS, not simply the first label.
//
// patchArrayByIndex used to switch on the raw head, so any label ahead of the op
// hid it and the element fell to the positional-patch branch. Parsing alone puts a
// label there -- `!delete {by: scott}` reads as !bracket.delete -- so deleting or
// inserting anything but a scalar was already broken, and logd, which marks every
// patch root, had !insert overwrite the element it was meant to insert before and
// !delete panic every reader of the store (jjbapb1ah12kranxg5n0).
//
// Every case below is an ordinary spelling a caller would write, and every one of
// them was wrong before the fix. What becomes of the label itself is the next test.
func TestArrayDiffFindsItsOpBehindALeadingLabel(t *testing.T) {
	// Objects, deliberately: parsing writes their braces as a !bracket label ahead
	// of the op, so each of these IS the masked case, in the spelling a caller
	// would actually write.
	const doc = "{v: [{a: 1}, {b: 2}]}"
	for _, tc := range []struct{ name, patch, want string }{
		{
			name:  "insert before the first element",
			patch: "{v: !arraydiff {0: !insert {c: 3}}}",
			want:  "{\n  c: 3\n} | {\n  a: 1\n} | {\n  b: 2\n}",
		},
		{
			name:  "insert at the end, which is an append",
			patch: "{v: !arraydiff {2: !insert {c: 3}}}",
			want:  "{\n  a: 1\n} | {\n  b: 2\n} | {\n  c: 3\n}",
		},
		{
			name:  "delete an element",
			patch: "{v: !arraydiff {0: !delete {a: 1}}}",
			want:  "{\n  b: 2\n}",
		},
		{
			name:  "replace an element",
			patch: "{v: !arraydiff {0: !replace {from: {a: 1}, to: {c: 3}}}}",
			want:  "{\n  c: 3\n} | {\n  b: 2\n}",
		},
		{
			name:  "a plain element is still a positional patch",
			patch: "{v: !arraydiff {0: {c: 3}}}",
			want:  "{\n  a: 1\n  c: 3\n} | {\n  b: 2\n}",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := elementsOf(t, doc, tc.patch); got != tc.want {
				t.Errorf("%s\n got %q\nwant %q", tc.patch, got, tc.want)
			}
		})
	}
}

// A label AHEAD of the op belongs to the value and is put back on it. The labels
// after the op do not: those are the diff's own encoding, restored by name
// (!insert(tag)) or by !raw, and otherwise dropped.
//
// This is what makes a delete of anything but a scalar work at all. Parsing writes
// the brace of `{by: scott}` as a !bracket label ahead of the op, and the document
// element it is compared against carries that same label; wiping the chain compared
// a braceless object against a braced one and nothing ever matched.
func TestArrayDiffKeepsTheLabelsAheadOfTheOp(t *testing.T) {
	for _, tc := range []struct{ name, doc, patch, want string }{
		{
			name:  "an inserted element keeps the label",
			doc:   "{v: [1]}",
			patch: "{v: !arraydiff {0: !lbl.insert 99}}",
			want:  "!lbl 99 | 1",
		},
		{
			name:  "an op-named tag is restored ahead of the label",
			doc:   "{v: [1]}",
			patch: "{v: !arraydiff {0: !lbl.insert(keepme) 99}}",
			want:  "!keepme.lbl 99 | 1",
		},
		{
			name:  "a delete matches an element carrying the same label",
			doc:   "{v: [{by: scott}, {by: dee}]}",
			patch: "{v: !arraydiff {0: !delete {by: scott}}}",
			want:  "{\n  by: dee\n}",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := elementsOf(t, tc.doc, tc.patch); got != tc.want {
				t.Errorf("got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// elementsOf renders the elements of v one by one, so a case reads as the array it
// is about rather than as the layout of the whole document.
func elementsOf(t *testing.T, doc, patch string) string {
	t.Helper()
	d, err := parse.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse doc: %v", err)
	}
	p, err := parse.Parse([]byte(patch))
	if err != nil {
		t.Fatalf("parse patch: %v", err)
	}
	got, err := tony.Patch(d, p)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	arr := ir.Get(got, "v")
	if arr == nil {
		return "<no v>"
	}
	out := ""
	for i, e := range arr.Values {
		if i > 0 {
			out += " | "
		}
		out += encode.MustString(e)
	}
	return out
}
