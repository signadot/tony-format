package libctl

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
)

// TestWatch_KeyingChangeEndsThroughDocd is the client end of 62r9amwph12krxfjn9n0: a
// schema commit that changes the keying of an array ends a watch on one of its elements,
// through docd as directly, and the caller is told why and where to watch again from.
func TestWatch_KeyingChangeEndsThroughDocd(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdProxy(t, logd.TCPAddr())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	admin := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "admin"})
	defer admin.Close()
	setSchema := func(doc string) int64 {
		t.Helper()
		schema, err := parse.Parse([]byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		commit, err := admin.SetSchema(ctx, schema, false)
		if err != nil {
			t.Fatalf("SetSchema %s: %v", doc, err)
		}
		return commit
	}
	setSchema(`{define: {runs: {id: !logd-key null}}}`)
	runs, err := parse.Parse([]byte(`[{id: r1, sku: A, n: 1}]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Patch(ctx, "runs", runs); err != nil {
		t.Fatalf("seed: %v", err)
	}

	client := NewLogdSession(&LogdSessionConfig{Addr: docd.ClientTCPAddr(), ClientID: "via-docd"})
	defer client.Close()
	// docd takes the store's spelling of an element; logd takes the sugar as well.
	w, err := client.Watch(ctx, `runs."(id=r1)"`, nil)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer w.Close()

	c := setSchema(`{define: {runs: {sku: !logd-key null}}}`)
	for range w.Events() {
		// the state, and nothing for the change itself
	}
	var ended *WatchEndedError
	if !errors.As(w.Err(), &ended) {
		t.Fatalf("the watch ended with %v, want a WatchEndedError", w.Err())
	}
	if ended.Reason != logdapi.ErrCodeKeyingChanged || ended.Commit != c {
		t.Errorf("ended with %s at %d (%s), want %s at %d", ended.Reason, ended.Commit, ended.Message, logdapi.ErrCodeKeyingChanged, c)
	}

	// Watching again from there, spelled as the schema now does, works.
	again, err := client.Watch(ctx, `runs."(sku=A)"`, &WatchOptions{FromCommit: &c})
	if err != nil {
		t.Fatalf("watch again: %v", err)
	}
	defer again.Close()
	select {
	case ev := <-again.Events():
		if ev == nil || ev.State == nil {
			t.Fatalf("the re-established watch began with %+v, want its state", ev)
		}
		if n := ir.Get(ev.State, "n"); n == nil || n.Int64 == nil || *n.Int64 != 1 {
			t.Errorf("state %v, want the element with n: 1", ev.State)
		}
	case <-ctx.Done():
		t.Fatal("no state for the re-established watch")
	}
}
