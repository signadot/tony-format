// Package snap provides event-based snapshot storage.
//
// Snapshots store stream.Event sequences with a size-bound index mapping
// paths to byte offsets. This enables efficient path lookups without
// loading entire documents into memory.
//
// Format: [header: 12 bytes][events][index]
package snap
