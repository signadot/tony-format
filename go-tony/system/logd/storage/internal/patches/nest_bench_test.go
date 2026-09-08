package patches

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// What the fold pays to nest a patch under a remainder, which it does per record on a
// read. It is a benchmark rather than a comment because the cost that was here was
// QUADRATIC in the path's length -- a parse of the whole remainder per segment, plus a
// re-quote of every segment and an unquote of each one back again -- and nothing in the
// suite would have noticed it growing.
//
// The deep path's segments must be quoted because they carry a / and a #, which is what a
// source id looks like; the plain one is the same shape without them, so the two together
// say how much of the cost is the quoting and how much is the walk.
var deepQuoted = `github.issue."signadot/verse#42".comments."8ab3c1/x#7".body`
var deepPlain = `github.issue.sig42.comments.c8ab3c1.body`

var benchVals = []*ir.Node{ir.FromString("v")}

func BenchmarkNestPatches_Quoted(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := nestPatches(deepQuoted, benchVals); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNestPatches_Plain(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := nestPatches(deepPlain, benchVals); err != nil {
			b.Fatal(err)
		}
	}
}
