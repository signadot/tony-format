package tony

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
)

// !nullify answers a new null, wearing the document's tag, and leaves the document it
// was given alone. It rewrote it: after Patch(doc, {spec: !nullify null}), doc.spec was
// itself null (fk1vg9sxh12ksyxxmdn0).
func TestNullifyLeavesTheDocumentAlone(t *testing.T) {
	for _, src := range []string{"{spec: {a: 1}, k: 1}", "{spec: !t {a: 1}, k: 1}", "{spec: x # note\nk: 1}"} {
		t.Run(src, func(t *testing.T) {
			doc, err := parse.Parse([]byte(src), parse.ParseComments(true))
			if err != nil {
				t.Fatal(err)
			}
			patch, err := parse.Parse([]byte("{spec: !nullify null}"))
			if err != nil {
				t.Fatal(err)
			}
			before := doc.Clone()
			res, err := Patch(doc, patch, mergeop.Comments(true))
			if err != nil {
				t.Fatal(err)
			}
			if !doc.DeepEqualWithComments(before) {
				t.Errorf("the document changed:\nwas %s\nnow %s", encode.MustString(before), encode.MustString(doc))
			}
			spec, _ := res.GetKPath("spec")
			if spec == nil || spec.Type.String() != "Null" {
				t.Fatalf("spec = %v, want null", spec)
			}
			was, _ := before.GetKPath("spec")
			if want := was.Tag; spec.Tag != want {
				t.Errorf("spec is tagged %q, want the document's %q", spec.Tag, want)
			}
		})
	}
}
