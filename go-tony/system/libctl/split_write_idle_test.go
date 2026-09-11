package libctl

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	logdserver "github.com/signadot/tony-format/go-tony/system/logd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A split write commits however long docd has been idle. Its transaction is created
// for the write; docd used to keep a pool of transaction ids fetched in advance, and a
// transaction's timeout runs from its creation, so an id held longer than logd's
// timeout was answered tx_not_found when it was used -- measured: with tx.timeout 1s,
// every split write more than ~2s after the pool warmed failed.
func TestSplitWrite_AfterIdlingPastTxTimeout(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	logd := logdserver.New(&logdserver.Spec{
		Storage: store,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config:  &logdserver.Config{Tx: &logdserver.TxConfig{Timeout: logdserver.Duration(time.Second)}},
	})
	if err := logd.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logd.StopTCP() })
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "m", newWatchableLogdController(t, logd.TCPAddr(), "cM"))

	ctx := context.Background()
	client := NewLogdSession(&LogdSessionConfig{Addr: docd.ClientTCPAddr(), ClientID: "c"})
	t.Cleanup(func() { client.Close() })

	if _, err := client.Patch(ctx, "", mustParseLibctl(t, `{base: {v: 1}, m: {v: 1}}`)); err != nil {
		t.Fatalf("split write 1: %v", err)
	}
	time.Sleep(3 * time.Second) // past tx.timeout, and the cleanup tick after it
	if _, err := client.Patch(ctx, "", mustParseLibctl(t, `{base: {v: 2}, m: {v: 2}}`)); err != nil {
		t.Fatalf("split write 2, after idling past tx.timeout: %v", err)
	}
}
