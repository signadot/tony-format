package storage

import (
	"bytes"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// What the index can name as an element is exactly what a name can spell: a scalar key,
// with its type. Two elements never share a path -- a number and the string of its
// digits are two names -- and an element whose key cannot be spelled is refused at the
// write rather than collapsed onto a path another element holds (element_identity.md).
func TestIndexKeyRange(t *testing.T) {
	for _, tc := range []struct {
		name, write string
		wantNames   []string // element names the index ends up with
		refused     string   // or why the write is refused
	}{
		{"string keys", `{items: [{sku: "A", q: 1}, {sku: "B", q: 2}]}`, []string{"(sku=A)", "(sku=B)"}, ""},
		{"number keys", `{items: [{sku: 1, q: 1}, {sku: 2, q: 2}]}`, []string{"(sku=1)", "(sku=2)"}, ""},
		{"number and string that render alike", `{items: [{sku: 1, q: 1}, {sku: "1", q: 2}]}`, []string{"(sku=1)", `(sku="1")`}, ""},
		{"object-valued key", `{items: [{sku: {a: 1}, q: 1}, {sku: {a: 2}, q: 2}]}`, nil, "no name"},
		{"two elements, one name", `{items: [{sku: "A", q: 1}, {sku: "A", q: 2}]}`, nil, "names two elements"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStorage(t)
			declareKeyed(t, s, `{define: {items: {sku: !logd-key null}}}`)
			err := scopedCommit(t, s, nil, "", tc.write)
			if tc.refused != "" {
				if err == nil || !strings.Contains(err.Error(), tc.refused) {
					t.Fatalf("want a refusal saying %q, got %v", tc.refused, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			elems := map[string]bool{}
			for _, p := range indexPathSet(s) {
				first, rest := kpath.Split(p)
				if first != "items" || rest == "" {
					continue
				}
				seg, _ := kpath.Split(rest)
				if name, isField := kpath.SegmentFieldName(seg); isField {
					elems[name] = true
				}
			}
			for _, want := range tc.wantNames {
				if !elems[want] {
					t.Errorf("index has no element %q; has %v", want, elems)
				}
			}
			if len(elems) != len(tc.wantNames) {
				t.Errorf("index has %d element paths %v, want %d", len(elems), elems, len(tc.wantNames))
			}
		})
	}
}

// encodeWire renders a node the way the wire carries it.
func encodeWire(t *testing.T, n *ir.Node) string {
	t.Helper()
	var buf bytes.Buffer
	if err := encode.Encode(n, &buf, encode.EncodeWire(true)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.String()
}
