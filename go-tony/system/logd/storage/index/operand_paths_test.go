package index

import (
	"sort"
	"strings"
	"testing"
)

// An operand is not ordinary structure, and the walk used to treat it as if it were.
// mergeop.OperandPaths is the one place that knows the difference; this pins what the
// index does with the answer.
func TestIndexDoesNotWalkIntoAnOperand(t *testing.T) {
	tests := []struct {
		name, src string
		want      []string
	}{{
		// head: and line: name comment POSITIONS. They were indexed as though the
		// document had fields by those names.
		name: "a comment's positions are not paths",
		src:  `{a: !comment {head: ["# note"]}}`,
		want: []string{"", "a"},
	}, {
		name: "and its lines are not elements",
		src:  `{a: !comment {head: ["# one", "# two"], line: ["# latch"]}}`,
		want: []string{"", "a"},
	}, {
		// The escape hides operations, not paths: a stored rule at spec.rules is
		// read back at spec.rules, so those paths have to be recorded.
		name: "an escaped value keeps its paths",
		src:  `{spec: !raw {rules: {x: 1}}}`,
		want: []string{"", "spec", "spec.rules", "spec.rules.x"},
	}, {
		// What a delete carries is what WENT AWAY, and recording those paths is
		// how an absent read knows the document no longer has them.
		name: "a delete records what it removed",
		src:  `{a: !delete {b: {c: 1}}}`,
		want: []string{"", "a", "a.b", "a.b.c"},
	}, {
		name: "an insert records where its value lands",
		src:  `{a: !insert {b: 1}}`,
		want: []string{"", "a", "a.b"},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := indexedPaths(t, test.src)
			sort.Strings(got)
			want := append([]string(nil), test.want...)
			sort.Strings(want)
			if strings.Join(got, " ") != strings.Join(want, " ") {
				t.Errorf("indexed %v, want %v", got, want)
			}
		})
	}
}
