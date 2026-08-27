package tony

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

// The point of the tag being right: the operator runs. `{a !delete, keep: me}`
// stored an operator-tagged null instead of removing the field -- worse than a
// no-op, since a consumer's guard asks whether a tag names a known non-patch
// operator and an UNKNOWN tag passes, having nothing to look up.
func TestShortDeleteActuallyDeletes(t *testing.T) {
	for _, patch := range []string{
		`{a !delete, keep: me}`,
		`{a !delete }`,
		`{a: !delete null}`,
	} {
		doc, err := parse.Parse([]byte(`{a: yes, keep: me}`), parse.ParseTony())
		if err != nil {
			t.Fatal(err)
		}
		pat, err := parse.Parse([]byte(patch), parse.ParseTony())
		if err != nil {
			t.Fatalf("%s: %v", patch, err)
		}
		got, err := Patch(doc, pat)
		if err != nil {
			t.Fatalf("%s: %v", patch, err)
		}
		out := encode.MustString(got)
		if strings.Contains(out, "a:") {
			t.Errorf("%s did not delete a:\n%s", patch, out)
		}
		if !strings.Contains(out, "keep") {
			t.Errorf("%s lost keep:\n%s", patch, out)
		}
	}
}
