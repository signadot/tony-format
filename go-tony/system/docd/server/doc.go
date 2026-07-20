// Package server implements docd, a document daemon that fronts a logd store and
// lets external controllers own subtrees of the document.
//
// # Two faces
//
// docd speaks two protocols:
//
//   - Client-facing: the logd session protocol verbatim (Hello, Match, Patch,
//     Watch/Unwatch, NewTx, DeleteScope, Schema). A client cannot tell whether it is
//     talking to docd or to logd directly. Served by ClientSession over
//     ClientTCPListener.
//   - Mount-facing: the MOUNT protocol (see package system/docd/api). A controller
//     dials in, handshakes, and registers a subtree — its mount path plus a schema.
//     Served by MountSession over TCPListener.
//
// # Routing
//
// Every client operation is routed by path, single-owner:
//
//   - A path at or under a mount goes to that mount's controller.
//   - A path outside every mount ("base") goes to docd's own logd link.
//   - The reserved .meta namespace is served by docd itself (meta.go): it lists the
//     mounts and the composed schema, and controllers may not mount under it.
//
// docd is a thin, fail-fast proxy: it forwards and composes, it does not cache
// document state.
//
// # Tombstones
//
// When a controller disconnects, its MountEntry is kept as a tombstone (nil Session)
// rather than removed, so operations on that subtree fail with a clear "controller
// unavailable" error instead of silently falling through to logd — the content lived
// in the controller, not in logd. A remount clears the tombstone.
//
// # Composition
//
// Reads and watches single-route by default, but a Match or Watch whose path is a
// strict ancestor of one or more mounts must be composed: docd fans the operation
// across the base owner and every mount below and merges the subtrees into one
// document (compose_read.go, watch.go). Watch deltas are root-rooted (absolute); the
// initial State event is relative to the watched path.
//
// A composed watch treats its mount membership as fixed for its lifetime. A
// mount/unmount that changes membership ends the watch (a terminal WatchEvent with
// EndReason "membership_changed") so the client re-watches against the new mount set.
// Event preservation is a logd guarantee (single commit sequence) that docd inherits
// for single-route watches; across mount boundaries it is best-effort — a re-watch
// re-inits with a fresh composed snapshot rather than replaying the gap. The terminal
// event carries the last delivered commit as a resume point (WatchEndedError.Commit
// in package system/libctl), exact for single-route watches.
//
// # Multi-mount transactions
//
// A client patch that spans several mounts (and possibly base) is statically
// decomposed (split.go) into a per-mount sub-patch for each owner plus base writes
// for the remainder, then applied as one multi-participant logd transaction. docd
// pre-fetches transaction ids (package system/docd/txpool) so a spanning write costs
// fewer round trips. Certain tags on a node above a mount boundary block static
// decomposition; such a patch is rejected rather than mis-split.
//
// # Mount coordination
//
// Mount and unmount are writers; active watches are readers. mountCoord (mountcoord.go)
// serializes them on overlapping paths with writer priority — a pending mount blocks
// new overlapping watches, then waits force_after for the already-active ones to drain
// before force-ending the stragglers. This is what lets a composed watch assume fixed
// membership for its lifetime.
//
// # Scopes
//
// A client's copy-on-write scope (from Hello) is threaded through every operation:
// base operations carry it to logd, and controller operations carry it in the request
// so a single multiplexed mount connection serves every scope.
//
// A fuller, prose treatment lives in the site docs under docs/docd/.
package server
