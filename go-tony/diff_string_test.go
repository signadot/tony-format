package tony

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A strdiff keys its operations by position, so the pairs worth pinning are the
// ones where a position could be counted two ways: a multi unit delete, which
// takes more than one, a replace whose sides are of different lengths, and a
// second operation after either, whose key is wrong if the first was miscounted.
// The unit itself is the other half: a line under !strdiff(true) and a rune,
// not a byte, under !strdiff(false).
var strDiffTests = []struct {
	name string
	a    string
	b    string
	tag  string
}{{
	name: "one line changed",
	a:    "alpha\nbeta\ngamma\ndelta\nepsilon\n",
	b:    "alpha\nbeta\nGAMMA\ndelta\nepsilon\n",
	tag:  "!strdiff(true)",
}, {
	name: "line appended, no trailing newline",
	a:    "alpha\nbeta\ngamma\ndelta",
	b:    "alpha\nbeta\ngamma\ndelta\nepsilon",
	tag:  "!strdiff(true)",
}, {
	name: "line inserted in the middle",
	a:    "alpha\nbeta\ngamma\ndelta\nepsilon\nzeta\n",
	b:    "alpha\nbeta\ngamma\nDELTA-ISH\ndelta\nepsilon\nzeta\n",
	tag:  "!strdiff(true)",
}, {
	name: "lines deleted, then a later change",
	a: "alpha\nbeta\ngamma\ndelta\nepsilon\nzeta\neta\ntheta\niota\nkappa\n" +
		"lambda\nmu\nnu\nxi\nomicron\npi\nrho\nsigma\ntau\nupsilon\n",
	b: "alpha\nepsilon\nzeta\neta\ntheta\niota\nkappa\n" +
		"lambda\nmu\nnu\nxi\nomicron\npi\nrho\nsigma\ntau\nUPSILON\n",
	tag: "!strdiff(true)",
}, {
	name: "lines replaced by fewer, then a later change",
	a: "alpha\nbeta\ngamma\ndelta\nepsilon\nzeta\neta\ntheta\niota\nkappa\n" +
		"lambda\nmu\nnu\nxi\nomicron\npi\nrho\nsigma\ntau\nupsilon\n",
	b: "alpha\nBETA\nepsilon\nzeta\neta\ntheta\niota\nkappa\n" +
		"lambda\nmu\nnu\nxi\nomicron\npi\nrho\nsigma\ntau\nUPSILON\n",
	tag: "!strdiff(true)",
}, {
	name: "trailing newline removed",
	a:    "alpha\nbeta\ngamma\ndelta\nepsilon\n",
	b:    "alpha\nbeta\ngamma\ndelta\nepsilon",
	tag:  "!strdiff(true)",
}, {
	// only one side has newlines, so there are no lines to diff by
	name: "newlines joined away",
	a:    "alpha\nbeta\ngamma\ndelta\nepsilon\n",
	b:    "alpha beta gamma delta epsilon\n",
}, {
	name: "runes, not bytes",
	a:    "héllo wörld, a fairly long string",
	b:    "héllo wörld, a fairly lung string",
	tag:  "!strdiff(false)",
}, {
	name: "runes deleted, then a later change",
	a:    "日本語のテキストです、これはかなり長い文章になりました",
	b:    "日本語のテキストす、これはかなり長い文章になりまし",
	tag:  "!strdiff(false)",
}, {
	name: "characters deleted, then a later change",
	a:    "the quick brown fox jumps over the lazy dog",
	b:    "the quick brwn fox jumps over the lzy dog",
	tag:  "!strdiff(false)",
}, {
	name: "characters replaced by fewer, then a later change",
	a:    "the quick brown fox jumps over the lazy dog",
	b:    "the qk brown fox jumps over the laazy dog",
	tag:  "!strdiff(false)",
}}

// TestDiffString holds Patch(a, Diff(a, b)) == b and Patch(b, Reverse(Diff(a,
// b))) == a for strings, which is the whole of what a strdiff is for.
func TestDiffString(t *testing.T) {
	for _, test := range strDiffTests {
		t.Run(test.name, func(t *testing.T) {
			a, b := ir.FromString(test.a), ir.FromString(test.b)
			diff := Diff(a, b)
			if diff == nil {
				t.Fatalf("no diff between %q and %q", test.a, test.b)
			}
			if test.tag != "" && diff.Tag != test.tag {
				t.Errorf("diff is %s, expected %s\n%s",
					diff.Tag, test.tag, encode.MustString(diff))
			}
			got, err := Patch(a, diff)
			if err != nil {
				t.Fatalf("patch(a, diff): %v\n%s", err, encode.MustString(diff))
			}
			if got.String != test.b {
				t.Errorf("patch(a, diff) is %q, expected %q\n%s",
					got.String, test.b, encode.MustString(diff))
			}
			rev, err := libdiff.Reverse(diff)
			if err != nil {
				t.Fatalf("reverse: %v", err)
			}
			back, err := Patch(b, rev)
			if err != nil {
				t.Fatalf("patch(b, reverse): %v\n%s", err, encode.MustString(rev))
			}
			if back.String != test.a {
				t.Errorf("patch(b, reverse) is %q, expected %q\n%s",
					back.String, test.a, encode.MustString(rev))
			}
		})
	}
}

// randomText writes a text and then an edited copy of it, in the two shapes a
// strdiff has to tell apart: several lines, which it keys by line, and one
// line, which it keys by rune.  The vocabulary is narrow and the edits are few
// so that the pair is close enough for a diff to have something to say beyond
// "replace everything".
type randomText struct {
	rnd *rand.Rand
}

