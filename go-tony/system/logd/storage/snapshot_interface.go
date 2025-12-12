package storage

import (
	"io"

	"github.com/signadot/tony-format/go-tony/ir"
)

// SnapshotReader provides read access to a snapshot at a specific commit.
// The snapshot may be provided as an in-memory ir.Node or streamed from disk.
//
// This interface abstracts the implementation details, allowing:
// - Initial implementation: In-memory ir.Node snapshots
// - Future implementation: Streaming snapshots (without loading entire root)
//
// The interface remains unchanged between implementations.
type SnapshotReader interface {
	// Commit returns the commit number this snapshot represents.
	Commit() int64

	// ReadFull reads the entire snapshot as an in-memory ir.Node.
	// For streaming implementations, this will load the full snapshot into memory.
	// Use this when you need the complete snapshot in memory.
	ReadFull() (*ir.Node, error)

	// ReadPath reads a specific path from the snapshot.
	// For in-memory implementations, this extracts the path from the ir.Node.
	// For streaming implementations, this reads only the needed sub-tree.
	//
	// Returns the node at the given kinded path, or nil if not found.
	// Returns an error if the path is invalid or read fails.
	ReadPath(kindedPath string) (*ir.Node, error)

	// StreamTo writes the entire snapshot to the given io.Writer in Tony format.
	// For in-memory implementations, this encodes the ir.Node to bytes.
	// For streaming implementations, this streams directly from disk.
	//
	// This is useful for:
	// - Creating new snapshots from existing ones
	// - Streaming snapshots to clients
	// - Compaction operations
	StreamTo(w io.Writer) error

	// StreamPathTo writes a specific path from the snapshot to the given io.Writer.
	// For in-memory implementations, this extracts and encodes the path.
	// For streaming implementations, this streams only the needed sub-tree.
	//
	// This is useful for:
	// - Reading specific resources from large snapshots
	// - Streaming sub-documents without loading full snapshot
	StreamPathTo(kindedPath string, w io.Writer) error

	// Close releases any resources associated with this snapshot reader.
	// Must be called when done with the snapshot.
	Close() error
}

// PatchApplier applies patches to a snapshot to produce a new state.
// The patches may be applied in-memory or via streaming, depending on implementation.
//
// This interface abstracts the implementation details, allowing:
// - Initial implementation: In-memory ir.Node merge
// - Future implementation: Streaming patch application (without loading entire root)
//
// The implementation chooses whether to use in-memory or streaming internally.
// The caller does not need to know or choose - the interface handles it automatically.
//
// The interface remains unchanged between implementations.
type PatchApplier interface {
	// Apply applies a single patch to the current state.
	// The patch is a root-level diff that modifies the state.
	//
	// For in-memory implementations, this merges the patch into the current state.
	// For streaming implementations, this applies the patch incrementally.
	//
	// Returns an error if the patch is invalid or application fails.
	Apply(patch *ir.Node) error

	// ApplyAll applies multiple patches in sequence.
	// Equivalent to calling Apply() for each patch, but may be optimized.
	//
	// For in-memory implementations, this merges all patches into the state.
	// For streaming implementations, this applies patches incrementally.
	//
	// Returns an error if any patch is invalid or application fails.
	ApplyAll(patches []*ir.Node) error

	// Result returns a SnapshotReader for the final state after applying all patches.
	// The implementation chooses whether to use in-memory or streaming internally.
	//
	// For in-memory implementations, this returns a reader backed by the merged ir.Node.
	// For streaming implementations, this returns a reader that streams from disk.
	//
	// The caller can then use the SnapshotReader to:
	//   - Read the full state: reader.ReadFull()
	//   - Read a specific path: reader.ReadPath(kindedPath)
	//   - Stream the state: reader.StreamTo(w)
	//   - Stream a path: reader.StreamPathTo(kindedPath, w)
	//
	// The implementation handles memory management automatically - if the state is
	// too large for memory, it will stream; if it fits in memory, it may cache it.
	//
	// Returns an error if the state cannot be read or patches cannot be applied.
	Result() (SnapshotReader, error)

	// Close releases any resources associated with this patch applier.
	// Must be called when done, even if an error occurred.
	Close() error
}

