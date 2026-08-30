package storage

import (
	"strings"

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
// This was the scope overlay's file, and these are what outlived it. The overlay derived
// a scope layer from two documents and could not survive keyed arrays for the same
// reason, among others; it is gone, and a scoped read replays the scope's own patches,
// where !key means what it always meant.

// scopeHasKeyedPaths reports whether any path the scope has written contains a kinded KEY
// segment -- items("G") rather than items[0] or items.field.
//
// Nothing that has to DIFF a scope's state can serve those correctly, and it must not
// pretend to. Two independent reasons, both tracing to the same missing fact -- nothing
// tells the differ what keys an array:
//
//  1. Granularity. A key segment is not a DOTTED descendant, so items("G").q does not make
//     items an ancestor by the prefix test below, and items survives as a "leaf". Whatever
//     re-states it then claims the whole array for the scope, freezing baseline out of it:
//     baseline adds an element and the scope never sees it; baseline updates its own
//     element and the scope keeps the old value. Measured, and silent.
//  2. Even at element granularity, Diff over op-free state cannot take its keyed branch,
//     so the result comes out positional and lands the scope's elements by index.
//
// So such a write is kept as the client sent it rather than lowered (lower.go), which is
// correct and merely less uniform -- the right way round.
func (s *Storage) scopeHasKeyedPaths(scopeID string) bool {
	// Cached, because this walks the WHOLE index and the read path asks on every scoped
	// read. Uncached it cost more than it saved: 53us -> 1.19ms at 400 accumulated
	// writes, turning a flat read back into a linear one. Invalidated where the answer can
	// change -- a scoped write, or a schema change, since "declared" is the schema's word.
	s.scopeKeyedMu.RLock()
	cached, ok := s.scopeKeyedCache[scopeID]
	s.scopeKeyedMu.RUnlock()
	if ok {
		return cached
	}
	res := s.computeScopeHasKeyedPaths(scopeID)
	s.scopeKeyedMu.Lock()
	if s.scopeKeyedCache == nil {
		s.scopeKeyedCache = map[string]bool{}
	}
	s.scopeKeyedCache[scopeID] = res
	s.scopeKeyedMu.Unlock()
	return res
}

// invalidateScopeKeyed forgets what is known about every scope's keyed paths. Called on a
// schema change, since "declared" is the schema's word and a change can redraw the answer
// for every scope at once.
func (s *Storage) invalidateScopeKeyed(scopeID *string) {
	s.scopeKeyedMu.Lock()
	defer s.scopeKeyedMu.Unlock()
	if scopeID == nil {
		s.scopeKeyedCache = nil
		return
	}
	delete(s.scopeKeyedCache, *scopeID)
}

// noteScopeKeyedWrite updates what is known about a scope from the patch just written,
// without re-reading the index.
//
// Dropping the cached answer on every scoped write would be correct and useless: the
// precondition path both writes and reads, so each write would force the next read to walk
// the whole index again -- measured turning a flat CAS into one that grows with the index.
// A write can only ADD keyed paths, and only ones that appear in the patch itself, so the
// patch is enough to decide.
func (s *Storage) noteScopeKeyedWrite(scopeID string, patch *ir.Node) {
	s.scopeKeyedMu.RLock()
	cached, ok := s.scopeKeyedCache[scopeID]
	s.scopeKeyedMu.RUnlock()
	if ok && cached {
		return // already unserviceable; nothing a write can do makes it serviceable again
	}
	if !patchHasUndeclaredKey(patch, "", s.keyedArrayPaths()) {
		return // the answer is unchanged, whatever it was
	}
	s.scopeKeyedMu.Lock()
	if s.scopeKeyedCache == nil {
		s.scopeKeyedCache = map[string]bool{}
	}
	s.scopeKeyedCache[scopeID] = true
	s.scopeKeyedMu.Unlock()
}

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
func (s *Storage) computeScopeHasKeyedPaths(scopeID string) bool {
	keys := s.keyedArrayPaths()
	for _, seg := range s.index.AllSegments() {
		if seg.ScopeID == nil || *seg.ScopeID != scopeID || isOverlaySegment(seg) {
			continue
		}
		p := seg.KindedPath
		if !strings.ContainsRune(p, '(') {
			continue
		}
		// A keyed path the SCHEMA declares is safe: the merge identifies elements the
		// same way (tx.InjectKeyTags puts !key(f) on the write), and storableDelta
		// annotates both sides of its diff from that same schema, so diff, merge and
		// index all key alike.
		//
		// A keyed path the schema does NOT declare exists only because some write carried
		// its own !key tag. Nothing can annotate the states for it, so the diff would go
		// positional while the merge stayed identity-based -- and a positional delta takes
		// ownership of the whole array. Such a write is kept as the client sent it.
		arr := p
		if i := strings.LastIndexByte(arr, ')'); i >= 0 {
			arr = arr[:i+1]
		}
		if _, _, declared := splitKeyedElemPath(arr, keys); !declared {
			return true
		}
	}
	return false
}

// splitKeyedElemPath reports the array path and key field when p names a keyed ELEMENT --
// items("A") rather than items or items.field.
func splitKeyedElemPath(p string, keys map[string]string) (arrayPath, keyField string, ok bool) {
	if len(p) == 0 || p[len(p)-1] != ')' {
		return "", "", false
	}
	open := strings.LastIndexByte(p, '(')
	if open <= 0 {
		return "", "", false
	}
	arrayPath = p[:open]
	f, ok := keys[arrayPath]
	return arrayPath, f, ok
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

// stripPresentationDeepIR removes presentation tags throughout, in place.
func stripPresentationDeepIR(n *ir.Node) *ir.Node {
	if n == nil {
		return nil
	}
	n.Tag = ir.StripPresentation(n.Tag)
	for _, f := range n.Fields {
		stripPresentationDeepIR(f)
	}
	for _, v := range n.Values {
		stripPresentationDeepIR(v)
	}
	return n
}
