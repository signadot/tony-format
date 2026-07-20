package server

import (
	"fmt"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
)

// mountPart is the portion of a client patch that falls under one mount.
type mountPart struct {
	mount *MountEntry
	data  *ir.Node // the sub-patch data, rooted at the mount path
}

// baseWrite is a portion of a client patch that falls outside every mount, to be
// written by docd over its own logd link. Each base write sits at a path that is
// not an ancestor of any mount path, so it never conflicts with a mount
// participant in the transaction merge (which rejects a patch whose path is a
// prefix of another's).
type baseWrite struct {
	path string
	data *ir.Node
}

// TagFilter reports whether a single tag head blocks static decomposition when it
// sits on a node ABOVE a mount boundary. The head is passed as it appears on the
// node, including its leading "!" (e.g. "!all", "!dive"). Only tags above a mount
// boundary are consulted — a tag inside a single mount's subtree is never
// filtered, since that subtree is handed to its controller untouched.
type TagFilter func(tagHead string) bool

// defaultTagFilter blocks decomposition through any tag that names a known merge
// operation (a patch op such as !all/!dive/!insert), whose per-mount effect
// cannot be deduced statically. Tags that are not merge ops (schema type refs,
// other non-patch tags) are treated as safe to descend through. Override via the
// server config to refine the classification.
//
// Convention: tag heads carry a leading "!" but the mergeop registry keys them
// without it, so the bang is stripped before lookup.
func defaultTagFilter(head string) bool {
	return mergeop.Lookup(strings.TrimPrefix(head, "!")) != nil
}

// blockedTag walks a (possibly compound) tag string and returns the first tag
// head the filter rejects, if any.
func blockedTag(compound string, blocks TagFilter) (string, bool) {
	for t := compound; t != ""; {
		hd, _, rest := ir.TagArgs(t)
		if hd != "" && blocks(hd) {
			return hd, true
		}
		if rest == t { // guard against non-advancing parse
			break
		}
		t = rest
	}
	return "", false
}

// mountInfo pairs a live mount with its pre-split path segments.
type mountInfo struct {
	entry *MountEntry
	segs  []string
}

// splitPatch partitions a client patch (path, data) across the live mounts and a
// base remainder, so a single client patch spanning multiple mounts can be
// committed atomically. Data is first rooted at the document root (nested under
// path); each mount claims the subtree at its path (longest-prefix wins for
// nested mounts), and whatever is left is the base remainder served by docd's own
// logd link.
//
// docd must decompose statically, so it is deliberately less expressive than
// logd: a higher-order merge op (a tagged node, e.g. !all/!dive) or a non-object
// that sits ABOVE a mount boundary cannot be attributed to a specific controller,
// so splitPatch rejects it. Ops WITHIN a single mount's subtree are fine — that
// subtree is handed to the controller untouched.
func splitPatch(reg *MountRegistry, path string, data *ir.Node, blocks TagFilter) (parts []mountPart, base []baseWrite, err error) {
	if blocks == nil {
		blocks = defaultTagFilter
	}
	clientFields, err := pathFields(path)
	if err != nil {
		return nil, nil, err
	}
	full := nestAtFields(clientFields, data)

	var mounts []mountInfo
	for _, m := range reg.List() {
		if !m.Live() {
			continue
		}
		mf, ferr := pathFields(m.Path)
		if ferr != nil {
			continue // mount paths are validated at registration
		}
		mounts = append(mounts, mountInfo{entry: m, segs: mf})
	}

	parts, baseTree, perr := partition(full, nil, mounts, blocks)
	if perr != nil {
		return nil, nil, perr
	}
	return parts, emitBase(baseTree, nil, mounts), nil
}

// emitBase decomposes the base remainder into writes at paths that are not
// ancestors of any mount path (so they don't conflict with mount participants in
// the transaction merge). A node with no mount beneath it is emitted whole at its
// path; a spine node that is an ancestor of a mount is recursed into.
func emitBase(n *ir.Node, cur []string, mounts []mountInfo) []baseWrite {
	if isEmptyObject(n) {
		return nil
	}
	if !anyMountUnder(cur, mounts) {
		return []baseWrite{{path: fieldsToKPath(cur), data: n.Clone()}}
	}
	if n.Type != ir.ObjectType {
		return nil // defensive: spine nodes above a mount are plain objects
	}
	var out []baseWrite
	for i := range n.Fields {
		childSegs := append(append([]string{}, cur...), n.Fields[i].String)
		out = append(out, emitBase(n.Values[i], childSegs, mounts)...)
	}
	return out
}

