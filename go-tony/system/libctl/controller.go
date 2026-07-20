package libctl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// ErrUnsupported is returned by a Handler method to decline an operation the
// controller does not implement. docd relays it to the client as an
// "unsupported" error. Declining is a per-controller choice, not a library gap —
// e.g. a read-only content controller returns ErrUnsupported from Watch.
var ErrUnsupported = errors.New("unsupported operation")

// Handler implements a controller's behavior for its mounted subtree. After a
// controller mounts, docd forwards client operations for that subtree to it as
// logd-protocol requests; the runtime dispatches them to these methods. Any
// method may return ErrUnsupported to decline.
//
// A controller is free to back its subtree with anything — a logd session
// (obtained via the mount), the local filesystem, or computed state. It is the
// owner and answers for the subtree; content need not live in logd.
type Handler interface {
	// Match returns the data at path. pattern, when non-nil, is the match/trim
	// pattern the client supplied (field selection and filtering).
	Match(ctx context.Context, path string, pattern *ir.Node) (*ir.Node, error)

	// Patch applies data at path and returns the resulting data. When
	// opts.TxID is set, the client is coordinating a multi-participant
	// transaction: the controller joins it by writing to logd with that tx id
	// (the write is the join).
	Patch(ctx context.Context, path string, data *ir.Node, opts PatchParams) (*ir.Node, error)

	// Watch streams events for path until ctx is cancelled (the client
	// unwatched or disconnected) or it returns. emit delivers each event. To
	// decline watching, return ErrUnsupported before emitting.
	Watch(ctx context.Context, path string, opts WatchParams, emit func(*api.WatchEvent) error) error
}

// WatchParams carries the client's watch options through to the Handler.
type WatchParams struct {
	FromCommit *int64
	NoInit     bool
}

// PatchParams carries a patch's options through to the Handler. TxID, when set,
// is the multi-participant transaction the patch must join. Match, when set, is a
// compare-and-swap precondition the controller must carry to logd (via
// LogdSession.PatchIf/PatchTxIf) so the write commits only if it holds.
type PatchParams struct {
	TxID  *int64
	Match *api.PathData
}

// ControllerConfig configures RunController.
type ControllerConfig struct {
	// DocdAddr is docd's mount (controller-facing) address.
	DocdAddr string
	// LogdAddr is logd's address; when set, the controller's Handler can reach
	// logd via the mount's LogdSession. Optional for controllers that back their
	// subtree with something other than logd.
	LogdAddr string
	// Controller identifies this controller to docd.
	Controller string
	// Path is the subtree to mount (e.g. "/users").
	Path string
	// Schema is the optional schema contribution for the mount.
	Schema *ir.Node
	// Handler implements the controller's behavior.
	Handler Handler
	// Log is an optional logger.
	Log *slog.Logger
}

// RunController mounts to docd and serves operations for the mounted subtree
// until ctx is cancelled or the connection ends. It blocks for the lifetime of
// the controller.
func RunController(ctx context.Context, cfg *ControllerConfig) error {
	if cfg.Handler == nil {
		return fmt.Errorf("ControllerConfig.Handler is required")
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}

	client, err := Mount(&MountConfig{
		DocdAddr:   cfg.DocdAddr,
		LogdAddr:   cfg.LogdAddr,
		Controller: cfg.Controller,
		Path:       cfg.Path,
		Schema:     cfg.Schema,
	})
	if err != nil {
		return fmt.Errorf("mount failed: %w", err)
	}
	defer client.Close()

	rt := &controllerRuntime{
		client:  client,
		handler: cfg.Handler,
		log:     log.With("component", "controller", "controller", cfg.Controller),
		ctx:     ctx,
		watches: make(map[string]*watchReg),
	}

	// Unblock the serve loop's blocking read when the caller cancels.
	serveDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			client.Close()
		case <-serveDone:
		}
	}()

	err = rt.serve()
	close(serveDone)
	return err
}

// controllerRuntime dispatches docd operations to the Handler and writes
// responses back, mirroring the async, id-correlated logd session protocol on
// the controller side.
type controllerRuntime struct {
	client  *MountClient
	handler Handler
	log     *slog.Logger
	ctx     context.Context

	writeMu sync.Mutex // serializes writes to docd

	watchMu sync.Mutex
	watches map[string]*watchReg // active watches by watch id
}

// watchReg is one active watch. Watches are keyed by the request id docd
// assigns, not by path, because docd multiplexes many client sessions onto one
// controller connection and several may watch the same path at once; keying by
// id keeps each independent. path is retained for the WatchID-less fallback.
type watchReg struct {
	cancel context.CancelFunc
	path   string
}

// serve reads operations from docd and dispatches each until the connection
// ends.
func (rt *controllerRuntime) serve() error {
	defer rt.cancelAllWatches()

	for {
		node, err := rt.client.readDocument()
		if err != nil {
			if err == io.EOF || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("read from docd: %w", err)
		}

		var req api.SessionRequest
		if err := req.FromTonyIR(node); err != nil {
			rt.log.Error("failed to parse request from docd", "error", err)
			continue
		}
		rt.dispatch(&req)
	}
}

// dispatch routes one request to its Handler method. Match/patch/watch run in
// their own goroutines so a slow or long-lived op does not stall the read loop.
func (rt *controllerRuntime) dispatch(req *api.SessionRequest) {
	switch {
	case req.Match != nil:
		go rt.handleMatch(req)
	case req.Patch != nil:
		go rt.handlePatch(req)
	case req.Watch != nil:
		go rt.handleWatch(req)
	case req.Unwatch != nil:
		rt.handleUnwatch(req)
	default:
		rt.replyErr(req.ID, fmt.Errorf("%w: request type", ErrUnsupported))
	}
}

