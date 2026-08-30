package mergeop_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

func oneLine(n *ir.Node) string {
	if n == nil {
		return "<nil>"
	}
	return strings.Join(strings.Fields(encode.MustString(n)), " ")
}

// OperandPaths is the one place that knows which parts of an operand are document
// values. Every walk over a patch needs that answer and each of them used to guess
// it, by descending into an operand as if it were ordinary structure.
func TestOperandPaths(t *testing.T) {
	tests := []struct {
		name, src string
		ok        bool
		want      []string // "suffix=value", sorted
	}{{
		// The operand names comment positions, which are not paths. Walking into
		// it is what indexed a.head and a.head[0] for a comment write.
		name: "a comment states positions, not paths",
		src:  `!comment {head: ["# note"]}`,
		ok:   true,
	}, {
		// The escape hides OPERATIONS, not paths: the data lands where the escape
		// is, so a stored rule at spec.rules is read at spec.rules.
		name: "the escape hides operations, not paths",
		src:  `!raw {a: 1, b: {c: 2}}`,
		ok:   true,
		want: []string{"=a: 1 b: { c: 2 }"},
	}, {
		name: "a string edit script is counts, not paths",
		src:  `!strdiff [1, 2]`,
		ok:   true,
	}, {
		// What an insert carries becomes the value AT that path, not below it.
		name: "an insert carries the value, at the same path",
		src:  `!insert {a: 1}`,
		ok:   true,
		want: []string{"=a: 1"},
	}, {
		// The payload of a delete is what WENT AWAY. Its paths are how the index
		// records that the document no longer has them.
		name: "a delete carries what went away",
		src:  `!delete {a: 1}`,
		ok:   true,
		want: []string{"=a: 1"},
	}, {
		name: "a checked replace describes one path twice",
		src:  `!replace {from: {a: 1}, to: {a: 2}}`,
		ok:   true,
		want: []string{"={ a: 1 }", "={ a: 2 }"},
	}, {
		// The one operand whose parts sit BELOW the operation.
		name: "an array diff names positions",
		src:  `!arraydiff {0: {a: 1}, 2: {b: 2}}`,
		ok:   true,
		want: []string{"[0]={ a: 1 }", "[2]={ b: 2 }"},
	}, {
		// !key annotates the array rather than consuming it, and the caller's own
		// array handling knows how to name keyed elements.
		name: "a keyed list is left to the caller",
		src:  `!key(sku) [{sku: "A"}]`,
	}, {
		// No operation: the caller walks it as ordinary structure, which is what
		// it did before this existed.
		name: "a plain value is not this function's business",
		src:  `{a: 1}`,
	}, {
		name: "nor is a data tag",
		src:  `!t1 {a: 1}`,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			n, err := parse.Parse([]byte(test.src))
			if err != nil {
				t.Fatalf("parse: %s", err)
			}
			ops, ok := mergeop.OperandPaths(n)
			if ok != test.ok {
				t.Fatalf("ok=%v, want %v", ok, test.ok)
			}
			var got []string
			for _, o := range ops {
				got = append(got, fmt.Sprintf("%s=%s", o.Suffix, oneLine(o.Node)))
			}
			sort.Strings(got)
			want := append([]string(nil), test.want...)
			sort.Strings(want)
			if strings.Join(got, " | ") != strings.Join(want, " | ") {
				t.Errorf("answered %v, want %v", got, want)
			}
		})
	}
}
