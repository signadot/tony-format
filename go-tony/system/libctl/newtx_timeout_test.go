package libctl

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	logdserver "github.com/signadot/tony-format/go-tony/system/logd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A newtx naming a timeout gets that timeout through docd too. docd answers a baseline
// newtx from its pool of ids fetched in advance, and a pooled id was created with logd's
// timeout: the client's would be dropped, and a transaction short of a participant would
// hold the others for logd's 5m rather than the client's 200ms.
func TestNewTxTimeoutThroughDocd(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	logd := logdserver.New(&logdserver.Spec{
		Storage: store,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err := logd.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logd.StopTCP() })
	docd := startDocdRouting(t, logd.TCPAddr())

	ctx := context.Background()
	client := NewLogdSession(&LogdSessionConfig{Addr: docd.ClientTCPAddr(), ClientID: "c"})
	t.Cleanup(func() { client.Close() })

	timeout := "200ms"
	resp, err := client.request(ctx, &api.SessionRequest{
		NewTx: &api.NewTxRequest{Participants: 2, Timeout: &timeout},
	})
	if err != nil {
		t.Fatalf("newtx: %v", err)
	}
	if resp.Error != nil || resp.Result == nil || resp.Result.NewTx == nil {
		t.Fatalf("newtx: %+v", resp)
	}

	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	start := time.Now()
	_, err = client.PatchTx(pctx, "a", vObj(1), resp.Result.NewTx.TxID)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("a transaction short of a participant committed")
	}
	// 200ms, or logd's cleanup tick (1s) if it expires the transaction first.
	if elapsed > 2*time.Second {
		t.Errorf("the lone participant was answered after %v (%v), want about the newtx's 200ms", elapsed, err)
	}
}
