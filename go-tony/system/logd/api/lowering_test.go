package api

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

// NeedsLowering decides, per write, whether the store can keep the patch it was given
// or has to apply it and diff the result. The vocabulary is the absolute operations,
// and a patch built only from those is already its own delta.
//
// What is being pinned is the SHAPE of the walk as much as the answer: it reads a
// composed tag chain, it stops at !raw, and it sees through a head comment. A miss in
// any of those is a relative operation stored unlowered, which is what the vocabulary
// exists to prevent.
func TestNeedsLowering(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string // the operation named, or "" for no lowering
	}{{
		name: "a plain data merge is already its own delta",
		doc:  "{a: 1, b: {c: 2}}",
	}, {
		name: "so is one built from the vocabulary",
		doc:  "{a: !delete null, b: !insert 5, c: !addtag(x) {}}",
	}, {
		name: "a keyed list, which is what logd injects for itself",
		doc:  `{items: !key(sku) [{sku: "A", q: 1}]}`,
	}, {
		name: "a checked replace is relative",
		doc:  "{a: !replace {from: 1, to: 2}}",
		want: "!replace",
	}, {
		name: "and so is one nested under plain data",
		doc:  "{a: {b: {c: !strdiff []}}}",
		want: "!strdiff",
	}, {
		name: "inside an array",
		doc:  "{a: [1, {b: !rename []}]}",
		want: "!rename",
	}, {
		name: "a composed chain is read, not just its head",
		doc:  "{a: !insert.retag(x,y) 5}",
		want: "!retag",
	}, {
		// The escape is what lets a document HOLDING a rule, a charter or a patch
		// be stored at all. Walking into it would refuse a write the writer
		// escaped correctly.
		name: "beneath !raw nothing is an operation",
		doc:  "{a: !raw {b: !replace {from: 1, to: 2}}}",
	}, {
		name: "but the raw node's own chain still answers",
		doc:  "{a: !strdiff.raw []}",
		want: "!strdiff",
	}, {
		// A comment wraps the value it precedes and is not a kind of container.
		name: "a head comment does not hide what is under it",
		doc:  "a:\n  # why\n  b: !replace {from: 1, to: 2}",
		want: "!replace",
	}, {
		name: "!comment is itself absolute",
		doc:  "{a: !comment {lines: [\"# hi\"]}}",
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			n, err := parse.Parse([]byte(test.doc), parse.ParseComments(true))
			if err != nil {
				t.Fatalf("parse: %s", err)
			}
			op, yes := NeedsLowering(n)
			if yes != (test.want != "") {
				t.Fatalf("answered %v (%q), want %v", yes, op, test.want != "")
			}
			if op != test.want {
				t.Errorf("named %q, want %q", op, test.want)
			}
		})
	}
}

// Every registered operation is in exactly one of three classes, and the write path
// asks about them in this order: unsafe is refused, relative is lowered, absolute is
// stored as it arrived. This fails when an operation is added to tony without logd
// having said which it is -- which is the point of the vocabulary being an allowlist
// declared in full rather than a property the operation asserts about itself.
func TestEveryOperationIsClassified(t *testing.T) {
	var unsafe, absolute, relative []string
	for _, s := range mergeop.Symbols() {
		n := s.String()
		switch {
		case mergeop.Unsafe(n):
			unsafe = append(unsafe, n)
		case IsStorableTag(n):
			absolute = append(absolute, n)
		default:
			relative = append(relative, n)
		}
	}
	if len(unsafe)+len(absolute)+len(relative) != len(mergeop.Symbols()) {
		t.Fatal("the three classes do not partition the registry")
	}
	// The absolute set is the vocabulary itself, so it is the one to state: a new
	// operation landing here means someone decided a store may keep it verbatim,
	// and that decision wants to be visible in a diff.
	want := map[string]bool{
		"insert": true, "delete": true, "key": true, "raw": true,
		"addtag": true, "rmtag": true, "comment": true,
	}
	if len(absolute) != len(want) {
		t.Errorf("%d absolute operations, want %d: %v", len(absolute), len(want), absolute)
	}
	for _, n := range absolute {
		if !want[n] {
			t.Errorf("%q is storable and was not before; say why in storableTags", n)
		}
	}
	if len(unsafe) != 1 || unsafe[0] != "pipe" {
		t.Errorf("unsafe operations are %v, want [pipe]", unsafe)
	}
	t.Logf("absolute %d, relative %d, unsafe %d", len(absolute), len(relative), len(unsafe))
}
