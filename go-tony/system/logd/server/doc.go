// Package server provides the TCP session server for logd.
//
// # Components
//
//   - [Server] - Main server coordinating storage and listeners
//   - [TCPListener] - Accepts TCP connections, creates sessions
//   - [Session] - Handles a single client's request/response stream
//   - [WatchHub] - Manages watch subscriptions and broadcasts
//
// # How a session is served
//
// [Session.Run] reads requests from the connection and dispatches them in order, and a
// writer goroutine sends what they produce. Everything runs ON that loop except reads
// and a patch that joins a transaction. A client is one session, so a read of a large
// document would otherwise hold up every write behind it; and a joining patch waits for
// the other participants, which a client pipelines behind it, so holding the loop would
// make that wait unsatisfiable (zh3bm3msh12kscpygnn0). Other writes stay on the loop,
// which is what keeps a client's own ordering -- a read dispatched after a write is
// dispatched after that write committed, so read-your-writes holds with nothing
// tracked. A ping stays on the loop too, because its answer means the loop is alive,
// which is what a probe asks (7qayp3hah12kscx2gdn0).
//
// A watch is registered on the loop and then served from its own goroutine: its initial
// state, its replay, and the live stream. Its events reach it through [WatchHub], whose
// broadcast never blocks -- a watcher which cannot keep up is FAILED, not waited for,
// and told so with a terminal event carrying the highest commit it accounted for.
//
// # What a watch holds
//
// A watch holds the value at its path and nothing wider. A baseline watch steps that
// value by each commit's stored delta projected onto the path ([api.ProjectDelta]) and
// sends the projection; where an operator above the path states the whole value there,
// it reads the path and sends the difference. A scoped watch steps the same way by its
// own scope's commits and by a baseline commit that meets no statement of the scope,
// drops a baseline commit hidden under the scope's claim, and re-reads its path for the
// rest ([storage.Storage.BaselineDeltaInScope]). A commit that changes nothing at the
// path ([api.SameState]) is not sent.
//
// # What a read costs
//
// A read is answered from the subtree the path names: the writes that reach the path
// since the nearest snapshot, projected onto it. Where an operator above the path states
// the whole value there, the read is taken at that ancestor and navigated down; a scoped
// read folds the scope's live statements at the path after baseline's. A path the index
// shows was never written is answered from the index without reading anything. Which
// of those happened is counted, and reported on the admin listener along with what
// writes cost -- see [storage.Storage.StatsReport], and ask it before reasoning about
// why a store is slow (ap8ddvp2h12krd43gdn0).
//
// A match with no pattern, in a view whose schema keys no array, is encoded from the
// read's event stream straight into the response, and no node is built. Every other read
// builds its node, and both are held to the session's read budget
// ([SessionConfig.ReadBudget]).
//
// # Configuration
//
// Server can be configured via [Config] loaded from a tony file:
//
//	config, _ := LoadConfig("logd.tony")
//	server := New(&Spec{Config: config, Storage: store})
package server
