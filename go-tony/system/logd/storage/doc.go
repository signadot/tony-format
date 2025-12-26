// Package storage provides the persistence layer for logd.
//
// [Storage] manages:
//
//   - Patch storage in a double-buffered write-ahead log (dlog)
//   - Path-based indexing for efficient lookups
//   - Multi-participant transactions
//   - Copy-on-write scopes for isolation
//   - Snapshots for read optimization
//
// # Subpackages
//
//   - [index] - Hierarchical path-based indexing
//   - [tx] - Transaction coordination
//   - [autoid] - Monotonic ID generation
package storage
