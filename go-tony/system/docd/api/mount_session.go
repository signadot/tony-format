// Package api provides types for the docd session protocols.
//
// docd has two faces:
//   - Mount-facing: for controllers (MOUNT protocol)
//   - User-facing: for clients (queries, patches routed through controllers)
//
// This package defines the mount protocol for controller registration.
package api

import (
	"github.com/signadot/tony-format/go-tony/ir"
)

// --- Controller → docd Messages (Mount Protocol) ---

// MountHello is the hello portion of the mount handshake.
//
//tony:schemagen=mount-hello,notag
type MountHello struct {
	Controller string `tony:"field=controller"` // Controller identifier

	// Clock, when set, asks docd to drive a virtual system clock at Clock.Path
	// instead of (or in addition to) a controller-backed mount. It is docd-specific
	// — logd knows nothing of it.
	Clock *ClockSpec `tony:"field=clock,omitzero"`
}

// ClockSpec configures a docd-driven virtual clock served at Path. docd ticks it
// every Frequency and, at tick N (N whole Frequencies since the mount was
// established), serves the single int64 value Epoch + N*Frequency, with Frequency
// counted in nanoseconds — a monotonic, quantized clock. docd computes each value
// on demand; no tick history is retained. Epoch is also recorded under
// .meta/clocks so it can be recovered without replaying ticks.
//
//tony:schemagen=clock-spec,notag
type ClockSpec struct {
	Path      string `tony:"field=path"`      // Path to serve the clock at (e.g. "sys/clock")
	Frequency string `tony:"field=frequency"` // Tick interval as a Go duration, e.g. "1s"
	Epoch     int64  `tony:"field=epoch"`     // Value at tick 0
}

// MountSpec specifies the path and schema for a mount.
//
//tony:schemagen=mount-spec,notag
type MountSpec struct {
	Path   string   `tony:"field=path"`   // Path to mount (e.g., "users")
	Schema *ir.Node `tony:"field=schema"` // Schema for this path

	// ForceAfter bounds how long docd waits for overlapping watch readers to drain
	// before force-ending them so this mount can proceed (e.g. "5s"). "0" means
	// wait forever (never force); absent uses docd's configured default.
	ForceAfter *string `tony:"field=forceAfter,omitzero"`
}

// UnmountSpec requests a graceful unmount of the controller's subtree: docd
// drains (force-ending after ForceAfter) the watches overlapping the mount so
// they see session_unmounted rather than an abrupt controller_unavailable, then
// fully removes the mount (no tombstone) and closes the connection.
//
//tony:schemagen=unmount-spec,notag
type UnmountSpec struct {
	ForceAfter *string `tony:"field=forceAfter,omitzero"` // as MountSpec.ForceAfter
}

// MountRequest is a message from controller to docd. The handshake carries Hello
// and Mount; a later Unmount requests a graceful unmount on the same connection.
//
// A mount connection carries this handshake and nothing else. AFTER the mount is
// accepted the direction inverts: docd sends the CONTROLLER logd session requests
// (logd/api.SessionRequest) for every client operation at or under the mounted path,
// and the controller answers them -- so a controller is a server of that protocol
// rather than a client of it. Two things differ from a client connection: the id on a
// routed request is docd's, since many clients share one controller connection, and
// the client's COW scope rides the request (SessionRequest.Scope) rather than the
// connection.
//
//tony:schemagen=mount-request,notag
type MountRequest struct {
	Hello   *MountHello  `tony:"field=hello"`
	Mount   *MountSpec   `tony:"field=mount"`
	Unmount *UnmountSpec `tony:"field=unmount,omitzero"`
}

// --- docd → Controller Messages ---

// MountHelloResponse is the hello response from docd.
//
//tony:schemagen=mount-hello-response,notag
type MountHelloResponse struct {
	DocdID string `tony:"field=docdId"` // docd server identifier
}

// MountResult is the mount result from docd.
//
//tony:schemagen=mount-result,notag
type MountResult struct {
	Path     string `tony:"field=path"`     // The mounted path
	Accepted bool   `tony:"field=accepted"` // Whether the mount was accepted
}

// MountResponseResult contains the successful response fields.
//
//tony:schemagen=mount-response-result,notag
type MountResponseResult struct {
	Hello *MountHelloResponse `tony:"field=hello"`
	Mount *MountResult        `tony:"field=mount"`
}

// MountError is an error response.
//
//tony:schemagen=mount-error,notag
type MountError struct {
	Code    string `tony:"field=code"`
	Message string `tony:"field=message"`
}

// Error implements the error interface.
func (e *MountError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code != "" {
		return e.Code + ": " + e.Message
	}
	return e.Message
}

// MountResponse is the response message from docd to controller.
// Either Result or Error will be set, not both.
//
//tony:schemagen=mount-response,notag
type MountResponse struct {
	Result *MountResponseResult `tony:"field=result"`
	Error  *MountError          `tony:"field=error"`
}

// --- Error codes ---

const (
	ErrCodeMountFailed        = "mount_failed"
	ErrCodePathAlreadyMounted = "path_already_mounted"
	ErrCodeInvalidPath        = "invalid_path"
	ErrCodeInvalidMessage     = "invalid_message"
)

// --- Helper constructors ---

// NewMountResponse creates a successful mount response.
func NewMountResponse(docdID, path string) *MountResponse {
	return &MountResponse{
		Result: &MountResponseResult{
			Hello: &MountHelloResponse{
				DocdID: docdID,
			},
			Mount: &MountResult{
				Path:     path,
				Accepted: true,
			},
		},
	}
}

// NewMountErrorResponse creates an error response.
func NewMountErrorResponse(code, message string) *MountResponse {
	return &MountResponse{
		Error: &MountError{
			Code:    code,
			Message: message,
		},
	}
}
