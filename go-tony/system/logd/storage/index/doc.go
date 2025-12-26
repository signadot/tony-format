// Package index provides hierarchical path-based indexing for storage.
//
// The index mirrors document structure, enabling efficient lookups by
// kinded path and commit range.
//
// # Structure
//
// [Index] is hierarchical - each path segment has its own sub-index:
//   - Root index for top-level paths
//   - Child indices for nested paths
//   - [LogSegment] maps path + commit range → log file offset
//
// # LogSegment Semantics
//
//   - StartCommit == EndCommit: snapshot (full state at that commit)
//   - StartCommit != EndCommit: patch (diff from StartCommit to EndCommit)
//   - ScopeID nil: baseline data
//   - ScopeID non-nil: scope-specific overlay
package index
