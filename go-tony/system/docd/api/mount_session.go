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
}

// MountSpec specifies the path and schema for a mount.
//
//tony:schemagen=mount-spec,notag
type MountSpec struct {
	Path   string   `tony:"field=path"`   // Path to mount (e.g., "/users")
	Schema *ir.Node `tony:"field=schema"` // Schema for this path
}

// MountRequest is the request message from controller to docd.
// Contains both hello and mount in a single message.
//
//tony:schemagen=mount-request,notag
type MountRequest struct {
	Hello *MountHello `tony:"field=hello"`
	Mount *MountSpec  `tony:"field=mount"`
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
	ErrCodeMountFailed      = "mount_failed"
	ErrCodePathAlreadyMounted = "path_already_mounted"
	ErrCodeInvalidPath      = "invalid_path"
	ErrCodeInvalidMessage   = "invalid_message"
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
