package mergeop_test

import (
	"strings"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// TestAllDropsWhatItsChildDeletes: a child patch reports a deletion by returning
// nil, and an element it deleted is gone rather than nil. !all kept the nil, so
// ir.FromSlice dereferenced it and the process died.
//
// The composition this makes work is worth noting: !all.if with a !delete in one
// branch filters a list in place.
func TestAllDropsWhatItsChildDeletes(t *testing.T) {
	for _, tc := range []struct {
		name, doc, patch string
		want             []string
		gone             []string
	}{
		{
			name:  "a list, filtered by the branch that deletes",
			doc:   "- {name: a, state: open}\n- {name: b, state: closed}\n",
			patch: "!all.if {if: {state: open}, then: !pass null, else: !delete null}",
			want:  []string{"name: a"},
			gone:  []string{"name: b"},
		},
		{
			name:  "an object, whose values the child deletes",
			doc:   "{a: {keep: 1}, b: {keep: 1}}",
			patch: "!all.delete null",
			gone:  []string{"keep"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := parse.Parse([]byte(tc.doc))
			if err != nil {
				t.Fatal(err)
			}
			p, err := parse.Parse([]byte(tc.patch))
			if err != nil {
				t.Fatalf("parse patch: %v", err)
			}
			got, err := tony.Patch(doc, p)
			if err != nil {
				t.Fatalf("patch: %v", err)
			}
			out := ""
			if got != nil {
				out = encode.MustString(got)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("result lost %q:\n%s", w, out)
				}
			}
			for _, g := range tc.gone {
				if strings.Contains(out, g) {
					t.Errorf("result kept %q, which the patch deleted:\n%s", g, out)
				}
			}
		})
	}
}
