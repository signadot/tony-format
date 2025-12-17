// Package dlog provides double-buffered write-ahead logging.
//
// Two alternating log files (logA/logB) enable atomic switching during
// snapshot creation. Patches are appended to the active log.
//
// # Related Packages
//
//   - github.com/signadot/tony-format/go-tony/system/logd/storage - Storage layer
package dlog
