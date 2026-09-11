package server

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// A transaction's timeout bounds every participant's wait, and there is no transaction
// without one (hqhyyat8h12ksarmcdn0). These run over TCP because the hang was TCP's:
// TCPListener.Close waits for every session, and a session waits for its participants.

// startTxServer serves a fresh store over TCP with the default config.
func startTxServer(t *testing.T) *Server {
	t.Helper()
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	srv := New(&Spec{Storage: store})
	if err := srv.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("start TCP: %v", err)
	}
	t.Cleanup(func() { srv.StopTCP() })
	return srv
}

// txClient is one session to a test server, reading its answers in order.
type txClient struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
}

func dialTx(t *testing.T, srv *Server) *txClient {
	t.Helper()
	conn, err := net.Dial("tcp", srv.TCPAddr())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	c := &txClient{t: t, conn: conn, r: bufio.NewReader(conn)}
	c.send(fmt.Sprintf(`{hello: {clientId: tx-timeout, protocol: %d}}`, api.ProtocolVersion))
	if resp := c.recv(time.Second); resp.Result == nil || resp.Result.Hello == nil {
		t.Fatalf("hello: %+v", resp)
	}
	return c
}

func (c *txClient) send(req string) {
	c.t.Helper()
	if _, err := c.conn.Write([]byte(req + "\n")); err != nil {
		c.t.Fatalf("write %s: %v", req, err)
	}
}

// recv answers the next response, failing if none arrives within wait.
func (c *txClient) recv(wait time.Duration) *api.SessionResponse {
	c.t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(wait))
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		c.t.Fatalf("no response within %v: %v", wait, err)
	}
	var resp api.SessionResponse
	if err := resp.FromTony(bytes.TrimSpace(line)); err != nil {
		c.t.Fatalf("parse %q: %v", line, err)
	}
	return &resp
}

// newTx creates a transaction from the newtx request body and answers its id.
func (c *txClient) newTx(body string) int64 {
	c.t.Helper()
	c.send(`{id: t, newtx: ` + body + `}`)
	resp := c.recv(time.Second)
	if resp.Error != nil || resp.Result == nil || resp.Result.NewTx == nil {
		c.t.Fatalf("newtx %s: %+v", body, resp)
	}
	return resp.Result.NewTx.TxID
}

// The server's tx.timeout is 5m unless configured, and configuring 0 is 5m too: it
// used to mean no timeout, which is the transaction that waited for ever.
func TestTxConfig_ZeroTimeoutIsTheDefault(t *testing.T) {
	if got := time.Duration(DefaultConfig().Tx.Timeout); got != tx.DefaultTimeout {
		t.Errorf("default tx.timeout = %v, want %v", got, tx.DefaultTimeout)
	}

	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer store.Close()
	New(&Spec{Storage: store, Config: &Config{Tx: &TxConfig{Timeout: 0}}})
	txn, err := store.NewTx(2, nil)
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	if got := txn.Timeout(); got != tx.DefaultTimeout {
		t.Errorf("tx.timeout: 0 gave a transaction %v, want %v", got, tx.DefaultTimeout)
	}
}

// A participant which names no timeout waits as long as its transaction does, and the
// transaction's is the one newtx named -- not the server's, which is 5m here.
func TestNewTx_ParticipantWaitsTheTransactionsTimeout(t *testing.T) {
	c := dialTx(t, startTxServer(t))
	txID := c.newTx(`{participants: 2, timeout: "200ms"}`)

	start := time.Now()
	c.send(fmt.Sprintf(`{id: p1, patch: {txId: %d, path: a, data: {n: 1}}}`, txID))
	resp := c.recv(5 * time.Second)
	elapsed := time.Since(start)

	if resp.Error == nil {
		t.Fatalf("a transaction short of a participant answered %+v, want its failure", resp.Result)
	}
	if !strings.Contains(resp.Error.Message, "transaction") {
		t.Errorf("answered %s: %s, want the transaction's timeout", resp.Error.Code, resp.Error.Message)
	}
	// 200ms, or the store's cleanup tick (1s) if it expires the transaction first.
	if elapsed > 2*time.Second {
		t.Errorf("answered after %v, want about the transaction's 200ms", elapsed)
	}
}

// A timeout newtx cannot read is the client's mistake, and is said so rather than
// replaced by the server's. So is one above the server's: the server's is the ceiling,
// and a client may ask for less of it, not more.
func TestNewTx_RefusesATimeoutItCannotReadOrAbove(t *testing.T) {
	c := dialTx(t, startTxServer(t)) // the server's timeout: 5m
	for _, timeout := range []string{"soon", "6m"} {
		c.send(fmt.Sprintf(`{id: t, newtx: {participants: 2, timeout: %q}}`, timeout))
		resp := c.recv(time.Second)
		if resp.Error == nil || resp.Error.Code != api.ErrCodeInvalidTx {
			t.Fatalf("newtx with timeout %q answered %+v, want %s", timeout, resp, api.ErrCodeInvalidTx)
		}
		t.Logf("timeout %q: %s", timeout, resp.Error.Message)
	}
}

// A participant waiting for the others held its session, and StopTCP with it, for the
// transaction's whole timeout -- for ever when there was none. A closing session stops
// waiting for participants which have not arrived; the transaction resolves in the store
// without it.
func TestStopTCP_NotHeldByAParticipantWaitingForOthers(t *testing.T) {
	srv := startTxServer(t)
	c := dialTx(t, srv)
	txID := c.newTx(`{participants: 2}`) // the server's timeout: 5m

	c.send(fmt.Sprintf(`{id: p1, patch: {txId: %d, path: a, data: {n: 1}}}`, txID))
	// A joining patch runs off the request loop, so no answer says it has joined; give
	// it the moment it needs.
	time.Sleep(200 * time.Millisecond)

	stopped := make(chan error, 1)
	go func() { stopped <- srv.StopTCP() }()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("StopTCP is held by a participant waiting for a participant which never comes")
	}
}
