package storage

import (
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
)

// Under a schema-authoritative keying rule (plan P1) stored state stays op-free, so
// diffArray cannot take its keyed branch -- it needs !key(f) on BOTH sides. The question
// this answers: does that force a change to Diff's recursion (which carries no path, so
// a path-keyed schema cannot be consulted inside it), or can the overlay builder just
// annotate the two states before diffing?
//
// Annotating is legitimate where storing the tag is not: the overlay is a WRITE, and a
// write is where ops are allowed to live.

// annotateKeys tags the array at each given path with !key(field), the way a schema
// lookup would. Only the shapes this test needs -- one level of field navigation.
func annotateKeys(n *ir.Node, keys map[string]string) *ir.Node {
	if n == nil || n.Type != ir.ObjectType {
		return n
	}
	for i, f := range n.Fields {
		field, ok := keys[f.String]
		if !ok {
			continue
		}
		v := n.Values[i]
		if v.Type != ir.ArrayType {
			continue
		}
		if _, args := ir.TagGet(v.Tag, "!key"); len(args) == 1 {
			continue // already keyed
		}
		v.Tag = ir.TagCompose("!key", []string{field}, v.Tag)
	}
	return n
}

// TestOverlayKeyAnnotation runs the keyed case that came out positional in
// scope_overlay_diff_test.go, but annotates both states from "schema" first.
func TestOverlayKeyAnnotation(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	scope := "s1"
	keys := map[string]string{"items": "sku"}

	scalingCommit(t, s, nil, `{items: !key(sku) [{sku: "W", q: 1}]}`, nil)
	scalingCommit(t, s, &scope, `{items: !key(sku) [{sku: "G", q: 3}]}`, nil)

	// Build the overlay at T from the two materialized states, annotated.
	commitT, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	baseT, err := s.ReadStateAt("", commitT, nil)
	if err != nil {
		t.Fatalf("baseline read: %v", err)
	}
	scopedT, err := s.ReadStateAt("", commitT, &scope)
	if err != nil {
		t.Fatalf("scoped read: %v", err)
	}
	t.Logf("baseline@T (as stored, op-free): %s", encodeWire(t, baseT))
	t.Logf("scoped@T   (as stored, op-free): %s", encodeWire(t, scopedT))

	plain := tony.Diff(baseT.Clone(), scopedT.Clone())
	t.Logf("overlay WITHOUT annotation: %s", nodeOrNil(t, plain))

	overlay := tony.Diff(
		annotateKeys(baseT.Clone(), keys),
		annotateKeys(scopedT.Clone(), keys),
	)
	t.Logf("overlay WITH annotation:    %s", nodeOrNil(t, overlay))

	// Baseline moves on: it adds its own keyed element after T.
	scalingCommit(t, s, nil, `{items: !key(sku) [{sku: "S", q: 1}]}`, nil)

	commit2, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	base2, err := s.ReadStateAt("", commit2, nil)
	if err != nil {
		t.Fatalf("baseline read: %v", err)
	}
	got, err := tony.Patch(base2.Clone(), overlay.Clone())
	if err != nil {
		t.Fatalf("apply annotated overlay: %v", err)
	}
	viaOverlay := encodeWire(t, got)
	viaReplay := showDocQuiet(t, s, &scope)
	t.Logf("via annotated overlay: %s", viaOverlay)
	t.Logf("via replay:            %s", viaReplay)

	gotSkus := skus(got, "items")
	wantSkus := skus(mustReadScopeAt(t, s, commit2, &scope), "items")
	if !sameSet(gotSkus, wantSkus) {
		t.Errorf("annotated overlay lost or gained elements: got %v want %v", gotSkus, wantSkus)
	}
	if viaOverlay != viaReplay {
		t.Logf("NOTE: same elements, but the rendering differs -- %s vs %s", viaOverlay, viaReplay)
	}
}

func mustReadScopeAt(t *testing.T, s *Storage, commit int64, scope *string) *ir.Node {
	t.Helper()
	n, err := s.ReadStateAt("", commit, scope)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return n
}
