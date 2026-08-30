package storage

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// Keyed arrays, and what a DIFF has to know about them.
//
// A keyed array merges by identity: !key(f) says an element is the one whose f matches,
// not the one at that index. Diffing two states of one is therefore only possible if
// something says which field keys it -- and stored state is op-free, so the tag is not in
// the states themselves. Two answers, and this file is both:
//
//	the schema declares the key    annotate both sides before diffing, and the diff takes
//	                               its keyed branch (annotateKeyed, keyedArrayPaths)
//	the client's patch declares it the schema cannot annotate anything, so the diff would
//	                               come out POSITIONAL (patchHasUndeclaredKey)
//
// A positional delta is not merely less precise: stating an array by index takes
// ownership of the whole of it, so baseline adds an element and the scope never sees it.
// That is why an undeclared key means the write is not lowered at all -- the client's own
// patch is kept, because the patch is the only thing carrying the fact. See lower.go.

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
