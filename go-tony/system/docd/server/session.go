package server

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/docd/api"
)

// MountSession represents a controller mount connection to docd.
type MountSession struct {
	ID     string
	conn   io.ReadWriteCloser
	server *Server
	log    *slog.Logger

	// Mount state (set after successful handshake)
	controllerID string
	mountPath    string
	schema       *ir.Node

	done      chan struct{}
	closeOnce sync.Once
}

// MountSessionConfig contains configuration for creating a session.
type MountSessionConfig struct {
	Log    *slog.Logger
	Server *Server
}

// NewMountSession creates a new session for the given connection.
func NewMountSession(id string, conn io.ReadWriteCloser, cfg *MountSessionConfig) *MountSession {
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	return &MountSession{
		ID:     id,
		conn:   conn,
		server: cfg.Server,
		log:    log.With("session", id),
		done:   make(chan struct{}),
	}
}

// Run starts the session and blocks until it completes.
func (s *MountSession) Run() error {
	defer s.cleanup()

	// Create decoder for reading Tony documents
	decoder, err := stream.NewDecoder(s.conn, stream.WithBrackets())
	if err != nil {
		return fmt.Errorf("failed to create decoder: %w", err)
	}

	// Perform mount handshake
	if err := s.handleHandshake(decoder); err != nil {
		return err
	}

	s.log.Info("controller mounted", "controller", s.controllerID, "path", s.mountPath)

	// After handshake, wait for operations or connection close
	// TODO: Handle PATCH operations from docd to controller
	// For now, we just wait for the connection to close by reading
	for {
		select {
		case <-s.done:
			return nil
		default:
		}

		// Try to read - this will return error/EOF when connection closes
		_, err := s.readDocument(decoder)
		if err != nil {
			if err == io.EOF {
				return nil // Clean disconnect
			}
			// Check if we're shutting down
			select {
			case <-s.done:
				return nil
			default:
			}
			return fmt.Errorf("read error: %w", err)
		}
		// TODO: Handle incoming messages from controller (e.g., PATCH responses)
	}
}

// handleHandshake reads the mount request and registers the controller.
func (s *MountSession) handleHandshake(decoder *stream.Decoder) error {
	// Read mount request
	node, err := s.readDocument(decoder)
	if err != nil {
		if err == io.EOF {
			return fmt.Errorf("connection closed before handshake")
		}
		return fmt.Errorf("failed to read mount request: %w", err)
	}

	// Parse request
	var req api.MountRequest
	if err := req.FromTonyIR(node); err != nil {
		s.sendError(api.ErrCodeInvalidMessage, fmt.Sprintf("failed to parse mount request: %v", err))
		return fmt.Errorf("invalid mount request: %w", err)
	}

	// Validate request
	if req.Hello == nil {
		s.sendError(api.ErrCodeInvalidMessage, "missing hello in mount request")
		return fmt.Errorf("missing hello")
	}
	if req.Mount == nil {
		s.sendError(api.ErrCodeInvalidMessage, "missing mount in mount request")
		return fmt.Errorf("missing mount")
	}
	if req.Hello.Controller == "" {
		s.sendError(api.ErrCodeInvalidMessage, "missing controller identifier")
		return fmt.Errorf("missing controller identifier")
	}
	if req.Mount.Path == "" {
		s.sendError(api.ErrCodeInvalidPath, "mount path is required")
		return fmt.Errorf("missing mount path")
	}
	if !strings.HasPrefix(req.Mount.Path, "/") {
		s.sendError(api.ErrCodeInvalidPath, "mount path must start with /")
		return fmt.Errorf("invalid mount path: must start with /")
	}

	// Register mount
	entry := &MountEntry{
		Path:       req.Mount.Path,
		Controller: req.Hello.Controller,
		Schema:     req.Mount.Schema,
		Session:    s,
	}

	if err := s.server.Mounts.Register(entry); err != nil {
		s.sendError(api.ErrCodePathAlreadyMounted, err.Error())
		return fmt.Errorf("mount registration failed: %w", err)
	}

	// Store mount state
	s.controllerID = req.Hello.Controller
	s.mountPath = req.Mount.Path
	s.schema = req.Mount.Schema

	// Send success response
	resp := api.NewMountResponse(s.ID, req.Mount.Path)
	if err := s.sendResponse(resp); err != nil {
		// Unregister on send failure
		s.server.Mounts.Unregister(req.Mount.Path)
		return fmt.Errorf("failed to send response: %w", err)
	}

	return nil
}

// readDocument reads events until we have a complete document.
func (s *MountSession) readDocument(decoder *stream.Decoder) (*ir.Node, error) {
	var events []stream.Event
	started := false

	for {
		event, err := decoder.ReadEvent()
		if err != nil {
			if err == io.EOF {
				if len(events) > 0 {
					return stream.EventsToNode(events)
				}
				return nil, io.EOF
			}
			return nil, err
		}

		events = append(events, *event)
		started = true

		if started && decoder.Depth() == 0 {
			return stream.EventsToNode(events)
		}
	}
}

// sendResponse sends a mount response.
func (s *MountSession) sendResponse(resp *api.MountResponse) error {
	data, err := resp.ToTony(gomap.EncodeWire(true))
	if err != nil {
		return fmt.Errorf("failed to encode response: %w", err)
	}
	if _, err := s.conn.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write response: %w", err)
	}
	return nil
}

// sendError sends an error response.
func (s *MountSession) sendError(code, message string) {
	resp := api.NewMountErrorResponse(code, message)
	if err := s.sendResponse(resp); err != nil {
		s.log.Error("failed to send error response", "error", err)
	}
}

// cleanup removes the mount registration and closes the connection.
func (s *MountSession) cleanup() {
	// Unregister mount if we had one
	if s.mountPath != "" {
		s.server.Mounts.Unregister(s.mountPath)
		s.log.Info("controller unmounted", "controller", s.controllerID, "path", s.mountPath)
	}
	s.conn.Close()
}

// Close signals the session to shut down.
func (s *MountSession) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
	})
	return s.conn.Close()
}
