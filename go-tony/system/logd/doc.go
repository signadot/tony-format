// Package logd provides a diff-based virtual document store.
//
// logd implements transactional storage with:
//
//   - Match (read) and Patch (write) operations on paths
//   - Multi-participant transactions for atomic updates
//   - Watch with real-time change notifications and replay
//   - Copy-on-write scopes for isolated views
//
// A store holds one document. Every commit -- one patch, or a transaction of several
// participants -- takes the next number in one commit sequence, and every read and watch
// says the commit it answers at. A path is a kpath (.field, [i], {i}) with no leading
// slash, and "" is the root. A schema, optional, declares which arrays are keyed, so
// their elements are addressed and merged by identity rather than by position.
//
// # Server
//
// Start the server with:
//
//	o system logd serve -data /path/to/data
//
// This starts the TCP session listener on localhost:9123 and an admin listener (read and
// write statistics, pprof) on localhost:9223, the defaults; -addr and -admin-addr move
// them, and -config names a configuration file (server.Config).
//
// # Session Protocol
//
// The TCP session protocol provides:
//
//   - Request pipelining with optional async IDs
//   - Watch/Unwatch for path-based change notifications
//   - Replay from historical commits
//   - Streaming events for real-time updates
//
// docd serves the same protocol to its clients, so a client moves between logd and docd
// by changing only the address.
//
// Connect with: o system logd session localhost:9123
//
// # Related Packages
//
//   - [github.com/signadot/tony-format/go-tony/system/logd/api] - Request/response types
//   - [github.com/signadot/tony-format/go-tony/system/logd/server] - TCP session server
//   - [github.com/signadot/tony-format/go-tony/system/logd/storage] - Storage layer
package logd