// SnapshotProvider provides access to snapshots at specific commits.
// This is the main interface for fetching snapshots.
//
// This interface abstracts the implementation details, allowing:
// - Initial implementation: In-memory ir.Node snapshots
// - Future implementation: Streaming snapshots (without loading entire root)
//
// The interface remains unchanged between implementations.
type SnapshotProvider interface {
	// GetSnapshot returns a SnapshotReader for the snapshot at the given commit.
	// If no snapshot exists at that exact commit, returns the highest snapshot ≤ commit.
	//
	// Returns nil, nil if no snapshot exists at or before the commit.
	// Returns an error if the snapshot cannot be read.
	GetSnapshot(commit int64) (SnapshotReader, error)

	// GetSnapshotAtOrBefore returns a SnapshotReader for the highest snapshot ≤ commit.
	// This is useful when you need a base snapshot to apply patches after it.
	//
	// Returns nil, nil if no snapshot exists at or before the commit.
	// Returns an error if the snapshot cannot be read.
	GetSnapshotAtOrBefore(commit int64) (SnapshotReader, error)

	// GetLatestSnapshot returns a SnapshotReader for the most recent snapshot.
	// Returns nil, nil if no snapshots exist.
	// Returns an error if the snapshot cannot be read.
	GetLatestSnapshot() (SnapshotReader, error)
}

// PatchProvider provides access to patches between commits.
// This is used to fetch patches that need to be applied after a snapshot.
type PatchProvider interface {
	// GetPatches returns all patches between startCommit (exclusive) and endCommit (inclusive).
	// Patches are returned in commit order.
	//
	// For example:
	//   - GetPatches(10, 15) returns patches for commits 11, 12, 13, 14, 15
	//   - GetPatches(10, 10) returns empty slice (no patches)
	//
	// Returns an error if patches cannot be read.
	GetPatches(startCommit, endCommit int64) ([]*ir.Node, error)
}

// StateReader provides a unified interface for reading state at a specific commit.
// It combines SnapshotProvider and PatchProvider to provide a complete view.
//
// This is the main interface for ReadStateAt() implementations.
//
// The implementation chooses whether to use in-memory or streaming internally.
// The caller does not need to know or choose - the interface handles it automatically.
type StateReader interface {
	// ReadStateAt returns a SnapshotReader for the state at the given commit.
	// This combines snapshot fetching and patch application:
	//   1. Get snapshot at or before commit
	//   2. Get patches after snapshot up to commit
	//   3. Apply patches to snapshot
	//   4. Return SnapshotReader for final state
	//
	// The implementation chooses whether to use in-memory or streaming internally.
	// For in-memory implementations, this returns a reader backed by the merged ir.Node.
	// For streaming implementations, this returns a reader that streams from disk.
	//
	// The caller can then use the SnapshotReader to:
	//   - Read the full state: reader.ReadFull()
	//   - Read a specific path: reader.ReadPath(kindedPath)
	//   - Stream the state: reader.StreamTo(w)
	//   - Stream a path: reader.StreamPathTo(kindedPath, w)
	//
	// The implementation handles memory management automatically - if the state is
	// too large for memory, it will stream; if it fits in memory, it may cache it.
	//
	// Returns nil, nil if no state exists at or before the commit.
	// Returns an error if state cannot be read or patches cannot be applied.
	ReadStateAt(commit int64) (SnapshotReader, error)

	// ReadStateAtPath returns a SnapshotReader for a specific path from the state at the given commit.
	// This is optimized for reading sub-documents without loading the full state.
	//
	// The implementation chooses whether to use in-memory or streaming internally.
	// For in-memory implementations, this reads the full state and extracts the path.
	// For streaming implementations, this may read only the needed sub-tree if path
	// indexing is available, otherwise it may need to read more of the state.
	//
	// The caller can then use the SnapshotReader to access the path:
	//   - Read the path: reader.ReadFull() (returns just the path sub-tree)
	//   - Stream the path: reader.StreamTo(w)
	//
	// Returns nil, nil if the path doesn't exist.
	// Returns an error if state cannot be read or patches cannot be applied.
	ReadStateAtPath(kindedPath string, commit int64) (SnapshotReader, error)
}
