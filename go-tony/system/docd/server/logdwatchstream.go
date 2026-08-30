package server

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/signadot/tony-format/go-tony/stream"
	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// logdWatchStream is a dedicated logd connection carrying the base (unmounted)
// sub-watch of a composed watch. It is separate from the client's own logd link
// because that link auto-forwards responses to the client, whereas a composed
// base watch must be intercepted and re-stamped before delivery.
type logdWatchStream struct {
	conn      net.Conn
	closeOnce sync.Once
}

// startLogdWatchStream opens a logd connection, watches path with NoInit (the
// composer supplies the single initial snapshot), and pumps every event to onMsg
// until Stop is called or the connection drops. It returns once the watch is
// confirmed, or an error if the watch cannot be established.
func startLogdWatchStream(logdAddr, path string, scope *string, fromCommit *int64, onMsg func(*logdapi.SessionResponse)) (*logdWatchStream, error) {
	conn, err := net.DialTimeout("tcp", logdAddr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to logd at %s: %w", logdAddr, err)
	}
	dec, err := stream.NewDecoder(conn, stream.WithBrackets())
	if err != nil {
		conn.Close()
		return nil, err
	}

	if err := writeSessionRequest(conn, &logdapi.SessionRequest{
		Hello: &logdapi.Hello{ClientID: "docd-watch", Scope: scope},
	}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("hello: %w", err)
	}
	if _, err := readSessionResponse(dec); err != nil {
		conn.Close()
		return nil, fmt.Errorf("hello response: %w", err)
	}

	if err := writeSessionRequest(conn, &logdapi.SessionRequest{
		// waitIfAbsent: this is one source of a composed watch, and a source with
		// nothing at the path is ordinary. See watchReq in watch.go.
		Watch: &logdapi.WatchRequest{Path: path, NoInit: true, FromCommit: fromCommit, WaitIfAbsent: true},
	}); err != nil {
		conn.Close()
		return nil, err
	}
	resp, err := readSessionResponse(dec)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.Error != nil {
		conn.Close()
		return nil, fmt.Errorf("logd watch %q: %s", path, resp.Error.Message)
	}

	w := &logdWatchStream{conn: conn}
	go w.pump(dec, onMsg)
	return w, nil
}

// pump forwards each logd event to onMsg until the connection ends, then signals
// the end with an error response so the composer can tear down and re-sync.
func (w *logdWatchStream) pump(dec *stream.Decoder, onMsg func(*logdapi.SessionResponse)) {
	for {
		resp, err := readSessionResponse(dec)
		if err != nil {
			onMsg(logdapi.NewErrorResponse(nil, logdapi.ErrCodeSessionClosed, "logd base watch ended"))
			return
		}
		onMsg(resp)
	}
}

// Stop closes the connection, ending the base sub-watch.
func (w *logdWatchStream) Stop() {
	w.closeOnce.Do(func() { w.conn.Close() })
}
