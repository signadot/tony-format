package libctl

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	logdserver "github.com/signadot/tony-format/go-tony/system/logd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// docd is a client of logd on every session it opens -- a read, a write, a watch, the
// watermark, the transaction pool -- and each says which protocol it speaks, so the hop
// between every verse and its logd is checked at the handshake as the others are. A
// hello built by hand without the version is accepted as a client from before versions
// existed, and logd says so in its log; that line must never be about docd.
func TestEveryDocdSessionSpeaksTheProtocol(t *testing.T) {
	var logged bytes.Buffer
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	logd := logdserver.New(&logdserver.Spec{
		Storage: store,
		Log:     slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err := logd.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logd.StopTCP() })
	docd := startDocdRouting(t, logd.TCPAddr())
	// A mount, so that operations above it are COMPOSED: those are the ones docd
	// serves from sessions of its own. A path with no mount under it is proxied on
	// the client's own hello, which says its version itself.
	runController(t, docd, "m", newWatchableLogdController(t, logd.TCPAddr(), "cM"))

	ctx := context.Background()
	client := NewLogdSession(&LogdSessionConfig{Addr: docd.ClientTCPAddr(), ClientID: "c"})
	t.Cleanup(func() { client.Close() })
	if _, err := client.Patch(ctx, "base", vObj(1)); err != nil {
		t.Fatalf("base write: %v", err)
	}
	if _, err := client.Patch(ctx, "m", vObj(2)); err != nil {
		t.Fatalf("mount write: %v", err)
	}
	// Split across base and the mount: docd-base and the pool.
	if _, err := client.Patch(ctx, "", mustParseLibctl(t, `{base: {v: 3}, m: {v: 4}}`)); err != nil {
		t.Fatalf("split write: %v", err)
	}
	if _, err := client.Match(ctx, ""); err != nil { // composed: docd-read
		t.Fatalf("composed read: %v", err)
	}
	w, err := client.Watch(ctx, "", nil) // composed: docd-watch
	if err != nil {
		t.Fatalf("composed watch: %v", err)
	}
	expectEvent(t, w)
	w.Close()
	from := int64(-1)
	replay, err := client.Watch(ctx, "", &WatchOptions{FromCommit: &from}) // docd-watermark
	if err != nil {
		t.Fatalf("composed replay: %v", err)
	}
	expectEvent(t, replay)
	replay.Close()

	log := logged.String()
	if strings.Contains(log, "assuming the current one") {
		t.Fatalf("logd accepted a docd session that did not say its protocol:\n%s", log)
	}
	for _, id := range []string{"docd-txpool", "docd-read", "docd-watch", "docd-watermark", "docd-base"} {
		if !strings.Contains(log, "msg=hello") || !strings.Contains(log, "clientId="+id) {
			t.Errorf("logd never saw a hello from %s:\n%s", id, log)
		}
	}
}
