package mergeop_test

import (
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
)

func elemWithKey(t *testing.T, list *ir.Node, keyField, key string) *ir.Node {
	t.Helper()
	for _, e := range list.Values {
		if k, ok := ir.ElemKey(e, keyField); ok && k == key {
			return e
		}
	}
	return nil
}

// An element the patch does not name is carried across, not copied. This is what makes a
// single-key write cost the elements it names rather than the array: the identity check is
// the property, since the timing that motivated it is not something a test can assert
// (thqtmm2th12kr051jhn0).
//
// It is also the rule objPatchYWith has always followed for a field the patch does not
// name -- the document's own node, re-parented into the container that now holds it -- so
// this pins the two paths agreeing rather than a local optimisation.
//
// It holds for PatchOwned, whose caller hands the document over. Patch copies the document
// first, so the document keeps its elements, linked to it (qzmkhfjqh12ksydsmdn0).
func TestAKeyedElementThePatchDoesNotNameIsNotCopied(t *testing.T) {
	doc := mustParseNode(t, `{items: !key(sku) [{sku: "A", q: 1}, {sku: "B", q: 2}, {sku: "C", q: 3}]}`)
	patch := mustParseNode(t, `{items: !key(sku) [{sku: "B", q: 99}]}`)

	before := ir.Get(doc, "items")
	untouchedA, untouchedC := elemWithKey(t, before, "sku", "A"), elemWithKey(t, before, "sku", "C")
	if untouchedA == nil || untouchedC == nil {
		t.Fatal("the document does not hold the elements the test is about")
	}

	copied, err := tony.Patch(doc, patch)
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if got := elemWithKey(t, ir.Get(copied, "items"), "sku", "A"); got == untouchedA {
		t.Error(`Patch's result holds the document's own element at "A"`)
	}
	if untouchedA.Parent != before {
		t.Error(`Patch re-parented the document's element at "A" out of it`)
	}

	res, err := tony.PatchOwned(doc, patch)
	if err != nil {
		t.Fatalf("PatchOwned: %v", err)
	}
	after := ir.Get(res, "items")
	if after == nil || len(after.Values) != 3 {
		t.Fatalf("result is %v, want three elements", after)
	}

	if got := elemWithKey(t, after, "sku", "A"); got != untouchedA {
		t.Error(`the element at "A" was copied; a patch that did not name it should carry it across`)
	}
	if got := elemWithKey(t, after, "sku", "C"); got != untouchedC {
		t.Error(`the element at "C" was copied; a patch that did not name it should carry it across`)
	}

	// and the one it DID name still merged, in place, rather than being appended
	b := elemWithKey(t, after, "sku", "B")
	if b == nil {
		t.Fatal(`the element at "B" is gone`)
	}
	if q := ir.Get(b, "q"); q == nil || q.Int64 == nil || *q.Int64 != 99 {
		t.Errorf(`"B".q is %v, want 99`, q)
	}
}

// A carried-across element belongs to the list that now holds it: ir.FromSlice re-parents
// it, so a walk up from the result stays in the result. Sharing the node must not leave it
// pointing at the array it came from.
func TestACarriedElementBelongsToTheListThatHoldsIt(t *testing.T) {
	doc := mustParseNode(t, `{items: !key(sku) [{sku: "A", q: 1}, {sku: "B", q: 2}]}`)
	patch := mustParseNode(t, `{items: !key(sku) [{sku: "B", q: 99}]}`)

	res, err := tony.Patch(doc, patch)
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	after := ir.Get(res, "items")
	a := elemWithKey(t, after, "sku", "A")
	if a == nil {
		t.Fatal(`no element at "A"`)
	}
	if a.Parent != after {
		t.Errorf("the carried element's parent is not the list holding it")
	}
	if a.Root() != res {
		t.Errorf("a walk up from the carried element leaves the result document")
	}
}