func (rt *controllerRuntime) handleMatch(req *api.SessionRequest) {
	body, err := rt.handler.Match(rt.ctx, req.Match.Body.Path, req.Match.Body.Data)
	if err != nil {
		rt.replyErr(req.ID, err)
		return
	}
	rt.reply(&api.SessionResponse{
		ID:     req.ID,
		Result: &api.SessionResult{Match: &api.MatchResult{Body: body}},
	})
}

func (rt *controllerRuntime) handlePatch(req *api.SessionRequest) {
	data, err := rt.handler.Patch(rt.ctx, req.Patch.Path, req.Patch.Data, PatchParams{
		TxID:  req.Patch.TxID,
		Match: req.Patch.Match,
	})
	if err != nil {
		rt.replyErr(req.ID, err)
		return
	}
	rt.reply(&api.SessionResponse{
		ID:     req.ID,
		Result: &api.SessionResult{Patch: &api.PatchResult{Data: data}},
	})
}

func (rt *controllerRuntime) handleWatch(req *api.SessionRequest) {
	path := req.Watch.Path
	key := watchKey(req.ID)

	ctx, cancel := context.WithCancel(rt.ctx)
	reg := &watchReg{cancel: cancel, path: path}
	rt.watchMu.Lock()
	rt.watches[key] = reg
	rt.watchMu.Unlock()
	defer func() {
		cancel()
		rt.watchMu.Lock()
		if rt.watches[key] == reg { // don't clobber a reused key
			delete(rt.watches, key)
		}
		rt.watchMu.Unlock()
	}()

	// The confirmation (WatchResult) is sent lazily: on the first emit for a
	// supported watch, or after the handler returns without emitting. This lets
	// a handler decline via ErrUnsupported before any confirmation is sent.
	confirmed := false
	confirm := func() {
		if confirmed {
			return
		}
		confirmed = true
		rt.reply(&api.SessionResponse{
			ID:     req.ID,
			Result: &api.SessionResult{Watch: &api.WatchResult{Watching: path}},
		})
	}
	emit := func(ev *api.WatchEvent) error {
		confirm()
		// Copy so defaulting the path never mutates the caller's event — a
		// controller may hand the same event to several watchers.
		out := *ev
		if out.Path == "" {
			out.Path = path
		}
		// The event carries the request id so docd can route it to the right
		// client; docd strips the id before delivering to the client.
		return rt.reply(&api.SessionResponse{ID: req.ID, Event: &out})
	}

	err := rt.handler.Watch(ctx, path, WatchParams{
		FromCommit: req.Watch.FromCommit,
		NoInit:     req.Watch.NoInit,
	}, emit)

	switch {
	case err == nil || errors.Is(err, context.Canceled):
		confirm() // supported but produced no events (e.g. NoInit, then cancelled)
	case !confirmed:
		rt.replyErr(req.ID, err) // declined before confirming
	default:
		rt.log.Error("watch handler failed after confirmation", "path", path, "error", err)
	}
}

func (rt *controllerRuntime) handleUnwatch(req *api.SessionRequest) {
	path := req.Unwatch.Path

	rt.watchMu.Lock()
	var cancels []context.CancelFunc
	if req.Unwatch.WatchID != nil {
		// Targeted: cancel exactly the identified watch.
		if reg := rt.watches[*req.Unwatch.WatchID]; reg != nil {
			cancels = append(cancels, reg.cancel)
			delete(rt.watches, *req.Unwatch.WatchID)
		}
	} else {
		// Fallback (e.g. a direct logd-style unwatch): cancel every watch on the
		// path.
		for id, reg := range rt.watches {
			if reg.path == path {
				cancels = append(cancels, reg.cancel)
				delete(rt.watches, id)
			}
		}
	}
	rt.watchMu.Unlock()

	for _, c := range cancels {
		c()
	}
	rt.reply(&api.SessionResponse{
		ID:     req.ID,
		Result: &api.SessionResult{Unwatch: &api.UnwatchResult{Unwatched: path}},
	})
}

// watchKey derives the map key for a watch from its request id. docd always
// assigns an id, so distinct watches (even on the same path) get distinct keys.
func watchKey(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

func (rt *controllerRuntime) cancelAllWatches() {
	rt.watchMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(rt.watches))
	for _, r := range rt.watches {
		cancels = append(cancels, r.cancel)
	}
	rt.watches = make(map[string]*watchReg)
	rt.watchMu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// reply encodes and writes a response to docd. Encoding happens under the same
// lock as the write: concurrent handlers must not interleave on the wire, and
// encoding mutates ir node linkage, so serializing it also keeps two handlers
// from racing on a node a controller shares across watchers.
func (rt *controllerRuntime) reply(resp *api.SessionResponse) error {
	rt.writeMu.Lock()
	defer rt.writeMu.Unlock()
	data, err := resp.ToTony(gomap.EncodeWire(true))
	if err != nil {
		rt.log.Error("failed to encode response", "error", err)
		return err
	}
	if _, err := rt.client.conn.Write(append(data, '\n')); err != nil {
		rt.log.Debug("failed to write response to docd", "error", err)
		return err
	}
	return nil
}

// replyErr sends an error response, mapping ErrUnsupported to the unsupported
// error code so docd and the client can distinguish a declined operation.
func (rt *controllerRuntime) replyErr(id *string, err error) {
	code := api.ErrCodeInvalidMessage
	switch {
	case errors.Is(err, ErrUnsupported):
		code = api.ErrCodeUnsupported
	case errors.Is(err, ErrMatchFailed):
		// A failed compare-and-swap must reach the client as match_failed so its
		// PatchIf/PatchTxIf surfaces ErrMatchFailed.
		code = api.ErrCodeMatchFailed
	}
	rt.reply(api.NewErrorResponse(id, code, err.Error()))
}
