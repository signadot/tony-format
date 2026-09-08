package mergeop_test

import (
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// !field(from,to) renames a field of the object it is applied to, and it answers with a
// new object: the document it was given is not touched. A store that keeps a document
// and steps it by each patch depends on that -- renaming in place made the document and
// the result one object, so the store's diff of the two saw nothing to store.
func TestFieldRenamesWithoutMutatingTheDocument(t *testing.T) {
	doc, err := parse.Parse([]byte(`{x: 1, y: 2}`))
	if err != nil {
		t.Fatal(err)
	}
	patch, err := parse.Parse([]byte(`!field(x,z) null`))
	if err != nil {
		t.Fatal(err)
	}
	before := encode.MustString(doc)
	res, err := tony.Patch(doc, patch)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if got := encode.MustString(doc); got != before {
		t.Errorf("the document was mutated: %s -> %s", before, got)
	}
	if ir.Get(res, "x") != nil || ir.Get(res, "y") == nil || ir.Get(res, "z") == nil ||
		ir.Get(res, "z").Int64 == nil || *ir.Get(res, "z").Int64 != 1 {
		t.Errorf("result %s, want x renamed to z with its value", encode.MustString(res))
	}
	if res == doc {
		t.Error("the result is the document itself")
	}
}
