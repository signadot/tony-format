package server

import (
	"fmt"
	"net"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// writeBaseParticipant is docd's own participant in a coordinated multi-mount
// transaction: it opens a short-lived baseline logd connection and joins
// transaction txID by writing the base remainder at the document root, with an
// optional compare-and-swap precondition and a per-participant timeout so a
// stalled transaction aborts. It blocks until the transaction commits (all
// participants joined) or fails, and returns the logd response.
//
// A dedicated connection is used (rather than a shared/pooled one) because the
// write blocks until the whole transaction commits; a fresh connection keeps
// concurrent coordinations from serializing on one link. Pooling these is a
// possible later optimization.
func writeBaseParticipant(logdAddr string, txID int64, path string, base, match *ir.Node, matchPath string, scope *string, timeout time.Duration) (*logdapi.SessionResponse, error) {
	conn, err := net.DialTimeout("tcp", logdAddr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to logd at %s: %w", logdAddr, err)
	}
	defer conn.Close()

	dec, err := stream.NewDecoder(conn, stream.WithBrackets())
	if err != nil {
		return nil, err
	}

	// Hello in the client's scope so this participant joins the tx in that scope
	// (logd requires all participants to share the transaction's scope).
	if err := writeSessionRequest(conn, &logdapi.SessionRequest{
		Hello: &logdapi.Hello{ClientID: "docd-base", Scope: scope},
	}); err != nil {
		return nil, fmt.Errorf("hello: %w", err)
	}
	if _, err := readSessionResponse(dec); err != nil {
		return nil, fmt.Errorf("hello response: %w", err)
	}

	// Join the transaction by writing the base remainder at its path.
	ts := timeout.String()
	req := &logdapi.SessionRequest{
		Patch: &logdapi.PatchRequest{
			TxID:     &txID,
			Timeout:  &ts,
			Match:    matchPathData(matchPath, match),
			PathData: logdapi.PathData{Path: path, Data: base},
		},
	}
	if err := writeSessionRequest(conn, req); err != nil {
		return nil, err
	}
	return readSessionResponse(dec)
}

// allocScopedTx creates a multi-participant transaction bound to a COW scope, on
// a short-lived scoped logd connection, and returns its id. The transaction lives
// in logd storage keyed by id (independent of the creating connection), so
// participants — docd's base write and each controller — then join it on their
// own scoped connections. Baseline transactions come from the (scopeless) txpool
// instead; scoped ones cannot, so they are allocated here.
func allocScopedTx(logdAddr string, scope *string, participants int, timeout time.Duration) (int64, error) {
	conn, err := net.DialTimeout("tcp", logdAddr, 5*time.Second)
	if err != nil {
		return 0, fmt.Errorf("connect to logd at %s: %w", logdAddr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	dec, err := stream.NewDecoder(conn, stream.WithBrackets())
	if err != nil {
		return 0, err
	}
	if err := writeSessionRequest(conn, &logdapi.SessionRequest{
		Hello: &logdapi.Hello{ClientID: "docd-tx", Scope: scope},
	}); err != nil {
		return 0, fmt.Errorf("hello: %w", err)
	}
	if _, err := readSessionResponse(dec); err != nil {
		return 0, fmt.Errorf("hello response: %w", err)
	}
	if err := writeSessionRequest(conn, &logdapi.SessionRequest{
		NewTx: &logdapi.NewTxRequest{Participants: participants},
	}); err != nil {
		return 0, err
	}
	resp, err := readSessionResponse(dec)
	if err != nil {
		return 0, err
	}
	if resp.Error != nil {
		return 0, fmt.Errorf("newtx in scope: %s", resp.Error.Message)
	}
	if resp.Result == nil || resp.Result.NewTx == nil {
		return 0, fmt.Errorf("newtx in scope: empty result")
	}
	return resp.Result.NewTx.TxID, nil
}

// matchPathData wraps a CAS precondition, or nil when there is none.
func matchPathData(path string, data *ir.Node) *logdapi.PathData {
	if data == nil {
		return nil
	}
	return &logdapi.PathData{Path: path, Data: data}
}

func writeSessionRequest(conn net.Conn, req *logdapi.SessionRequest) error {
	data, err := req.ToTony(logdapi.WireOptions()...)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write to logd: %w", err)
	}
	return nil
}

func readSessionResponse(dec *stream.Decoder) (*logdapi.SessionResponse, error) {
	node, err := stream.ReadDocument(dec)
	if err != nil {
		return nil, err
	}
	var resp logdapi.SessionResponse
	if err := resp.FromTonyIR(node); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &resp, nil
}
