package server

import (
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"sync"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/docd/api"
	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
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

	// Routing state (post-handshake). docd forwards client ops to the controller
	// over this connection using the logd session protocol, assigning its own ids
	// so that ops from many client sessions can multiplex onto the one controller
	// connection. routes maps a docd-assigned id to the client waiting on it.
	routeMu sync.Mutex
	nextID  uint64
	routes  map[string]*routeEntry
	writeMu sync.Mutex // serializes writes to the controller connection

	done      chan struct{}
	closeOnce sync.Once
}

// routeEntry records the client a routed operation belongs to, so the
// controller's response (correlated by the docd-assigned id) can be delivered
// back with the client's original id restored.
type routeEntry struct {
	client   *ClientSession
	clientID *string // the client's original request id, restored on the way back
	path     string
	isWatch  bool // watches keep their route alive to forward streaming events

	// collect, when non-nil, marks a coordinator route: the controller's response
	// is delivered here (one-shot) instead of being forwarded to a client. Used by
	// docd-coordinated multi-mount transactions.
	collect chan *logdapi.SessionResponse
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
		routes: make(map[string]*routeEntry),
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

	// After handshake the controller speaks the logd session protocol for its
	// subtree: docd sends it match/patch/watch requests and it replies with
	// results, errors, and watch events. Pump those responses back to the
	// clients waiting on them until the connection closes.
	return s.readPump(decoder)
}

// readPump reads responses from the controller and dispatches each to the
// client that issued the correlated request. It runs until the connection
// closes or errors, failing any in-flight routes on the way out.
func (s *MountSession) readPump(decoder *stream.Decoder) error {
	for {
		node, err := s.readDocument(decoder)
		if err != nil {
			select {
			case <-s.done:
				s.failAllRoutes(fmt.Errorf("controller session closed"))
				return nil
			default:
			}
			if err == io.EOF {
				s.failAllRoutes(fmt.Errorf("controller disconnected"))
				return nil
			}
			s.failAllRoutes(err)
			return fmt.Errorf("read error: %w", err)
		}

		var resp logdapi.SessionResponse
		if err := resp.FromTonyIR(node); err != nil {
			s.log.Error("failed to parse controller response", "error", err)
			continue
		}
		s.dispatch(&resp)
	}
}

// dispatch delivers a single controller response to the appropriate client.
// Watch events (id-less) are handled separately; results and errors are
// correlated by the docd-assigned id.
func (s *MountSession) dispatch(resp *logdapi.SessionResponse) {
	if resp.Event != nil {
		s.forwardEvent(resp)
		return
	}
	if resp.ID == nil {
		s.log.Warn("dropping controller response with no id")
		return
	}

	s.routeMu.Lock()
	entry := s.routes[*resp.ID]
	if entry != nil {
		// A successful watch confirmation keeps its route alive so the streaming
		// events that follow can be forwarded; everything else is one-shot.
		keep := entry.isWatch && resp.Result != nil && resp.Result.Watch != nil
		if !keep {
			delete(s.routes, *resp.ID)
		}
	}
	s.routeMu.Unlock()

	if entry == nil {
		s.log.Warn("dropping controller response for unknown route", "id", *resp.ID)
		return
	}

	if entry.collect != nil {
		entry.collect <- resp // buffered (cap 1); coordinator route
		return
	}

	resp.ID = entry.clientID // restore the client's original id
	if err := entry.client.writeToClient(resp); err != nil {
		s.log.Debug("failed to forward controller response to client", "error", err)
	}
}

// RouteCollect forwards a request to the controller under a fresh docd-assigned
// id and returns a channel that receives the single response. Unlike
// RouteRequest, the response is collected (not forwarded to a client) — used by
// the multi-mount transaction coordinator.
func (s *MountSession) RouteCollect(req *logdapi.SessionRequest) <-chan *logdapi.SessionResponse {
	ch := make(chan *logdapi.SessionResponse, 1)

	s.routeMu.Lock()
	s.nextID++
	docdID := strconv.FormatUint(s.nextID, 10)
	s.routes[docdID] = &routeEntry{path: requestPath(req), collect: ch}
	s.routeMu.Unlock()

	out := *req
	out.ID = &docdID
	if err := s.writeToController(&out); err != nil {
		s.routeMu.Lock()
		delete(s.routes, docdID)
		s.routeMu.Unlock()
		ch <- logdapi.NewErrorResponse(nil, logdapi.ErrCodeSessionClosed,
			fmt.Sprintf("controller %q unavailable: %v", s.controllerID, err))
	}
	return ch
}

// forwardEvent forwards a streaming watch event to the client that owns the
// watch. The controller stamps events with the docd-assigned route id; docd
// strips it before delivery because clients route events by path, as with logd.
func (s *MountSession) forwardEvent(resp *logdapi.SessionResponse) {
	if resp.ID == nil {
		s.log.Warn("dropping controller watch event with no route id")
		return
	}
	s.routeMu.Lock()
	entry := s.routes[*resp.ID]
	s.routeMu.Unlock()
	if entry == nil {
		return // watch already torn down
	}
	resp.ID = nil // events are id-less to the client
	if err := entry.client.writeToClient(resp); err != nil {
		s.log.Debug("failed to forward watch event to client", "error", err)
	}
}

