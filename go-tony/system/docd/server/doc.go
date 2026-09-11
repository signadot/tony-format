// Package server implements docd, a document daemon that fronts a logd store and
// lets external controllers own subtrees of the document.
//
// # Two faces
//
// docd speaks two protocols:
//
//   - Client-facing: the logd session protocol verbatim (Hello, Match, Patch,
//     Watch/Unwatch, NewTx, DeleteScope, Schema), so a client uses docd exactly as it
//     uses logd directly: switching is a change of address. Served by ClientSession
//     over ClientTCPListener. docd answers Ping itself, with the highest commit it has
//     reported to any client — a lower bound on logd's head.
//   - Mount-facing: the MOUNT protocol (see package system/docd/api). A controller
//     dials in, handshakes, and registers a subtree — its mount path plus a schema.
//     Served by MountSession over TCPListener. After the handshake the connection
//     carries the logd session protocol with docd as the requester: a mounted
//     controller is a logd-session server for its subtree.
//
// # Routing
//
// Every client operation is routed by path, single-owner:
//
//   - A path at or under a mount goes to that mount's controller.
//   - A path outside every mount ("base") goes to docd's own logd link.
//   - The reserved .meta namespace is served by docd itself (meta.go): it lists the
//     mounts, the clocks, and each mount's schema contribution, and controllers may
//     not mount under it.
//   - A virtual clock (a mount connection whose hello carries a ClockSpec) is served
//     by docd itself at its path, read-only, while that connection stays open.
//
// docd is a thin, fail-fast proxy: it forwards and composes, it does not cache
// document state. It retries its dial to logd with backoff, but a client session
// whose logd connection drops ends; the client reconnects (libctl.LogdSession does
// so on its next call).
//
// # Tombstones
//
// When a controller disconnects, its MountEntry is kept as a tombstone (nil Session)
// rather than removed, so operations on that subtree fail with a clear "controller
// unavailable" error instead of silently falling through to logd — the content lived
// in the controller, not in logd. A remount clears the tombstone. A graceful unmount
// (MountRequest.Unmount) removes the entry outright.
//
// # Composition
//
// Reads and watches single-route by default, but a Match or Watch whose path is a
// strict ancestor of one or more mounts must be composed: docd fans the operation
// across the base owner and every mount below and merges the subtrees into one
// document (compose_read.go, watch.go). Every watch event is rooted at the watched
// path — the initial State and each delta after it — as logd's are: docd re-roots a
// mount's deltas at the composed path, and trims the composed path's own stream to
// what that path owns — the same partition that splits a write — so each commit is
// delivered once.
//
// A composed watch treats its mount membership as fixed for its lifetime. A
// mount/unmount that changes membership ends the watch — a terminal WatchEvent whose
// EndReason says which happened, "session_mounted" or "session_unmounted" — so the
// client re-watches against the new mount set knowing which way it moved.
// Event preservation is a logd guarantee that docd inherits: mounts share the commit
// sequence for their lifetime, so a composed watch honours FromCommit — docd resolves
// it to one commit, reads the composed state there, and flushes every sub-watch's
// replay from it in commit order. A membership change is a re-sync rather than a
// replay: the composition itself changed, so deltas from before it describe a
// different document. The terminal event carries the last delivered commit as a
// resume point (WatchEndedError.Commit in package system/libctl), exact across a
// composed path as on a single route — save one gap: a composed watch forwards live
// deltas from different mounts as they arrive rather than in commit order (issue
// hb44wv28h12ksarmcdn0).
//
// # Multi-mount transactions
//
// A client patch that spans several mounts (and possibly base) is statically
// decomposed (split.go) into a per-mount sub-patch for each owner plus base writes
// for the remainder, then applied as one multi-participant logd transaction: every
// participant commits through the one logd under the tx id docd allocates,
// all-or-nothing, so the write is one commit, and the client is answered with the
// participants' data joined back into the subtree it patched. docd pre-fetches
// transaction ids (package system/docd/txpool) so a spanning write, and a baseline
// client's NewTx, costs fewer round trips; a NewTx naming a timeout goes to logd,
// since a pooled id was created with logd's. Certain tags on a node above a mount
// boundary block static decomposition; such a patch is rejected rather than
// mis-split. A spanning patch that is itself a participant in a client's transaction
// is refused (invalid_tx): the client sends one patch per mount and counts them in
// NewTx.
//
// # Mount coordination
//
// Mount and unmount are writers; active watches are readers. mountCoord (mountcoord.go)
// serializes them on overlapping paths with writer priority — a pending mount blocks
// new overlapping watches, then waits forceAfter for the already-active ones to drain
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
