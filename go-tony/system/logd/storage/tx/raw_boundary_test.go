package tx

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A !raw subtree is a document the store CARRIES, not one it owns: the escape says
// to treat it as data, interpreting no operation at any depth, and it lands with its
// own tags intact. Both injectors walked straight in:
//
//	!raw {items: [{sku: A}]}   ->   !raw {items: !key(sku) [{id: a1a0, sku: A}]}
//
// which is the store editing someone else's document -- a rule, a charter, a patch,
// which is what !raw is for. The injected !key is the sharper half: it is an
// OPERATOR tag inside a subtree marked not-to-be-interpreted, and when that payload
// is read back and applied it is an instruction its author never wrote. That is
// 6225etzfh12kr955fxn0's shape arriving by another route.
func TestInjectionStopsAtRaw(t *testing.T) {
	keys := &api.Schema{KeyFields: []api.KeyField{
		{Path: "items", Field: "sku"},
		{Path: "doc.items", Field: "sku"},
	}}
	ids := &api.Schema{AutoIDFields: []api.AutoIDField{
		{Path: "items", Field: "id"},
		{Path: "doc.items", Field: "id"},
	}}

	for _, tc := range []struct {
		name, src string
		touched   bool
	}{
		{"an ordinary write is injected", `{items: [{sku: A, q: 1}]}`, true},
		{"a raw write at the root is not", `!raw {items: [{sku: A, q: 1}]}`, false},
		{"a raw subtree under a field is not", `{doc: !raw {items: [{sku: A, q: 1}]}}`, false},
		{"raw composed with a data tag is not", `{doc: !raw.t1 {items: [{sku: A, q: 1}]}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node, err := parse.Parse([]byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			pd := []*PatcherData{{API: &api.Patch{PathData: api.PathData{Path: "", Data: node}}}}
			InjectAutoIDs(1, ids, pd)
			if err := InjectKeyTags(keys, pd); err != nil {
				t.Fatalf("InjectKeyTags: %v", err)
			}
			got := encode.MustString(pd[0].API.Data)

			hasKey := strings.Contains(got, "!key(sku)")
			hasID := strings.Contains(got, "id:")
			if tc.touched {
				if !hasKey {
					t.Errorf("no !key(sku) injected:\n%s", got)
				}
				if !hasID {
					t.Errorf("no id generated:\n%s", got)
				}
				return
			}
			if hasKey {
				t.Errorf("an operator tag was written into a !raw subtree:\n%s", got)
			}
			if hasID {
				t.Errorf("a field was generated inside a !raw subtree:\n%s", got)
			}
		})
	}
}

// The boundary is the raw node itself: what sits ABOVE it is the store's own
// document and is injected as usual.
func TestInjectionAboveRawIsUnaffected(t *testing.T) {
	keys := &api.Schema{KeyFields: []api.KeyField{{Path: "items", Field: "sku"}}}
	node, err := parse.Parse([]byte(`{items: [{sku: A}], payload: !raw {items: [{sku: B}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	pd := []*PatcherData{{API: &api.Patch{PathData: api.PathData{Path: "", Data: node}}}}
	if err := InjectKeyTags(keys, pd); err != nil {
		t.Fatal(err)
	}
	got := encode.MustString(pd[0].API.Data)
	if n := strings.Count(got, "!key(sku)"); n != 1 {
		t.Errorf("%d keyed arrays, want 1 -- the store's own, not the payload's:\n%s", n, got)
	}
}
