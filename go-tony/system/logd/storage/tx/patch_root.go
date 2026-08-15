package tx

import "github.com/signadot/tony-format/go-tony/ir"

// PatchRootTag marks the root of an API patch in the merged IR tree.
// Used by StreamingProcessor to identify which subtrees need patching.
const PatchRootTag = "!logd-patch-root"

// TagPatchRoots adds PatchRootTag to each patch data's root node.
// Called before MergePatches to mark where patches originate.
//
// The tag goes on the VALUE, looking through the wrapper a head comment makes. A
// tag is a property of a value, and nothing carries one for a comment: the event
// stream writes a comment as its lines and no more, so a tag left on a wrapper
// was silently gone by the time the entry came back out of the log. The patch was
// then no longer a patch root, the read applied nothing at that path, and the
// subtree it wrote was missing from every read while the stepped head still had
// it -- the store and its readers disagreeing about what was written
// (3cdjz00jh12krns4g1n0).
func TagPatchRoots(patches []*PatcherData) {
	for _, pd := range patches {
		if node := ir.Uncomment(pd.API.Data); node != nil {
			node.Tag = ir.TagCompose(PatchRootTag, nil, node.Tag)
		}
	}
}

// HasPatchRootTag checks if a node has the PatchRootTag, looking through a
// comment wrapper for the value that carries it.
func HasPatchRootTag(node *ir.Node) bool {
	node = ir.Uncomment(node)
	if node == nil {
		return false
	}
	return ir.TagHas(node.Tag, PatchRootTag)
}

// StripPatchRootTag removes PatchRootTag from a node's tag, on the value inside a
// comment wrapper where that is where it sits.
func StripPatchRootTag(node *ir.Node) {
	node = ir.Uncomment(node)
	if node == nil || node.Tag == "" {
		return
	}
	node.Tag = ir.TagRemove(node.Tag, PatchRootTag)
}

// StripPatchRootTagRecursive removes PatchRootTag from a node and all descendants.
func StripPatchRootTagRecursive(node *ir.Node) {
	if node == nil {
		return
	}
	StripPatchRootTag(node)
	for _, v := range node.Values {
		StripPatchRootTagRecursive(v)
	}
}
