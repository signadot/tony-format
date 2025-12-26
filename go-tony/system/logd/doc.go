// Package logd provides a diff-based virtual document store.
//
// logd implements transactional storage with:
//
//   - Match (read) and Patch (write) operations on paths
//   - Multi-participant transactions for atomic updates
//   - Watch with real-time change notifications and replay
//   - Copy-on-write scopes for isolated views
//
// # Server
//
// Start the server with:
//
//	o logd serve -data /path/to/data
//
// This starts the TCP session listener on localhost:9123 (default).
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
// Connect with: o logd session localhost:9123
//
// # Related Packages
//
//   - [api] - Request/response types
//   - [server] - TCP session server
//   - [storage] - Storage layer
package logd
