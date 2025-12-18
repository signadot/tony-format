// Package patches provides streaming patch application for snapshots.
//
// This package implements the PatchApplier interface for applying patches
// to event streams without materializing full documents in memory.
//
// Current implementation (InMemoryApplier) is temporary and materializes
// the full document. Future implementation (StreamingApplier) will apply
// patches incrementally to streaming events, only materializing small
// subtrees at patch target paths.
//
// See docs/patch_design_reference.md for the full streaming design (Piece 2).
package patches
