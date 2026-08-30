package libctl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
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
	// pattern the client supplied (field selection and filtering). opts.Scope, when
	// set, is the COW scope the read belongs to — a scope-aware controller returns
	// its scoped view (a logd-backed one reads logd in that scope).
	//
	// opts.Commit, when set, is a point-in-time read: answer with the subtree's state
	// as of that commit. The commit is a logd commit, and that is the only timeline
	// there is: a mount takes the tx id docd allocates and commits through the same
	// backing logd, all-or-nothing, so every mount advances in one sequence and a
	// commit number means the same thing to all of them. A controller reads logd at
	// that commit (LogdSession.MatchAt) and is done.
	//
	// A controller which cannot answer for a commit is not participating in the
	// protocol -- it is broken, not a variety. What it must not do is answer from
	// some timeline of its own, because the answer is composed with every other
	// mount's at that commit; if it cannot, it must say so with an error.
	Match(ctx context.Context, path string, pattern *ir.Node, opts MatchParams) (*ir.Node, error)

	// Patch applies data at path and reports what the write landed as: Commit is
	// the commit it committed at and Data is the resulting data (with any
	// auto-generated ids). A logd-backed controller returns its LogdSession.PatchWith
	// result unchanged, so the client sees the same commit it would from a direct
	// logd write. A SELF-BACKED controller has no logd commit to report and should
	// leave Commit zero rather than invent one from its own timeline — the same
	// reason Match cannot honor a historical read. Returning nil is treated as an
	// empty result (no commit, no data).
	//
	// When opts.TxID is set, the client is coordinating a multi-participant
	// transaction: the controller joins it by writing to logd with that tx id
	// (the write is the join). opts.Scope, when set, is the COW scope the write
	// belongs to; a scope-aware controller must join the transaction in that scope
	// (a logd-backed one writes on a connection with that scope).
	Patch(ctx context.Context, path string, data *ir.Node, opts PatchParams) (*api.PatchResult, error)

	// Watch streams events for path until ctx is cancelled (the client
	// unwatched or disconnected) or it returns. emit delivers each event. To
	// decline watching, return ErrUnsupported before emitting.
	//
	// Event rooting follows the canonical logd contract so a client cannot tell a
	// controller-served subtree from logd: the first event's State is the full
	// state AT path (relative to it), but every subsequent event's Patch is
	// ROOT-ROOTED — the delta expressed from the document root (e.g. a change to
	// "a.b.x" is {a:{b:{x:...}}}), not relative to path. docd composes an ancestor
	// watch by forwarding these absolute deltas unchanged, re-stamping only the
	// event path, so controllers must emit root-rooted patches.
	Watch(ctx context.Context, path string, opts WatchParams, emit func(*api.WatchEvent) error) error
}

// MatchParams carries a match's ancillary options through to the Handler.
// Scope, when set, is the COW scope the read belongs to. Commit, when set, is a
// point-in-time read at that logd commit (see Handler.Match for the self-backed
// caveat).
type MatchParams struct {
	Scope  *string
	Commit *int64
}

// WatchParams carries the client's watch options through to the Handler. Scope,
// when set, is the COW scope the watch belongs to.
type WatchParams struct {
	FromCommit *int64
	NoInit     bool
	Scope      *string

	// WaitIfAbsent is the client's answer to what a watch on a path holding nothing
	// should do, and it has to reach the handler because the handler is what watches:
	// a controller serving from its own logd session passes this to that session, or
	// the client's request is refused at a hop it never asked about.
	WaitIfAbsent bool
}

