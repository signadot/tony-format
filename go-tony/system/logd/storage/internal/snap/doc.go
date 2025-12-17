// Package snap provides event-based snapshot storage.
//
// Snapshots store stream.Event sequences with a size-bound index mapping
// kinded paths to byte offsets. This enables efficient path lookups without
// loading entire documents into memory.
//
// Format: [header: 12 bytes][events][index]
//
// The archive/ subpackage contains the deprecated IR-node-based implementation.
package snap
