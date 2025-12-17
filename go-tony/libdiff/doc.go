// Package libdiff provides diff computation for Tony documents.
//
// # Usage
//
//	// Compute diff between two nodes
//	diff, err := libdiff.Diff(oldNode, newNode)
//
//	// Apply a diff
//	patched, err := libdiff.Apply(original, diff)
//
// Diffs represent changes as IR nodes that can be stored, transmitted,
// and applied to reconstruct document states.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/ir - IR representation
//   - github.com/signadot/tony-format/go-tony/mergeop - Operations including diff ops
package libdiff
