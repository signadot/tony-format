package tony

import "testing"

// !tag matches the DATA tags of a value, whichever way it was written: a flow-style
// `!mytag {x: 1}` carries !bracket.mytag, and reading the head label answered false
// where the block spelling answered true (p478tacqh12krg32msn0 item 4).
func TestTagMatchesThroughPresentation(t *testing.T) {
	for _, tc := range []struct {
		doc, pat string
		want     bool
	}{
		{"c: !mytag {x: 1}", "c: !tag {name: mytag}", true},
		{"c: !mytag\n  x: 1\n", "c: !tag {name: mytag}", true},
		{"c: !key(name) [{name: a}]", "c: !tag {name: key, args: [name]}", true},
		{"c: {x: 1}", "c: !tag {name: \"\"}", true}, // bracketed, but no data tag
		{"c: !other {x: 1}", "c: !tag {name: mytag}", false},
	} {
		got, err := Match(parseT(t, tc.doc), parseT(t, tc.pat))
		if err != nil {
			t.Fatalf("%s ~ %s: %v", tc.doc, tc.pat, err)
		}
		if got != tc.want {
			t.Errorf("%s ~ %s = %v, want %v", tc.doc, tc.pat, got, tc.want)
		}
	}
}
