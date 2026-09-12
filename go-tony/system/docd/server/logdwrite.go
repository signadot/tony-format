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
// transaction: it opens a short-lived logd connection in the client's scope and
// joins transaction txID by writing one base remainder at its path, with an
// optional compare-and-swap precondition. It blocks until the transaction commits
// (all participants joined) or fails -- the transaction's timeout bounds that, and
// it names none of its own (coordinatePatch) -- and returns the logd response.
//
// A dedicated connection is used (rather than a shared/pooled one) because the
// write blocks until the whole transaction commits; a fresh connection keeps
// concurrent coordinations from serializing on one link. Pooling these is a
// possible later optimization.
func writeBaseParticipant(logdAddr string, txID int64, path string, base, match *ir.Node, matchPath string, scope *string) (*logdapi.SessionResponse, error) {
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
		Hello: logdHello("docd-base", scope),
	}); err != nil {
		return nil, fmt.Errorf("hello: %w", err)
	}
	if _, err := readSessionResponse(dec); err != nil {
		return nil, fmt.Errorf("hello response: %w", err)
	}

	// Join the transaction by writing the base remainder at its path. The write is
	// recorded under the transaction's author, which allocTx set to the client's; this
	// connection's hello is docd's and plays no part.
	req := &logdapi.SessionRequest{
		Patch: &logdapi.PatchRequest{
			TxID:     &txID,
			Match:    matchPathData(matchPath, match),
			PathData: logdapi.PathData{Path: path, Data: base},
		},
	}
	if err := writeSessionRequest(conn, req); err != nil {
		return nil, err
	}
	return readSessionResponse(dec)
}

// allocTx creates a multi-participant transaction in scope (nil is baseline), written
// by author (the client's, resolved; empty is none), on a short-lived logd connection,
// and returns its id. The transaction lives in logd storage keyed by id (independent of
// the creating connection), so participants -- docd's base write and each controller --
// then join it on their own connections in that scope, and inherit its author.
//
// It is created for the write that asks, never in advance: a transaction's timeout
// runs from its creation, so an id fetched early and held was a transaction dying
// in the hand -- docd kept a pool of them, and every id it held longer than logd's
// timeout was answered tx_not_found when it was finally used.
func allocTx(logdAddr string, scope *string, participants int, author string) (int64, error) {
	conn, err := net.DialTimeout("tcp", logdAddr, 5*time.Second)
	if err != nil {
		return 0, fmt.Errorf("connect to logd at %s: %w", logdAddr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	dec, err := stream.NewDecoder(conn, stream.WithBrackets())
	if err != nil {
		return 0, err
	}
	if err := writeSessionRequest(conn, &logdapi.SessionRequest{
		Hello: logdHello("docd-tx", scope),
	}); err != nil {
		return 0, fmt.Errorf("hello: %w", err)
	}
	if _, err := readSessionResponse(dec); err != nil {
		return 0, fmt.Errorf("hello response: %w", err)
	}
	if err := writeSessionRequest(conn, &logdapi.SessionRequest{
		NewTx: &logdapi.NewTxRequest{Participants: participants, Author: author},
	}); err != nil {
		return 0, err
	}
	resp, err := readSessionResponse(dec)
	if err != nil {
		return 0, err
	}
	if resp.Error != nil {
		return 0, fmt.Errorf("newtx: %s", resp.Error.Message)
	}
	if resp.Result == nil || resp.Result.NewTx == nil {
		return 0, fmt.Errorf("newtx: empty result")
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
