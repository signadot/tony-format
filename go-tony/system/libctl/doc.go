// Package libctl provides support code for building controllers that mount to docd.
//
// This package is built bottom-up: as patterns emerge from implementing and
// testing controllers, useful abstractions are collected here. The goal is to
// provide a foundation for controller development without over-engineering
// ahead of actual needs.
//
// Typical controller lifecycle:
//
//  1. Connect to docd via TCP
//  2. Send mount request (hello + mount with path and schema)
//  3. Receive mount confirmation
//  4. Handle PATCH operations routed from docd
//  5. Write results to logd
//  6. Clean up on shutdown
package libctl
