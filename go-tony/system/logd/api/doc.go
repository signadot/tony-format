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
// watch event, or an error. logd serves the protocol, and docd serves it verbatim to its
// own clients, so a client moves between the two by changing only the address.
//
//	{hello: {clientId: verse, protocol: 2}}
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
// operation, and the operation's own fields sit beside it -- commit for a match; match
// (the precondition), txId and timeout for a patch; fromCommit, noInit and waitIfAbsent
// for a watch. A request has no body: body is what a RESPONSE carries, and it is the
// answer ([MatchResult.Body]).
//
// This matters more than it reads. A field the protocol does not recognise is ignored,
// and an unread path defaults to "", which is the whole document for a read and the
// document ROOT for a write -- so a request in the wrong shape is answered rather than
// refused (k0d4y1m6h12kr7cdgdn0).
//
// # What each operation means
//
//   - hello ([Hello]) opens the session, names the client and the protocol version it
//     speaks ([ProtocolVersion]), and fixes the COW scope for everything sent on it. A
//     server speaking another version refuses the hello with protocol_mismatch. The
//     answer carries the server's version and schema.
//   - match ([MatchRequest]) reads. path restricts the read to that subdocument, data
//     is an optional pattern the state is matched and trimmed against WITHIN it, and
//     commit reads state as of a past commit. The answer is the state and the commit it
//     was read at.
//   - patch ([PatchRequest]) writes. match, when set, is a compare-and-swap
//     precondition; txId joins a multi-participant transaction; timeout bounds that
//     participant's wait. The answer is the commit and the data as committed -- a
//     keyed array as an array -- which is where a client learns a server-generated id.
//   - newtx ([NewTxRequest]) opens a transaction of n participants. Every participant
//     patches with its txId, and the whole transaction commits or none of it does.
//   - watch ([WatchRequest]) streams. The answer confirms the watch ([WatchResult]); then
//     come the state at the path and a [WatchEvent] per commit that changes it. With
//     fromCommit the state is as of that commit, the commits since it are replayed, and a
//     replayComplete marker follows them; a negative fromCommit asks for the last N
//     commits. noInit skips the initial state. A path holding nothing is refused with
//     not_found unless waitIfAbsent is set.
//   - unwatch ([UnwatchRequest]) ends every watch on the path, or the one watchId names.
//   - ping ([PingRequest]) is answered by whichever server owns the connection, so a
//     pong means that server's request loop is alive. [PongResult] carries the head
//     commit with it, which is how a client tracks the store's revision without
//     holding a watch open for it, and the replay floor a watch may start from.
//   - schema ([SchemaRequest]) reads the store's schema, or sets it as one commit.
//   - deleteScope: see its type.
//
// # What a watch promises
//
// The first event is the state, unless noInit skipped it; every event after it is the
// delta of one commit ([WatchEvent.Patch]), in commit order, with no gaps -- a consumer
// that applies them in order holds what the store holds. A watch that cannot keep that
// promise ENDS rather than skipping: [WatchEvent.Ended] with an [WatchEvent.EndReason]
// from the error vocabulary and [WatchEvent.EndMessage] with the detail, carrying the
// highest commit the watch accounted for so the client can re-watch from there.
//
// A delta is rooted at the watched path, as the state is, and it is a delta of the value
// there: a projection of the stored commit, or the difference of two reads, not
// necessarily the patch the writer sent. A consumer applies it; it does not read meaning
// into its shape. A null in a state or a delta is a null the path holds, and
// [WatchEvent.Absent] says the path holds nothing after the event.
//
// # Errors
//
// A [SessionError] names a code from the ErrCode* vocabulary, and the code is the part a
// client should branch on. The distinctions that matter most: not_found is a path with
// nothing at it, path_conflict is a path that disagrees with the shape of what is there,
// invalid_path is a path that cannot address anything, invalid_diff is a delta that
// would not apply or that the schema's keying refuses, match_failed is a precondition
// that did not hold (the write did not happen), schema_refused is a schema the store will
// not adopt, and replay_compacted, the reason a watch ends rather than an error
// response, is a fromCommit below retained history.
//
// # Keyed arrays
//
// [Schema] says which arrays are identified by a key rather than by position, in two
// forms: !logd-auto-id, where the server generates the value, and !logd-key, where the
// client supplies it. Both mean the same thing to a merge and to the index; they differ
// only in who produces the key.
//
// A declaration changes what a write MEANS, not just how it is recorded: logd stores a
// declared-keyed array as an object whose fields are its elements' names, so a write to
// it merges by identity instead of replacing by position. A write carrying its own
// disagreeing !key is refused, as is a !key on an array the schema does not key, and a
// schema declaring two identities for one array ([Schema.Validate]).
//
// An element is addressed by its name: items."(sku=A)" is the name as the store keeps it,
// and items(sku=A) and items(A) name the same element and are canonicalized to it where
// a request arrives. An index into a keyed array names nothing. A read answers a keyed
// array as an array, and a watch delta carries !key(f) on it where the identity is one
// field, so a client's merge identifies elements the way the store does.
//
// # What may be stored
//
// [StorageContext] declares the operations a stored delta may use: the absolute ones,
// whose result states what a value IS. A write built only from those is stored as it
// arrived. A write that uses any other ([NeedsLowering]) is applied, and what is stored
// is its result in that vocabulary, which [ValidateForStorage] enforces along with the
// index's own requirement that a key render as a path segment -- a scalar, unique among
// its siblings. The index is narrower here than a merge is: mergeop keys an array by any
// node at all, encoded, while the index needs something it can write into a path.
//
// Baseline and a scope store different results, because their bases behave differently.
// A baseline delta replays against a base which never moves, so baseline stores the
// difference the write made. A scope's base moves as baseline advances, so a scope
// stores its claim: what it holds at the path, whatever baseline does afterwards. Either
// way the write must APPLY to the state it is written against, which the commit path
// checks before storing it ([DoesNotApplyError]).
//
// [mergeop.FindUnsafe] is the third: an operation which calls out to the system is refused
// everywhere, and never applied, because a stored one runs again on every read.
package api
