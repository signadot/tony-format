// Package snap provides event-based snapshot storage for logd.
//
// This package is being redesigned to store events directly from the stream package,
// with a size-bound index into those events. This allows efficient storage and
// retrieval of large snapshots without loading entire documents into memory.
//
// Design Principles:
//   - Store events directly (from stream.Event) rather than IR nodes
//   - Maintain a size-bound index for efficient path lookups
//   - Support streaming reads without full document reconstruction
//
// The previous IR-node-based implementation has been archived in internal/snap/archive/.
package snap
