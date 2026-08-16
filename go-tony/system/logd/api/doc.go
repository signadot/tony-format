// Package api provides types for the logd session protocol.
//
// # Core Types
//
//   - [PathData] - Path and data for match/patch operations
//   - [Patch] - Patch with optional match precondition
//   - [Schema] - Which arrays are keyed, and on what field
//
// # Session Protocol
//
// Request/response types for bidirectional TCP streaming:
//
//   - [SessionRequest] - Union of Hello, Match, Patch, NewTx, Watch, Unwatch, DeleteScope
//   - [SessionResponse] - Union of Result, Event, or Error
//   - [WatchEvent] - Streaming events with state or patch
//   - [SessionError] - Error with code and message
//
// The session protocol supports:
//   - Sync mode: No ID field, client blocks for response
//   - Async mode: With ID field, enables request pipelining
//   - Multi-participant transactions: NewTx + Patch with txId
//   - Watches: Real-time change notifications with replay
//   - Scopes: Copy-on-write isolation via Hello scope field
//
// # Keyed arrays
//
// [Schema] says which arrays are identified by a key rather than by position, in two
// forms: !logd-auto-id, where the server generates the value, and !logd-key, where the
// client supplies it. Both mean the same thing to a merge and to the index; they differ
// only in who produces the key.
//
// A declaration changes what a write MEANS, not just how it is recorded: logd injects
// !key(f) into a write to a declared-keyed array, so the write merges by identity instead
// of replacing by position. A write carrying its own disagreeing !key is refused, as is a
// schema declaring two identities for one array ([Schema.Validate]).
//
// # What may be stored
//
// [StorageContext] declares the operations a stored delta may use, and
// [ValidateForStorage] enforces it along with the index's own requirement that a key
// render as a path segment -- a scalar, unique among its siblings. The index is narrower
// here than a merge is: mergeop keys an array by any node at all, encoded, while the index
// needs something it can write into a path.
//
// Baseline and a scope are held to different rules, because their bases behave
// differently. A baseline delta replays against a base which never moves, so what it
// needs is that it APPLIES, which the commit path checks once before storing it
// ([DoesNotApplyError]). A scope's base moves as baseline advances, so an operation
// whose meaning depends on what was there can stop applying long after it was written:
// scoped writes are held to the vocabulary above, and baseline writes are not.
//
// [mergeop.FindUnsafe] is the third: an operation which calls out to the system is refused
// everywhere, and never applied, because a stored one runs again on every read.
package api
