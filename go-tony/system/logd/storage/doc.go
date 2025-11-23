// Package storage provides a filesystem based storage layer for
// tony system api.
//
// Storage manages
//
//   - mappings of virtual document paths to and from the filesyste
//
//   - indexing of virtual document nodes
//
//   - storage of diffs associated with virtual document nodes
//
//   - transactions of multi-participant diffs
//
//   - compaction of diffs
package storage
