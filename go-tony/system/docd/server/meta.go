package server

import (
	"sort"

	"github.com/signadot/tony-format/go-tony/ir"
)

// metaSegment is the reserved top-level path segment under which docd serves its
// own metadata (mounts, and later schema). It is served by docd directly rather
// than routed to logd or a controller, and controllers may not mount under it.
const metaSegment = ".meta"

// isMetaPath reports whether path is under the reserved .meta subtree.
func isMetaPath(path string) bool {
	segs := splitPathSegments(path)
	return len(segs) > 0 && segs[0] == metaSegment
}

// metaLeaf returns the metadata resource named after .meta (e.g. "mounts" for
// ".meta/mounts"), or "" for ".meta" itself.
func metaLeaf(path string) string {
	segs := splitPathSegments(path)
	if len(segs) < 2 || segs[0] != metaSegment {
		return ""
	}
	return segs[1]
}

// metaIndexDoc lists the metadata resources docd serves, so .meta is
// self-describing.
func metaIndexDoc() *ir.Node {
	return ir.FromKeyVals([]ir.KeyVal{
		{Key: ir.FromString("resources"), Val: ir.FromSlice([]*ir.Node{
			ir.FromString("mounts"),
			ir.FromString("schema"),
		})},
	})
}

// schemaDoc renders each mount's schema contribution for .meta/schema:
//
//	contributions:
//	- path: /users
//	  status: live
//	  schema: {define: ..., accept: ...}
//
// These are the raw per-mount contributions; composing them into one unified
// schema is future work (component F). Each schema is cloned so building and
// encoding the response never mutates the registry's stored node.
func schemaDoc(entries []*MountEntry) *ir.Node {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })

	items := make([]*ir.Node, 0, len(entries))
	for _, e := range entries {
		status := "live"
		if !e.Live() {
			status = "tombstoned"
		}
		schema := ir.Null()
		if e.Schema != nil {
			schema = e.Schema.Clone()
		}
		items = append(items, ir.FromKeyVals([]ir.KeyVal{
			{Key: ir.FromString("path"), Val: ir.FromString(e.Path)},
			{Key: ir.FromString("status"), Val: ir.FromString(status)},
			{Key: ir.FromString("schema"), Val: schema},
		}))
	}

	return ir.FromKeyVals([]ir.KeyVal{
		{Key: ir.FromString("contributions"), Val: ir.FromSlice(items)},
	})
}

// mountsDoc renders the mount registry as a Tony document for .meta/mounts:
//
//	mounts:
//	- path: /users
//	  controller: user-ctrl
//	  status: live
//	- path: /local
//	  controller: connect
//	  status: tombstoned
//
// Tombstoned mounts (crashed controllers) are included so operators can see them.
func mountsDoc(entries []*MountEntry) *ir.Node {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })

	items := make([]*ir.Node, 0, len(entries))
	for _, e := range entries {
		status := "live"
		if !e.Live() {
			status = "tombstoned"
		}
		items = append(items, ir.FromKeyVals([]ir.KeyVal{
			{Key: ir.FromString("path"), Val: ir.FromString(e.Path)},
			{Key: ir.FromString("controller"), Val: ir.FromString(e.Controller)},
			{Key: ir.FromString("status"), Val: ir.FromString(status)},
		}))
	}

	return ir.FromKeyVals([]ir.KeyVal{
		{Key: ir.FromString("mounts"), Val: ir.FromSlice(items)},
	})
}