// PatchParams carries a patch's options through to the Handler. TxID, when set,
// is the multi-participant transaction the patch must join. Match, when set, is a
// compare-and-swap precondition. Timeout, when set, is the per-participant wait
// timeout for the transaction. A controller participating in a docd-coordinated
// transaction must carry all three to its logd write (e.g. via
// LogdSession.PatchWith) so the participant behaves correctly and a stalled
// transaction aborts.
type PatchParams struct {
	TxID    *int64
	Match   *api.PathData
	Timeout *string
	Scope   *string
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
	// ForceAfter, when non-nil, overrides how long docd waits for overlapping
	// watch readers to drain before force-ending them so this mount can proceed (a
	// pointer to 0 means wait forever). Passed through to the mount handshake.
	ForceAfter *time.Duration
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
		ForceAfter: cfg.ForceAfter,
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

	// Unblock the serve loop's blocking read when the caller cancels. Cancelling is
	// an abrupt disconnect (docd tombstones the mount); a controller that wants to
	// detach cleanly instead calls MountClient.Unmount for a graceful drain.
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
		node, err := stream.ReadDocument(rt.client.decoder)
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
	body, err := rt.handler.Match(rt.ctx, req.Match.Path, req.Match.Data, MatchParams{Scope: req.Scope, Commit: req.Match.Commit})
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
	res, err := rt.handler.Patch(rt.ctx, req.Patch.Path, req.Patch.Data, PatchParams{
		TxID:    req.Patch.TxID,
		Match:   req.Patch.Match,
		Timeout: req.Patch.Timeout,
		Scope:   req.Scope,
	})
	if err != nil {
		rt.replyErr(req.ID, err)
		return
	}
	if res == nil { // a handler that reports neither commit nor data
		res = &api.PatchResult{}
	}
	rt.reply(&api.SessionResponse{
		ID:     req.ID,
		Result: &api.SessionResult{Patch: res},
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
		FromCommit:   req.Watch.FromCommit,
		NoInit:       req.Watch.NoInit,
		Scope:        req.Scope,
		WaitIfAbsent: req.Watch.WaitIfAbsent,
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
	data, err := resp.ToTony(api.WireOptions()...)
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

// forwardableCodes are the error codes that mean the same thing whichever hop reports
// them, and so may be passed to the client as the controller received them.
//
// The axis is what the code is ABOUT. These describe the document, or this request's
// relationship to it: they are as true for the client as they were for the controller,
// and a client acts on them the same way.
//
// The ones deliberately absent describe a CONNECTION or a lifecycle the client is not
// party to -- session_closed, protocol_mismatch, invalid_watch, not_watching,
// already_watching, slow_consumer, the replay_* pair, the tx_* family, and
// invalid_message. A controller's downstream session closing is not the client's session
// closing; a downstream calling the controller's message invalid is the controller's bug,
// not the client's. Forwarding those would name a condition that has not happened, which
// is the mistake docd made when a failed composed read reported session_closed.
var forwardableCodes = map[string]bool{
	api.ErrCodeNotFound:       true,
	api.ErrCodePathConflict:   true,
	api.ErrCodeInvalidPath:    true,
	api.ErrCodeInvalidDiff:    true,
	api.ErrCodeMatch:          true,
	api.ErrCodeMatchFailed:    true,
	api.ErrCodeCommitNotFound: true,
	api.ErrCodeScopeNotFound:  true,
	api.ErrCodeScopeExists:    true,
	api.ErrCodeUnsupported:    true,
	api.ErrCodeStorage:        true,
	api.ErrCodeTimeout:        true,
}

// replyErr answers a request the controller could not serve, with a code the client can
// act on. It takes the first of:
//
//  1. a forwardable code the error already carries -- a controller which serves from
//     another session hands back that session's error, and its classification with it
//  2. this package's own sentinels, for an error a handler built rather than received
//  3. storage_error, for a responder that failed and did not say why
//
// The default used to be invalid_message, which says the CLIENT's request was wrong. Every
// caller here but one is a handler that failed, and the one that is not passes
// ErrUnsupported explicitly -- so the default blamed the request for the responder's
// failure, and a client acting on it would rewrite a request that was fine. logd draws the
// line the same way: storage_error is what it sends when the server could not do the
// thing, and invalid_message when the message itself was wrong.
func (rt *controllerRuntime) replyErr(id *string, err error) {
	rt.reply(api.NewErrorResponse(id, replyErrCode(err), err.Error()))
}

// replyErrCode is the decision replyErr makes, as a function of the error alone, so that
// what a client is told can be checked without a connection to tell it over.
func replyErrCode(err error) string {
	switch carried := api.ErrorCode(err); {
	case forwardableCodes[carried]:
		return carried
	case errors.Is(err, ErrUnsupported):
		return api.ErrCodeUnsupported
	case errors.Is(err, ErrMatchFailed):
		// A failed compare-and-swap must reach the client as match_failed so its
		// PatchIf/PatchTxIf surfaces ErrMatchFailed.
		return api.ErrCodeMatchFailed
	}
	return api.ErrCodeStorage
}
