package snap

import (
	"github.com/signadot/tony-format/go-tony/ir"
)

// rangeDesc represents a single range descriptor [from, to, offset, size]
type rangeDesc [4]int64

const (
	descFrom = iota
	descTo
	descOffset
	descSize
)

// patchCat represents how a patch is categorized relative to a chunked container
type patchCat struct {
	// patch is the original patch node
	patch *ir.Node
	
	// replacement is true if this patch replaces the entire container
	replacement bool
	
	// rangeIdx is the index of the range descriptor this patch affects, or -1 if insertion
	rangeIdx int
	
	// insertPos is the child index where this patch should be inserted in final order, or -1 if not an insertion
	insertPos int64
}

// categorize categorizes patches relative to a chunked container.
// 
// containerIndex is the !snap-chunks container index node.
// patches are root-level patches that may affect this container or its descendants.
//
// Returns patches categorized as:
// - Replacements: patches that replace the entire container
// - Range patches: patches that affect specific range descriptors
// - Insertions: patches that add children not in any existing range
//
// Patches are returned in the same order as input (respecting external order).
func categorize(containerIndex *ir.Node, patches []*ir.Node) ([]patchCat, error) {
	// TODO: Implement
	// 1. Extract kpath from each patch
	// 2. Determine if patch targets container itself (replacement)
	// 3. Parse range descriptors from !snap-range children
	// 4. For each patch, find which range descriptor contains its target index
	// 5. If not in any range, mark as insertion with position (w.r.t. final order)
	return nil, nil
}

// project projects a patch to be relative to a specific range descriptor.
//
// patch is the original patch node (root-level).
// desc is the range descriptor [from, to, offset, size] this patch affects.
// containerType is the type of the container (ObjectType, ArrayType, or SparseArrayType).
// containerKPath is the kpath to the container (e.g., "users" for patch "users.123.name").
//
// Returns the projected patch node that can be applied to the range's loaded data.
// For example, if patch is { "users": { "123": { "name": "Alice" } } } and
// desc is [100, 200, offset, size], returns { "123": { "name": "Alice" } }
// (removes "users" prefix, adjusts indices if needed).
func project(patch *ir.Node, desc rangeDesc, containerType ir.Type, containerKPath string) (*ir.Node, error) {
	// TODO: Implement
	// 1. Extract sub-node from patch tree corresponding to containerKPath
	// 2. If containerType is ArrayType, adjust array indices: index - desc[from]
	// 3. Return projected patch node
	return nil, nil
}

// Helper functions

// patchKPaths extracts all kpaths that a patch affects from its tree structure.
// 
// A patch can affect multiple paths. For example:
// - { "users": { "123": { "name": "Alice" } } } → ["users.123.name"]
// - { "users": { "123": {...}, "456": {...} } } → ["users.123", "users.456"]
//
// Returns all kpaths that the patch affects (paths to nodes being modified).
// Returns empty slice if patch is a replacement (targets root/entire container).
func patchKPaths(patch *ir.Node) ([]string, error) {
	// TODO: Implement
	// Traverse patch tree structure to find all paths being modified
	// For objects: use field names
	// For arrays: use indices
	// Collect all paths to modified nodes
	return nil, nil
}

// findRange finds which range descriptor contains the given child index.
// Returns the index of the descriptor in the array, or -1 if not found.
func findRange(rangeNode *ir.Node, childIndex int64) (int, rangeDesc, error) {
	// TODO: Implement
	return -1, rangeDesc{}, nil
}

// parseRanges parses a !snap-range node's array body into rangeDesc slice.
func parseRanges(rangeNode *ir.Node) ([]rangeDesc, error) {
	// TODO: Implement
	return nil, nil
}

// extractSub extracts the sub-node from a patch tree corresponding to a given kpath prefix.
// Used for projection - removes the prefix from the patch structure.
func extractSub(patch *ir.Node, prefixKPath string) (*ir.Node, error) {
	// TODO: Implement
	return nil, nil
}
