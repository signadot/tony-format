// Package api provides types for the logd session protocol.
//
// # Core Types
//
//   - [PathData] - Path and data for match/patch operations
//   - [Patch] - Patch with optional match precondition
//   - [Schema] - Which arrays are keyed, and on what field
//
// # Session protocol
//
// A session is a bidirectional stream of newline-delimited Tony documents over one
// connection. Every message from a client is a [SessionRequest] naming exactly one
// operation; every message from a server is a [SessionResponse] carrying a result, a
// watch event, or an error.
//
//	{hello: {clientId: verse}}
//	{patch: {path: "verse.entities.e1", data: {status: ready}}}
//	{match: {path: "verse.entities.e1"}}
//	{watch: {path: "verse.entities"}}
//
// An id makes a request asynchronous: the response carries it back, so a client may
// pipeline. Without one the client is expected to wait for the answer. The id is also
// the routing key for watch events, so several watches on one path stay apart.
//
// # Where the path goes
//
// [MatchRequest], [PatchRequest] and [WatchRequest] all keep path directly under the
// operation, and the operation's own fields sit beside it -- commit for a match, txId
// and timeout for a patch, fromCommit and noInit for a watch. A request has no body:
// body is what a RESPONSE carries, and it is the answer ([MatchResult.Body]).
//
// This matters more than it reads. A field the protocol does not recognise is ignored,
// and an unread path defaults to "", which is the whole document for a read and the
// document ROOT for a write -- so a request in the wrong shape is answered rather than
// refused (k0d4y1m6h12kr7cdgdn0).
//
// # What each operation means
//
//   - hello ([Hello]) opens the session, names the client, and fixes the COW scope for
//     everything sent on it. The answer carries the server's schema.
//   - match ([MatchRequest]) reads. path restricts the read to that subdocument, data
//     is an optional pattern the state is matched and trimmed against WITHIN it, and
//     commit reads state as of a past commit. The answer is the state and the commit it
//     was read at.
//   - patch ([PatchRequest]) writes. match, when set, is a compare-and-swap
//     precondition; txId joins a multi-participant transaction; timeout bounds that
//     participant's wait. The answer is the commit and the data as stored, which is
//     where a client learns a server-generated id.
//   - newtx ([NewTxRequest]) opens a transaction of n participants. Every participant
//     patches with its txId, and the whole transaction commits or none of it does.
//   - watch ([WatchRequest]) streams. It sends the state at the path, then a
//     replayComplete marker, then a [WatchEvent] per commit that changes it. fromCommit
//     replays history first; noInit skips the initial state.
//   - unwatch ([UnwatchRequest]) ends one watch, by id where a client holds several.
//   - ping ([PingRequest]) is answered by whichever server owns the connection, so a
//     pong means that server's request loop is alive. [PongResult] carries the head
//     commit with it, which is how a client tracks the store's revision without
//     holding a watch open for it.
//   - schema, migration, deleteScope: see their own types.
//
// # What a watch promises
//
// The first event is the state; every event after it is the delta of one commit
// ([WatchEvent.Patch]), in commit order, with no gaps -- a consumer that applies them
// in order holds what the store holds. A watch that cannot keep that promise ENDS
// rather than skipping: [WatchEvent.Ended] with an [WatchEvent.EndReason] from the
// error vocabulary, carrying the last commit it delivered so the client can re-watch
// from there.
//
// # Errors
//
// A [SessionError] names a code from the vocabulary in errors.go, and the code is the
// part a client should branch on. The distinctions that matter most: not_found is a
// path with nothing at it, invalid_path is a path that cannot address anything,
// match_failed is a precondition that did not hold (the write did not happen), and
// replay_compacted is a fromCommit below retained history.
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
