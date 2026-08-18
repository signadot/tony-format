package server

import (
	"fmt"
	"net"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// readLogdMatch reads the state at path directly from logd over a short-lived
// baseline connection, returning the matched body and commit. atCommit, when
// non-nil, reads historical state at that commit rather than the current one.
//
// A composed read cannot use the client's own logd connection for its base
// portion: responses on that connection are auto-forwarded to the client by the
// logd pump, but a composed read must intercept the base result and merge it with
// the mount subtrees before replying. A dedicated connection keeps the base read
// collectable, mirroring writeBaseParticipant on the write path.
func readLogdMatch(logdAddr, path string, scope *string, atCommit *int64, timeout time.Duration) (*ir.Node, int64, error) {
	conn, err := net.DialTimeout("tcp", logdAddr, 5*time.Second)
	if err != nil {
		return nil, 0, fmt.Errorf("connect to logd at %s: %w", logdAddr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	dec, err := stream.NewDecoder(conn, stream.WithBrackets())
	if err != nil {
		return nil, 0, err
	}

	// Hello in the client's scope so the base read sees the scoped (COW) view.
	if err := writeSessionRequest(conn, &logdapi.SessionRequest{
		Hello: &logdapi.Hello{ClientID: "docd-read", Scope: scope},
	}); err != nil {
		return nil, 0, fmt.Errorf("hello: %w", err)
	}
	if _, err := readSessionResponse(dec); err != nil {
		return nil, 0, fmt.Errorf("hello response: %w", err)
	}

	if err := writeSessionRequest(conn, &logdapi.SessionRequest{
		Match: &logdapi.MatchRequest{PathData: logdapi.PathData{Path: path}, Commit: atCommit},
	}); err != nil {
		return nil, 0, err
	}
	resp, err := readSessionResponse(dec)
	if err != nil {
		return nil, 0, err
	}
	if resp.Error != nil {
		return nil, 0, fmt.Errorf("logd match at %q: %s", path, resp.Error.Message)
	}
	if resp.Result == nil || resp.Result.Match == nil {
		return nil, 0, fmt.Errorf("logd match at %q: empty result", path)
	}
	return resp.Result.Match.Body, resp.Result.Match.Commit, nil
}
