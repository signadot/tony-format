package tony

import (
	"strconv"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
	"github.com/signadot/tony-format/go-tony/mergeop"
)

// roundTrips are pairs for which Patch(a, Diff(a, b)) must be b, and
// Patch(b, Reverse(Diff(a, b))) must be a.  A diff is only worth anything if
// it applies, and structural inspection of one does not establish that.
var roundTrips = []struct {
	name string
	a    string
	b    string
}{{
	name: "plain value change",
	a:    `rule: {a: 1}`,
	b:    `rule: {a: 2}`,
}, {
	name: "plain insert",
	a:    "a: 1",
	b:    "a: 1\nb: 2",
}, {
	name: "tagged insert",
	a:    "a: 1",
	b:    "a: 1\nb: !mytag 2",
}, {
	name: "tagged delete",
	a:    "a: 1\nb: !mytag 2",
	b:    "a: 1",
}, {
	name: "insert a bracketed object",
	a:    `{}`,
	b:    `{rule: {id: "x"}}`,
}, {
	name: "insert a hyphenated tag",
	a:    "a: 1",
	b:    "a: 1\nb: !has-path 2",
}, {
	name: "delete a hyphenated tag",
	a:    "a: 1\nb: !has-path 2",
	b:    "a: 1",
}, {
	name: "tagged string change",
	a:    `id: !mytag "a-fairly-long-identifier"`,
	b:    `id: !mytag "b-fairly-long-identifier"`,
}, {
	name: "tag added to a string",
	a:    `id: "a-fairly-long-identifier"`,
	b:    `id: !mytag "b-fairly-long-identifier"`,
}, {
	name: "tag removed from a string",
	a:    `id: !mytag "a-fairly-long-identifier"`,
	b:    `id: "b-fairly-long-identifier"`,
}, {
	name: "tag replaced on a string",
	a:    `id: !mytag "a-fairly-long-identifier"`,
	b:    `id: !othertag "b-fairly-long-identifier"`,
}, {
	// the rest hold merge operations as data: a stored rule, a stored patch
	name: "insert an operator held as data",
	a:    `{}`,
	b:    `rule: {id: !glob "hot-*"}`,
}, {
	name: "insert a stored patch",
	a:    `rule: {}`,
	b:    `rule: {patch: {tmp: !delete null}}`,
}, {
	name: "insert a whole stored rule",
	a:    `{}`,
	b:    `rule: {id: !glob "hotfix-*", patch: {tmp: !delete null}, when: !not "closed"}`,
}, {
	name: "insert an operator tag at the root of the value",
	a:    `{}`,
	b:    `id: !glob "hot-*"`,
}, {
	name: "delete an operator held as data",
	a:    `rule: {id: !glob "x", b: 1}`,
	b:    `rule: {b: 1}`,
}, {
	name: "change a string under an operator tag",
	a:    `rule: {id: !glob "a-fairly-long-pattern-*"}`,
	b:    `rule: {id: !glob "b-fairly-long-pattern-*"}`,
}, {
	name: "replace a value with an operator held as data",
	a:    `id: "x"`,
	b:    `id: !glob "hot-*"`,
}, {
	name: "an operator held as data appears in an array",
	a:    `rules: [{id: "a"}]`,
	b:    `rules: [{id: "a"}, {id: !glob "b-*"}]`,
}, {
	name: "a stored patch of a stored patch",
	a:    `{}`,
	b:    `rule: {patch: {inner: !raw {tmp: !delete null}}}`,
}}

func TestDiffRoundTrip(t *testing.T) {
	for _, test := range roundTrips {
		t.Run(test.name, func(t *testing.T) {
			a := mustParse(t, test.a)
			b := mustParse(t, test.b)

			diff := Diff(a, b)
			if diff == nil {
				if !mergeop.RawEqual(a, b) {
					t.Fatalf("no diff between distinct documents\na\n%s\nb\n%s",
						encode.MustString(a), encode.MustString(b))
				}
				return
			}
			t.Logf("diff\n%s", encode.MustString(diff))

			got, err := Patch(a, diff)
			if err != nil {
				t.Fatalf("patch(a, diff): %v\ndiff\n%s", err, encode.MustString(diff))
			}
			if !mergeop.RawEqual(got, b) {
				t.Errorf("patch(a, diff)\n%s\nwant b\n%s",
					encode.MustString(got), encode.MustString(b))
			}

			rev, err := libdiff.Reverse(diff)
			if err != nil {
				t.Fatalf("reverse: %v", err)
			}
			back, err := Patch(b, rev)
			if err != nil {
				t.Fatalf("patch(b, reverse(diff)): %v\nreverse\n%s", err, encode.MustString(rev))
			}
			if !mergeop.RawEqual(back, a) {
				t.Errorf("patch(b, reverse(diff))\n%s\nwant a\n%s",
					encode.MustString(back), encode.MustString(a))
			}
		})
	}
}