// anyMountUnder reports whether any mount path lies strictly below cur.
func anyMountUnder(cur []string, mounts []mountInfo) bool {
	for _, mi := range mounts {
		if !fieldsEqual(mi.segs, cur) && hasFieldPrefix(mi.segs, cur) {
			return true
		}
	}
	return false
}

// partition recursively splits the node at path cur into per-mount parts and a
// base remainder, erroring if a higher-order op above a mount boundary prevents
// static decomposition.
func partition(n *ir.Node, cur []string, mounts []mountInfo, blocks TagFilter) ([]mountPart, *ir.Node, error) {
	var exact *MountEntry
	deeper := false
	for _, mi := range mounts {
		switch {
		case fieldsEqual(mi.segs, cur):
			exact = mi.entry
		case hasFieldPrefix(mi.segs, cur): // cur is a strict prefix of a mount path
			deeper = true
		}
	}

	// No mount lies below this node: the whole subtree belongs either to the mount
	// rooted exactly here, or to the base remainder. Either way it is handed over
	// untouched (any ops within are the recipient's to interpret).
	if !deeper {
		if exact != nil {
			return []mountPart{{mount: exact, data: cloneOrNil(n)}}, nil, nil
		}
		return nil, cloneOrNil(n), nil
	}

	// A mount lies below this node, so docd must descend into it — which is only
	// possible through an object whose tags (if any) do not transform the subtree.
	// A non-object, or a tag the filter rejects (a higher-order patch op), spans a
	// mount boundary and cannot be decomposed statically.
	if n == nil || n.Type != ir.ObjectType {
		return nil, nil, fmt.Errorf(
			"cannot decompose patch across mounts: %s at %q spans a mount boundary",
			describeUndecomposable(n), fieldsToKPath(cur))
	}
	if op, ok := blockedTag(n.Tag, blocks); ok {
		return nil, nil, fmt.Errorf(
			"cannot decompose patch across mounts: higher-order op %q at %q spans a mount boundary",
			op, fieldsToKPath(cur))
	}

	var parts []mountPart
	var baseKV []ir.KeyVal
	for i := range n.Fields {
		key := n.Fields[i].String
		childSegs := append(append([]string{}, cur...), key)
		cparts, cbase, err := partition(n.Values[i], childSegs, mounts, blocks)
		if err != nil {
			return nil, nil, err
		}
		parts = append(parts, cparts...)
		if !isEmptyObject(cbase) {
			baseKV = append(baseKV, ir.KeyVal{Key: ir.FromString(key), Val: cbase})
		}
	}

	var baseNode *ir.Node
	if len(baseKV) > 0 {
		baseNode = ir.FromKeyVals(baseKV)
	}
	// If a mount is rooted exactly here (with deeper mounts nested below it), the
	// remainder at this level — fields not claimed by a deeper mount — is that
	// mount's data.
	if exact != nil && baseNode != nil {
		parts = append(parts, mountPart{mount: exact, data: baseNode})
		baseNode = nil
	}
	return parts, baseNode, nil
}

func describeUndecomposable(n *ir.Node) string {
	if n == nil {
		return "missing value"
	}
	if n.Tag != "" {
		return fmt.Sprintf("higher-order op %q", n.Tag)
	}
	return fmt.Sprintf("non-object value (%v)", n.Type)
}

// nestAtFields roots data at the document root by wrapping it under each field
// of the client patch path. No fields returns data unchanged.
func nestAtFields(fields []string, data *ir.Node) *ir.Node {
	node := data
	for i := len(fields) - 1; i >= 0; i-- {
		node = ir.FromKeyVals([]ir.KeyVal{{Key: ir.FromString(fields[i]), Val: node}})
	}
	return node
}

// cloneOrNil deep-copies n, or returns nil when n carries no data, so callers get
// a detached subtree that encoding cannot mutate back into the client's request.
func cloneOrNil(n *ir.Node) *ir.Node {
	if isEmptyObject(n) {
		return nil
	}
	return n.Clone()
}

// patchTagFilter returns the server's configured cross-mount tag filter, or the
// default.
func (s *Server) patchTagFilter() TagFilter {
	if s.Spec.PatchTagFilter != nil {
		return s.Spec.PatchTagFilter
	}
	return defaultTagFilter
}

// isEmptyObject reports whether n carries no data — nil, null, or an object with
// no fields.
func isEmptyObject(n *ir.Node) bool {
	if n == nil {
		return true
	}
	if n.Type == ir.NullType {
		return true
	}
	return n.Type == ir.ObjectType && len(n.Fields) == 0
}
