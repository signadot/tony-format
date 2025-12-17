// Package index provides hierarchical path-based indexing for storage.
//
// The index mirrors document structure, enabling efficient lookups by
// kinded path and commit range.
//
// # Structure
//
// Index is hierarchical - each path can have child indices:
//   - Root index for top-level paths
//   - Child indices for nested paths
//   - LogSegments track path → commit range → log offset
//
// # Usage
//
//	// Add a segment to the index
//	segment := &LogSegment{
//	    Path:   "users[0].name",
//	    Commit: 42,
//	    LogFile: logA,
//	    Offset: 1234,
//	}
//	index.Add(segment)
//
//	// Lookup segments for a path
//	segments := index.LookupRange(commit)
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/ir/kpath - Kinded paths
//   - github.com/signadot/tony-format/go-tony/system/logd/storage - Storage layer
package index