// TestDiffTestsRoundTrip holds the same invariant over the corpus TestDiff
// already checks the shape of.  A diff whose text is right but which does not
// apply is not right.
// notApplicable are diffTests indices whose generated diff cannot be applied
// to a at all, for a reason which predates the escape and has nothing to do
// with it: an arraydiff whose keys are numbered past the end of the document,
// which panics rather than erroring.  Issue 7rdzbjjbh12krw58cxn0.
var notApplicable = map[int]bool{2: true}

func TestDiffTestsRoundTrip(t *testing.T) {
	for i := range diffTests {
		test := &diffTests[i]
		if notApplicable[i] {
			continue
		}
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			a := mustParse(t, test.a)
			b := mustParse(t, test.b)
			diff := Diff(a, b)
			if diff == nil {
				return
			}
			got, err := Patch(a, diff)
			if err != nil {
				t.Fatalf("patch(a, diff): %v\na\n%s\nb\n%s\ndiff\n%s",
					err, encode.MustString(a), encode.MustString(b), encode.MustString(diff))
			}
			if !mergeop.RawEqual(got, b) {
				t.Errorf("patch(a, diff)\n%s\nwant b\n%s\ndiff\n%s",
					encode.MustString(got), encode.MustString(b), encode.MustString(diff))
			}
		})
	}
}

// TestDiffEscapesOperatorsHeldAsData pins the escape itself: a diff which
// carries a document's operators must say !raw, or applying it would run them.
func TestDiffEscapesOperatorsHeldAsData(t *testing.T) {
	a := mustParse(t, `{}`)
	b := mustParse(t, `rule: {patch: {tmp: !delete null}}`)
	diff := Diff(a, b)
	if diff == nil {
		t.Fatal("no diff")
	}
	rule := ir.Get(diff, "rule")
	if rule == nil {
		t.Fatalf("no rule field in\n%s", encode.MustString(diff))
	}
	if !ir.TagHas(rule.Tag, libdiff.RawTag) {
		t.Errorf("rule tag %q has no %s in\n%s", rule.Tag, libdiff.RawTag, encode.MustString(diff))
	}
}

// TestReverseLeavesRawDataAlone: reversing a diff reverses its operations, and
// the operation names beneath a !raw are not its operations.
func TestReverseLeavesRawDataAlone(t *testing.T) {
	// built rather than parsed: the parser composes !bracket ahead of the
	// tag it is given, and it is the head of a diff's tag which names the
	// operation.
	stored := mustParse(t, `{tmp: !delete null, was: !insert 1}`)
	stored.Tag = ir.TagCompose(libdiff.InsertTag, nil,
		ir.TagCompose(libdiff.RawTag, nil, stored.Tag))
	diff := ir.FromMap(map[string]*ir.Node{"rule": stored})

	rev, err := libdiff.Reverse(diff)
	if err != nil {
		t.Fatalf("reverse: %v", err)
	}
	rule := ir.Get(rev, "rule")
	if rule == nil {
		t.Fatalf("no rule field in\n%s", encode.MustString(rev))
	}
	if !ir.TagHas(rule.Tag, libdiff.DeleteTag) {
		t.Errorf("insert did not reverse to delete: %q", rule.Tag)
	}
	want := mustParse(t, `{tmp: !delete null, was: !insert 1}`)
	if !mergeop.RawEqual(rule.WithTag(""), want.WithTag("")) {
		t.Errorf("data beneath !raw was rewritten\ngot\n%s\nwant\n%s",
			encode.MustString(rule), encode.MustString(want))
	}
}
