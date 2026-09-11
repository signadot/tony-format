// Package libdiff provides diff computation for Tony documents.
//
// A diff is a patch: an IR node whose tags -- !insert, !delete, !replace and the rest
// -- are merge operations, so applying a diff is patching with it. This package builds
// the parts of one: [MakeDiff] for a value inserted, deleted or replaced, [DiffObject],
// [DiffArrayByIndex], [DiffArrayByKey], [DiffString] and [DiffNumber] for two nodes of
// one kind, and [MakeTagDiff] for a change of tag. [Reverse] turns a diff from a to b
// into one from b to a. The container diffs recurse through a [DiffFunc]; tony.Diff is
// the one that chooses a diff by kind and passes itself.
//
// # Usage
//
//	// Compute a diff between two nodes; nil when they are equal
//	diff := tony.Diff(oldNode, newNode)
//
//	// Apply it: the result has newNode's content
//	patched, err := tony.Patch(oldNode, diff)
//
//	// Reverse it, to take newNode back to oldNode
//	back, err := libdiff.Reverse(diff)
//
// Diffs represent changes as IR nodes that can be stored, transmitted,
// and applied to reconstruct document states.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony - Diff, and Patch, which applies a diff
//   - github.com/signadot/tony-format/go-tony/ir - IR representation
//   - github.com/signadot/tony-format/go-tony/mergeop - Operations including diff ops
package libdiff
