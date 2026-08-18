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
// writer goroutine sends what they produce. Everything runs ON that loop except reads:
// a client is one session, so a read of a large document would otherwise hold up every
// write behind it. Writes stay on the loop, which is what keeps a client's own
// ordering -- a read dispatched after a write is dispatched after that write committed,
// so read-your-writes holds with nothing tracked. A ping stays on the loop too, because
// its answer means the loop is alive, which is what a probe asks (7qayp3hah12kscx2gdn0).
//
// A watch is registered on the loop and then served from its own goroutine: its initial
// state, its replay, and the live stream. Its events reach it through [WatchHub], whose
// broadcast never blocks -- a watcher which cannot keep up is FAILED, not waited for,
// and told so with a terminal event carrying the last commit it received.
//
// # What a read costs
//
// A read is answered from the subtree the path names where the store can do that, and
// from the whole document where it cannot (an operator above the path, a scoped read).
// A path with nothing at it is answered from the index without reading anything. Which
// of those happened is counted, and reported on the admin listener along with what
// writes cost -- see storage.StatsReport, and ask it before reasoning about why a store
// is slow (ap8ddvp2h12krd43gdn0).
//
// # Configuration
//
// Server can be configured via [Config] loaded from a tony file:
//
//	config, _ := LoadConfig("logd.tony")
//	server := New(&Spec{Config: config, Storage: store})
package server
