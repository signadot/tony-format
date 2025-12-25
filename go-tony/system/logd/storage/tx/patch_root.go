package tx

import "github.com/signadot/tony-format/go-tony/ir"

// PatchRootTag marks the root of an API patch in the merged IR tree.
// Used by StreamingProcessor to identify which subtrees need patching.
const PatchRootTag = "!logd-patch-root"

// TagPatchRoots adds PatchRootTag to each patch data's root node.
// Called before MergePatches to mark where patches originate.
func TagPatchRoots(patches []*PatcherData) {
	for _, pd := range patches {
		if pd.API.Patch.Data != nil {
			pd.API.Patch.Data.Tag = ir.TagCompose(PatchRootTag, nil, pd.API.Patch.Data.Tag)
		}
	}
}

// HasPatchRootTag checks if a node has the PatchRootTag.
func HasPatchRootTag(node *ir.Node) bool {
	if node == nil {
		return false
	}
	return ir.TagHas(node.Tag, PatchRootTag)
}

// StripPatchRootTag removes PatchRootTag from a node's tag.
func StripPatchRootTag(node *ir.Node) {
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
