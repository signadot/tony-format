// Package patches provides streaming patch application for snapshots.
//
// This package implements the PatchApplier interface for applying patches
// to event streams without materializing full documents in memory.
//
// StreamingProcessor streams the base events and materializes only the subtrees a patch
// reaches. An earlier InMemoryApplier, which read the whole document into memory first, is
// gone: nothing had called it since the streaming processor landed, and its "TODO: replace
// with a streaming implementation" outlived the replacement, which is worse than no note at
// all -- it was cited as the state of the code a year later.
//
// Where a whole document is still folded is the COMMIT path and the WATCH path, not here;
// see rkb7p8v5h12ksdnmgsn0. Future implementation (StreamingApplier) will apply
// patches incrementally to streaming events, only materializing small
// subtrees at patch target paths.
//
// See docs/patch_design_reference.md for the full streaming design (Piece 2).
package patches
