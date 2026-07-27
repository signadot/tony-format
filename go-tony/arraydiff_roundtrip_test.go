package tony

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/libdiff"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

// arrayRoundTrips exercise DiffArrayByIndex and its applier.  The keys of an
// !arraydiff are positions in a merged sequence which every slot advances by
// one, so a slot which consumes a different number of elements than the
// applier assumes throws off every key after it.
var arrayRoundTrips = []struct {
	name string
	a    string
	b    string
}{{
	name: "no change",
	a:    `[1, 2, 3]`,
	b:    `[1, 2, 3]`,
}, {
	name: "trailing insert",
	a:    `[1, 2]`,
	b:    `[1, 2, 3]`,
}, {
	name: "leading delete",
	a:    `[1, 2, 3]`,
	b:    `[2, 3]`,
}, {
	name: "trailing replace",
	a:    `[1, 2, 8]`,
	b:    `[1, 2, 9]`,
}, {
	name: "replace in the middle, then more",
	a:    `[1, 8, 2, 3]`,
	b:    `[1, 9, 2, 3]`,
}, {
	name: "replace in the middle, then an insert",
	a:    `[1, 8, 2]`,
	b:    `[1, 9, 2, 4]`,
}, {
	name: "replace then replace",
	a:    `[1, 8, 2, 6]`,
	b:    `[1, 9, 2, 7]`,
}, {
	name: "two adjacent replaced",
	a:    `[1, 8, 6, 2]`,
	b:    `[1, 9, 7, 2]`,
}, {
	name: "delete more than inserted",
	a:    `[1, 8, 6, 5, 2]`,
	b:    `[1, 9, 2]`,
}, {
	name: "insert more than deleted",
	a:    `[1, 8, 2]`,
	b:    `[1, 9, 7, 5, 2]`,
}, {
	name: "the shipped diffTests[2]",
	a:    "- 1\n- 2\n- hello \n- hello\n- hellp \n- 7\n- 8",
	b:    "- 2\n- hello\n- hello \n- hello\n- 4\n- 7\n- 9",
}, {
	name: "replaced element carries a tag",
	a:    `[1, !mytag 8, 2]`,
	b:    `[1, !mytag 9, 2]`,
}, {
	name: "replaced element gains a tag",
	a:    `[1, 8, 2]`,
	b:    `[1, !mytag 9, 2]`,
}, {
	name: "inserted element carries a tag",
	a:    `[1, 2]`,
	b:    `[1, !mytag 8, 2]`,
}, {
	name: "deleted element carries a tag",
	a:    `[1, !mytag 8, 2]`,
	b:    `[1, 2]`,
}, {
	name: "replaced element holds an operator as data",
	a:    `[1, !glob "a-*", 2]`,
	b:    `[1, !glob "b-*", 2]`,
}, {
	name: "objects, one replaced in the middle",
	a:    `[{n: 1}, {n: 8}, {n: 2}, {n: 3}]`,
	b:    `[{n: 1}, {n: 9}, {n: 2}, {n: 3}]`,
}}

// TestArrayDiffRoundTripRandom walks a lot of small arrays through the same
// invariant.  A table cannot cover which runs of deletes and inserts end up
// adjacent, and adjacency is what decides whether a slot holds a replace, so
// the numbering is only really pinned by volume.
func TestArrayDiffRoundTripRandom(t *testing.T) {
	// elements chosen so that pairs differ in type, in value and in tag, since
	// DiffArrayByIndex matches elements by a summary of type and value and
	// folds a delete with the insert which follows it.
	elems := []string{
		`1`, `2`, `3`, `true`, `false`, `null`, `"a"`, `"b"`, `"a-long-string"`,
		`!mytag 1`, `!mytag 2`, `!other 1`, `!glob "a-*"`, `!delete null`,
		`{n: 1}`, `{n: 2}`, `[1]`, `[1, 2]`,
	}
	rnd := rand.New(rand.NewSource(20260727))
	array := func() string {
		n := rnd.Intn(7)
		vals := make([]string, n)
		for i := range vals {
			vals[i] = elems[rnd.Intn(len(elems))]
		}
		return "[" + strings.Join(vals, ", ") + "]"
	}
	for i := 0; i < 3000; i++ {
		aSrc, bSrc := array(), array()
		a, err := parse.Parse([]byte(aSrc))
		if err != nil {
			t.Fatalf("parse %q: %v", aSrc, err)
		}
		b, err := parse.Parse([]byte(bSrc))
		if err != nil {
			t.Fatalf("parse %q: %v", bSrc, err)
		}
		diff := Diff(a, b)
		if diff == nil {
			if !mergeop.RawEqual(a, b) {
				t.Fatalf("no diff between %s and %s", aSrc, bSrc)
			}
			continue
		}
		got, err := Patch(a, diff)
		if err != nil {
			t.Fatalf("a %s\nb %s\ndiff\n%s\npatch: %v", aSrc, bSrc, encode.MustString(diff), err)
		}
		if !mergeop.RawEqual(got, b) {
			t.Fatalf("a %s\nb %s\ndiff\n%s\ngot\n%s", aSrc, bSrc, encode.MustString(diff), encode.MustString(got))
		}
		rev, err := libdiff.Reverse(diff)
		if err != nil {
			t.Fatalf("a %s\nb %s\nreverse: %v", aSrc, bSrc, err)
		}
		back, err := Patch(b, rev)
		if err != nil {
			t.Fatalf("a %s\nb %s\nreverse\n%s\npatch: %v", aSrc, bSrc, encode.MustString(rev), err)
		}
		if !mergeop.RawEqual(back, a) {
			t.Fatalf("a %s\nb %s\nreverse\n%s\ngot\n%s", aSrc, bSrc, encode.MustString(rev), encode.MustString(back))
		}
	}
}

func TestArrayDiffRoundTrip(t *testing.T) {
	for _, test := range arrayRoundTrips {
		t.Run(test.name, func(t *testing.T) {
			a := mustParse(t, test.a)
			b := mustParse(t, test.b)

			diff := Diff(a, b)
			if diff == nil {
				if !mergeop.RawEqual(a, b) {
					t.Fatalf("no diff between distinct arrays")
				}
				return
			}
			t.Logf("diff\n%s", encode.MustString(diff))

			got, err := Patch(a, diff)
			if err != nil {
				t.Fatalf("patch(a, diff): %v", err)
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