// RouteRequest forwards a client operation to the controller under a fresh
// docd-assigned id, recording the route so the response can be sent back to the
// originating client. An unwatch first tears down the matching watch route.
func (s *MountSession) RouteRequest(cs *ClientSession, req *logdapi.SessionRequest) {
	var unwatchTarget *string
	if req.Unwatch != nil {
		unwatchTarget = s.dropWatch(cs, req.Unwatch.Path)
	}

	s.routeMu.Lock()
	s.nextID++
	docdID := strconv.FormatUint(s.nextID, 10)
	s.routes[docdID] = &routeEntry{
		client:   cs,
		clientID: req.ID,
		path:     requestPath(req),
		isWatch:  req.Watch != nil,
	}
	s.routeMu.Unlock()

	cs.trackMount(s)

	// Copy so we can rewrite the id without mutating the client's request.
	out := *req
	out.ID = &docdID
	if req.Unwatch != nil {
		// Target the specific controller-side watch this unwatch cancels, since
		// several clients may watch the same path over this connection.
		out.Unwatch = &logdapi.UnwatchRequest{Path: req.Unwatch.Path, WatchID: unwatchTarget}
	}
	if err := s.writeToController(&out); err != nil {
		s.routeMu.Lock()
		delete(s.routes, docdID)
		s.routeMu.Unlock()
		_ = cs.writeToClient(logdapi.NewErrorResponse(req.ID, logdapi.ErrCodeSessionClosed,
			fmt.Sprintf("controller %q unavailable: %v", s.controllerID, err)))
	}
}

// writeToController encodes and writes a request to the controller connection,
// serialized so concurrent client routes do not interleave on the wire.
func (s *MountSession) writeToController(req *logdapi.SessionRequest) error {
	// Encode under the write lock (see ClientSession.writeToClient): ir encoding
	// mutates node linkage, so serializing it keeps concurrent routing goroutines
	// from racing on a shared node.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	data, err := req.ToTony(gomap.EncodeWire(true))
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}
	if _, err := s.conn.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write to controller: %w", err)
	}
	return nil
}

// dropWatch removes the watch route a client holds for a path, stopping event
// forwarding to it, and returns the docd-assigned id of that watch so the
// forwarded unwatch can target the exact controller-side watch. A client can
// hold at most one watch per path, so there is at most one to remove.
func (s *MountSession) dropWatch(cs *ClientSession, path string) *string {
	s.routeMu.Lock()
	defer s.routeMu.Unlock()
	for id, e := range s.routes {
		if e.client == cs && e.isWatch && e.path == path {
			delete(s.routes, id)
			idCopy := id
			return &idCopy
		}
	}
	return nil
}

// dropClient removes every route a departing client holds and best-effort tells
// the controller to stop the client's watches.
func (s *MountSession) dropClient(cs *ClientSession) {
	type target struct{ path, id string }

	s.routeMu.Lock()
	var unwatch []target
	for id, e := range s.routes {
		if e.client == cs {
			if e.isWatch {
				unwatch = append(unwatch, target{path: e.path, id: id})
			}
			delete(s.routes, id)
		}
	}
	s.routeMu.Unlock()

	for _, t := range unwatch {
		id := t.id
		_ = s.writeToController(&logdapi.SessionRequest{
			Unwatch: &logdapi.UnwatchRequest{Path: t.path, WatchID: &id},
		})
	}
}

// failAllRoutes drops every route and tells the waiting clients that in-flight
// (non-watch) operations will not complete. Called when the controller
// connection ends.
func (s *MountSession) failAllRoutes(err error) {
	s.routeMu.Lock()
	entries := s.routes
	s.routes = make(map[string]*routeEntry)
	s.routeMu.Unlock()

	for _, e := range entries {
		if e.collect != nil {
			e.collect <- logdapi.NewErrorResponse(nil, logdapi.ErrCodeSessionClosed,
				"controller disconnected: "+err.Error())
			continue
		}
		if e.isWatch {
			continue // the watch simply stops delivering events
		}
		_ = e.client.writeToClient(logdapi.NewErrorResponse(e.clientID,
			logdapi.ErrCodeSessionClosed, "controller disconnected: "+err.Error()))
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
	if fields, ferr := pathFields(req.Mount.Path); ferr != nil || len(fields) == 0 {
		s.sendError(api.ErrCodeInvalidPath, "mount path must be a non-empty kpath (no leading /)")
		return fmt.Errorf("invalid mount path %q", req.Mount.Path)
	}
	if isMetaPath(req.Mount.Path) {
		s.sendError(api.ErrCodeInvalidPath, "path .meta is reserved by docd")
		return fmt.Errorf("invalid mount path: .meta is reserved")
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

// cleanup tombstones the mount and closes the connection. The mount is
// tombstoned rather than removed so operations on its subtree fail with a clear
// error until a controller remounts, instead of silently falling through to
// logd (which does not hold the controller's content).
func (s *MountSession) cleanup() {
	if s.mountPath != "" {
		s.server.Mounts.TombstoneBySession(s.mountPath, s)
		s.log.Info("controller disconnected (mount tombstoned)", "controller", s.controllerID, "path", s.mountPath)
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
