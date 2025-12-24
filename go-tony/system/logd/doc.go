// Package logd provides the backend storage for the tony system api.
//
// Package logd implements a diff-based virtual document store with
// transactional support sufficient for the system API. It supports:
//
//   - HTTP API: MATCH (read), PATCH (write) operations
//   - TCP Session Protocol: Bidirectional streaming with watch support
//   - Watch: Real-time change notifications with replay support
//
// # Server
//
// The server can be started with:
//
//	o logd serve -data /path/to/data
//
// This starts the TCP session listener on localhost:9123 (default).
// To also enable HTTP:
//
//	o logd serve -data /path/to/data -http 9000
//
// # Session Protocol
//
// The TCP session protocol provides:
//
//   - Request pipelining with optional async IDs
//   - Watch/Unwatch for path-based change notifications
//   - Replay from historical commits with deduplication
//   - Streaming events for real-time updates
//
// Connect with: o logd session localhost:9123
//
// See docs/tonyapi/session-protocol.md for the full protocol specification.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/system/logd/api - API types
//   - github.com/signadot/tony-format/go-tony/system/logd/server - Server implementation
//   - github.com/signadot/tony-format/go-tony/system/logd/storage - Storage layer
package logd
