package mergeop_test

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

// FindUnsafe asks of a whole patch what Unsafe asks of one tag, so a caller can
// refuse the patch rather than discover it while applying one.
func TestFindUnsafe(t *testing.T) {
	for _, tc := range []struct {
		name, patch, want string
	}{
		{name: "no operation at all", patch: `{a: 1}`},
		{name: "a storable operation", patch: `{a: !insert 1}`},
		{name: "at the root", patch: `!pipe "date"`, want: "pipe"},
		{name: "under a field", patch: `{a: {b: !pipe "date"}}`, want: "pipe"},
		{name: "in an array element", patch: `{a: [1, !pipe "date"]}`, want: "pipe"},
		{name: "behind a label which is not an op", patch: `{a: !lbl.pipe "date"}`, want: "pipe"},
		{name: "after a storable op in the chain", patch: `{a: !insert.pipe "date"}`, want: "pipe"},
		{
			// The escape is the whole point: a document which CONTAINS a patch is
			// data, and refusing it would make a charter or a stored rule unstorable
			// (6225etzfh12kr955fxn0).
			name:  "beneath !raw, where nothing is interpreted",
			patch: `{a: !raw {b: !pipe "date"}}`,
		},
		{name: "on the node !raw itself escapes", patch: `{a: !pipe.raw "date"}`, want: "pipe"},
		{name: "under a comment", patch: "{a: {\n# note\nb: !pipe \"date\"}}", want: "pipe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := parse.Parse([]byte(tc.patch))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			op, found := mergeop.FindUnsafe(p)
			if tc.want == "" {
				if found {
					t.Errorf("%s: reported !%s, which executes nothing", tc.patch, op)
				}
				return
			}
			if !found {
				t.Errorf("%s: found nothing; !%s executes", tc.patch, tc.want)
			} else if op != tc.want {
				t.Errorf("%s: got !%s, want !%s", tc.patch, op, tc.want)
			}
		})
	}
}
