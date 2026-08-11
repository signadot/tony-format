// Package storage provides the persistence layer for logd.
//
// [Storage] manages:
//
//   - Patch storage in a double-buffered write-ahead log (dlog)
//   - Path-based indexing for efficient lookups
//   - Multi-participant transactions
//   - Copy-on-write scopes for isolation, bounded by a per-scope overlay
//   - Snapshots for read optimization
//
// # Scopes and the overlay
//
// A scope is a copy-on-write overlay on baseline: reads see baseline with the scope's own
// writes applied last, and those writes shadow later baseline writes to the same path.
// Replaying every scope write on every read made a scoped read cost the scope's whole
// history, since scope patches are exempt from both snapshotting and compaction.
//
// Each baseline snapshot now also materializes each live scope's ownership as an overlay
// entry, so a scoped read is
//
//	baseline snapshot + baseline patches since + overlay + the scope's patches since
//
// which bounds it to one snapshot interval, exactly as baseline is bounded. The two hot
// paths -- a CAS precondition and a watch event -- compose their view on top of a stepped
// BASELINE document rather than re-reading; a scoped document cannot itself be stepped,
// because a scope's writes shadow stickily, and the overlay is what they re-assert from.
//
// See docs/scope_overlay_plan.md. A scope holding keyed arrays the schema does not declare
// falls back to replay, which is slower and correct.
//
// # Subpackages
//
//   - [index] - Hierarchical path-based indexing
//   - [tx] - Transaction coordination
//   - [autoid] - Monotonic ID generation
package storage
