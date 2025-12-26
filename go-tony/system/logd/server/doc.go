// Package server provides the TCP session server for logd.
//
// # Components
//
//   - [Server] - Main server coordinating storage and listeners
//   - [TCPListener] - Accepts TCP connections, creates sessions
//   - [Session] - Handles a single client's request/response stream
//   - [WatchHub] - Manages watch subscriptions and broadcasts
//
// # Configuration
//
// Server can be configured via [Config] loaded from a tony file:
//
//	config, _ := LoadConfig("logd.tony")
//	server := New(&Spec{Config: config, Storage: store})
package server
