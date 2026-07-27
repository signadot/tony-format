package tony

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/libdiff"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

// randomDoc writes a small tony document.  The vocabulary is deliberately
// narrow -- few field names, few values, tags drawn from a handful which
// includes merge operation names -- so that two independently generated
// documents share enough structure for a diff to have something to say beyond
// "replace everything", and so that operation names turn up as data.
type randomDoc struct {
	rnd *rand.Rand
}

func (g *randomDoc) tag() string {
	switch g.rnd.Intn(8) {
	case 0:
		return "!mytag "
	case 1:
		return "!other "
	case 2:
		// operation names, held as data
		return "!glob "
	case 3:
		return "!delete "
	default:
		return ""
	}
}

func (g *randomDoc) value(depth int) string {
	kind := g.rnd.Intn(10)
	if depth >= 2 && kind > 6 {
		kind = g.rnd.Intn(7)
	}
	switch kind {
	case 0, 1:
		return g.tag() + fmt.Sprint(g.rnd.Intn(4))
	case 2:
		return g.tag() + []string{"true", "false"}[g.rnd.Intn(2)]
	case 3:
		return g.tag() + "null"
	case 4, 5, 6:
		// long enough that a small edit is worth a strdiff rather than a
		// replace, which is the branch worth exercising
		return g.tag() + `"` + []string{
			"a-fairly-long-identifier", "b-fairly-long-identifier",
			"a-fairly-long-idxntifier", "short", "shore",
		}[g.rnd.Intn(5)] + `"`
	case 7, 8:
		n := g.rnd.Intn(4)
		parts := make([]string, n)
		for i := range parts {
			parts[i] = g.value(depth + 1)
		}
		return g.tag() + "[" + strings.Join(parts, ", ") + "]"
	default:
		fields := []string{"a", "b", "c", "d"}
		g.rnd.Shuffle(len(fields), func(i, j int) { fields[i], fields[j] = fields[j], fields[i] })
		n := g.rnd.Intn(4)
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fields[i] + ": " + g.value(depth+1)
		}
		return g.tag() + "{" + strings.Join(parts, ", ") + "}"
	}
}

// TestDiffRoundTripRandom holds Patch(a, Diff(a, b)) == b and
// Patch(b, Reverse(Diff(a, b))) == a over random documents.  The corpus in
// diff_test.go pins the text a diff should have; this pins that it works, which
// is the property anything storing patches actually depends on.
func TestDiffRoundTripRandom(t *testing.T) {
	g := &randomDoc{rnd: rand.New(rand.NewSource(20260727))}
	for i := 0; i < 3000; i++ {
		aSrc, bSrc := g.value(0), g.value(0)
		a, err := parse.Parse([]byte(aSrc))
		if err != nil || a == nil {
			continue // the generator is loose; only well formed pairs are of interest
		}
		b, err := parse.Parse([]byte(bSrc))
		if err != nil || b == nil {
			continue
		}
		diff := Diff(a, b)
		if diff == nil {
			if !mergeop.RawEqual(a, b) {
				t.Fatalf("no diff between distinct documents\na %s\nb %s", aSrc, bSrc)
			}
			continue
		}
		report := func(what string, extra ...any) string {
			return fmt.Sprintf("%s\na %s\nb %s\ndiff\n%s\n%v",
				what, aSrc, bSrc, encode.MustString(diff), extra)
		}
		got, err := Patch(a, diff)
		if err != nil {
			t.Fatal(report("patch(a, diff) failed", err))
		}
		if !mergeop.RawEqual(got, b) {
			t.Fatal(report("patch(a, diff) != b", encode.MustString(got)))
		}
		rev, err := libdiff.Reverse(diff)
		if err != nil {
			t.Fatal(report("reverse failed", err))
		}
		back, err := Patch(b, rev)
		if err != nil {
			t.Fatal(report("patch(b, reverse) failed", encode.MustString(rev), err))
		}
		if !mergeop.RawEqual(back, a) {
			t.Fatal(report("patch(b, reverse) != a", encode.MustString(rev), encode.MustString(back)))
		}
	}
}
