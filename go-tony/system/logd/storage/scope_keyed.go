package storage

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// What a scope's KEYED arrays mean to the code that has to say what a scope holds.
//
// A keyed array merges by identity: !key(f) says an element is the one whose f matches,
// not the one at that index. Stored state is op-free, so the only thing that can say an
// array is keyed is the schema -- and a write whose key the schema does not declare
// carries that fact in the patch and nowhere else.
//
// That is why lowering refuses such a write rather than diffing it (lower.go): a diff
// over op-free state comes out POSITIONAL, and a positional delta takes ownership of the
// whole array, so baseline adds an element and the scope never sees it.
//
// This was the scope overlay's file, and what is left is what outlived it. The overlay
// asked per scoped READ whether a scope held keyed paths the schema does not declare, and
// kept a cache to make that affordable; with the overlay gone nothing asks, so the
// question, its cache and the walk behind it went too. Lowering asks a different one, per
// WRITE and from the patch in hand: does THIS patch carry a key the schema has not
// declared. That is what remains here.

// patchHasUndeclaredKey reports whether the patch keys an array the schema does not
// declare -- a !key the client supplied for a path the schema has never heard of, which
// nothing can annotate a materialized state for.
func patchHasUndeclaredKey(n *ir.Node, prefix string, keys map[string]string) bool {
	if n == nil {
		return false
	}
	// A comment wraps the value it precedes, and the !key this looks for is on the
	// array inside it (3cdjz00jh12krns4g1n0).
	n = ir.Uncomment(n)
	if n.Type == ir.ArrayType {
		if _, keyed := n.KeyField(); keyed {
			if _, declared := keys[prefix]; !declared {
				return true
			}
		}
	}
	switch n.Type {
	case ir.ObjectType:
		for i, f := range n.Fields {
			p := kpath.ChildField(prefix, f.String)
			if i < len(n.Values) && patchHasUndeclaredKey(n.Values[i], p, keys) {
				return true
			}
		}
	case ir.ArrayType:
		for _, v := range n.Values {
			if patchHasUndeclaredKey(v, prefix, keys) {
				return true
			}
		}
	}
	return false
}

// keyedArrayPaths is the schema's keying as a diff needs it: array path -> key field,
// over both declarations, since auto-id is keying that also generates.
func (s *Storage) keyedArrayPaths() map[string]string {
	sch := s.schemaForScope(nil)
	if sch == nil {
		return nil
	}
	out := map[string]string{}
	for _, f := range sch.AutoIDFields {
		out[f.Path] = f.Field
	}
	for _, f := range sch.KeyFields {
		out[f.Path] = f.Field
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// annotateKeyed tags arrays at the schema's keyed paths with !key(f), in place.
//
// Stored state is op-free, so diffArray cannot take its keyed branch: it needs !key(f) on
// BOTH sides. Without this the diff comes out POSITIONAL and lands the scope's elements
// by index -- and re-asserted on every read, a positional diff duplicates them. Annotating
// is legitimate exactly where storing the tag is not: a lowered delta is a WRITE, and a write
// is where ops belong.
//
// Elements inherit the array's path, matching api.AutoIDField.Path ("orders.items") and
// indexPatchRec's own recursion. Node.GetPath returns a Clone, so this tags on the way
// down rather than by lookup.
func annotateKeyed(n *ir.Node, prefix string, keys map[string]string) {
	if n == nil {
		return
	}
	// The tag belongs to the array, not to what was said above it, so the wrapper a
	// head comment makes is looked through and the array inside is tagged in place
	// (3cdjz00jh12krns4g1n0).
	n = ir.Uncomment(n)
	if f, ok := keys[prefix]; ok && n.Type == ir.ArrayType {
		if _, args := ir.TagGet(n.Tag, "!key"); len(args) != 1 {
			n.Tag = ir.TagCompose("!key", []string{f}, n.Tag)
		}
	}
	switch n.Type {
	case ir.ObjectType:
		for i, fld := range n.Fields {
			p := kpath.ChildField(prefix, fld.String)
			if i < len(n.Values) {
				annotateKeyed(n.Values[i], p, keys)
			}
		}
	case ir.ArrayType:
		for _, v := range n.Values {
			annotateKeyed(v, prefix, keys)
		}
	}
}