var textWords = []string{
	"alpha", "beta", "gamma", "delta", "epsilon",
	"chaîne", "peü", "日本語", "テキスト", "文章",
}

func (g *randomText) word() string {
	return textWords[g.rnd.Intn(len(textWords))]
}

func (g *randomText) line() string {
	n := 1 + g.rnd.Intn(4)
	words := make([]string, n)
	for i := range words {
		words[i] = g.word()
	}
	return strings.Join(words, " ")
}

func (g *randomText) text() []string {
	if g.rnd.Intn(4) == 0 {
		return []string{g.line()} // one line: a rune diff
	}
	n := 3 + g.rnd.Intn(10)
	lines := make([]string, n)
	for i := range lines {
		lines[i] = g.line()
	}
	if g.rnd.Intn(2) == 0 {
		lines = append(lines, "") // a trailing newline is a trailing empty line
	}
	return lines
}

// edit changes lines in place, in as many ways as there are to change them:
// a line dropped, a line added, a line rewritten, and a rune within a line
// changed, which is what a rune keyed diff sees.
func (g *randomText) edit(lines []string) []string {
	out := slices.Clone(lines)
	for n := 1 + g.rnd.Intn(3); n > 0; n-- {
		i := g.rnd.Intn(len(out))
		switch g.rnd.Intn(4) {
		case 0:
			if len(out) > 1 {
				out = slices.Delete(out, i, i+1)
			}
		case 1:
			out = slices.Insert(out, i, g.line())
		case 2:
			out[i] = g.line()
		default:
			rs := []rune(out[i])
			if len(rs) == 0 {
				continue
			}
			rs[g.rnd.Intn(len(rs))] = []rune(g.word())[0]
			out[i] = string(rs)
		}
	}
	return out
}

// TestDiffStringRandom holds the round trip of TestDiffString over random text.
// The corpus there names the shapes worth pinning; this one goes looking for
// the shapes nobody thought of.
func TestDiffStringRandom(t *testing.T) {
	g := &randomText{rnd: rand.New(rand.NewSource(20260812))}
	byLine, byRune := 0, 0
	for i := 0; i < 3000; i++ {
		aText := g.text()
		aStr := strings.Join(aText, "\n")
		bStr := strings.Join(g.edit(aText), "\n")
		if aStr == bStr {
			continue
		}
		a, b := ir.FromString(aStr), ir.FromString(bStr)
		diff := Diff(a, b)
		if diff == nil {
			t.Fatalf("no diff between %q and %q", aStr, bStr)
		}
		switch diff.Tag {
		case "!strdiff(true)":
			byLine++
		case "!strdiff(false)":
			byRune++
		}
		report := func(what string, extra ...any) string {
			return fmt.Sprintf("%s\na %q\nb %q\ndiff\n%s\n%v",
				what, aStr, bStr, encode.MustString(diff), extra)
		}
		got, err := Patch(a, diff)
		if err != nil {
			t.Fatal(report("patch(a, diff) failed", err))
		}
		if got.String != bStr {
			t.Fatal(report("patch(a, diff) != b", got.String))
		}
		rev, err := libdiff.Reverse(diff)
		if err != nil {
			t.Fatal(report("reverse failed", err))
		}
		back, err := Patch(b, rev)
		if err != nil {
			t.Fatal(report("patch(b, reverse) failed", encode.MustString(rev), err))
		}
		if back.String != aStr {
			t.Fatal(report("patch(b, reverse) != a", encode.MustString(rev), back.String))
		}
	}
	// a corpus which stopped reaching the diffs this is here to exercise would
	// go on passing and mean nothing
	if byLine < 100 || byRune < 100 {
		t.Errorf("thin corpus: %d line keyed and %d rune keyed diffs", byLine, byRune)
	}
}

// A strdiff is relative: it says what changed about the string that was there,
// so it meets documents it was not made from -- ones which have moved, and ones
// a malformed diff never described.  Every one of those is an error, and none of
// them is a read off the end of the document.
var strDiffFailTests = []struct {
	name  string
	doc   string
	patch string
	want  string
}{{
	name:  "keys past the end of the lines",
	doc:   "one\ntwo\nthree",
	patch: "!strdiff(true)\n9: !insert four",
	want:  "reaches line 9 of 3",
}, {
	name:  "keys past the end of the runes",
	doc:   "hello",
	patch: "!strdiff(false)\n9: !insert '!'",
	want:  "reaches rune 9 of 5",
}, {
	name:  "deleting what is not there",
	doc:   "hello world",
	patch: "!strdiff(false)\n2: !delete zz",
	want:  "unexpected text",
}, {
	name:  "deleting more lines than there are",
	doc:   "one\ntwo\nthree",
	patch: "!strdiff(true)\n1: !delete |-\n  two\n  three\n  four",
	want:  "unexpected text",
}, {
	name:  "replacing what is not there",
	doc:   "hello world",
	patch: "!strdiff(false)\n2: !replace\n  from: zz\n  to: xx",
	want:  "unexpected text",
}, {
	name:  "a key inside the operation before it",
	doc:   "hello world",
	patch: "!strdiff(false)\n0: !replace\n  from: he\n  to: HE\n1: !insert y",
	want:  "inside the operation before it",
}}

func TestDiffStringFails(t *testing.T) {
	for _, test := range strDiffFailTests {
		t.Run(test.name, func(t *testing.T) {
			patch, err := parse.Parse([]byte(test.patch))
			if err != nil {
				t.Fatalf("parse patch: %v", err)
			}
			got, err := Patch(ir.FromString(test.doc), patch)
			if err == nil {
				t.Fatalf("patched to %q, expected an error mentioning %q",
					got.String, test.want)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("error is %q, expected it to mention %q", err, test.want)
			}
		})
	}
}
