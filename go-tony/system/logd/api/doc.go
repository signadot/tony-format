// Package api provides types for the logd session protocol.
//
// # Core Types
//
//   - [PathData] - Path and data for match/patch operations
//   - [Patch] - Patch with optional match precondition
//   - [Schema] - Keyed array definitions for indexing
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
package api
