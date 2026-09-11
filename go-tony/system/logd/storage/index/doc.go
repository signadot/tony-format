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
// A patch entry is indexed at every path on the way to what it writes ([IndexPatch],
// [EachSegment]): as Spine where it only passes through, and as a statement where it
// states something, with the statement's [Cover]. A snapshot is indexed only at the path
// it is of. [Index.Segments] answers the segments that can affect the subtree at a path,
// each entry once, without visiting anything below it, and [Index.SnapshotAtOrAbove] the
// snapshot a read there starts from.
//
// # LogSegment Semantics
//
//   - StartCommit == EndCommit: snapshot (full state of the subtree at its path, at that commit)
//   - StartCommit != EndCommit: patch (diff from StartCommit to EndCommit)
//   - ScopeID nil: baseline data
//   - ScopeID non-nil: one scope's own patch, which layers over baseline
//
// # Durable index and residency
//
// The log is the record, and the index is derived from it. [OpenIndex] opens the durable
// index -- an append-only regions file and a manifest naming its current records -- and
// [Build] indexes the log entries past the manifest's MaxCommit; when the files cannot be
// trusted, OpenIndex answers an empty index and Build indexes the whole log.
// [Index.Persist] writes what the file does not yet hold, and [Index.Rewrite] drops the
// records earlier persists superseded. A node holds its segments in regions, slices of
// its commit history; under a ceiling ([Residency]) the least recently used regions that
// are durable and clean are evicted and paged back in when a read needs them. Eviction
// changes a cost, never an answer.
//
// # Footprint
//
// The [Footprint] holds each scope's live statements: those no later statement of the
// scope dominates ([Cover]). It is decided as each entry is indexed and kept in the
// manifest. A scoped read folds only the live statements bearing on its path, compaction
// drops a scope entry none of whose statements is live, and [Index.DeleteScope] visits
// only the scope's paths.
package index
