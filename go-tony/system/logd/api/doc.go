// Package api provides API types for the logd server.
//
// This package defines request/response structures for both protocols:
//
// # HTTP API
//
// Types for HTTP MATCH and PATCH operations:
//   - [Body] - Request body with path and data
//   - [Patch] - Patch request wrapper
//   - [Response] - HTTP response with commit and result
//
// # Session Protocol (TCP)
//
// Types for bidirectional TCP streaming:
//   - [SessionRequest] - Union of all request types (Hello, Match, Patch, Watch, Unwatch)
//   - [SessionResponse] - Union of Result, Event, or Error
//   - [WatchEvent] - Streaming watch events with state or patch
//   - [SessionError] - Error with code and message
//
// The session protocol supports:
//   - Sync mode: No ID field, client blocks for response
//   - Async mode: With ID field, enables request pipelining
//   - Watches: Real-time change notifications with replay
//
// See docs/tonyapi/session-protocol.md for the full protocol specification.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/system/logd/server - Server implementation
//   - github.com/signadot/tony-format/go-tony/system/logd/storage - Storage layer
package api
