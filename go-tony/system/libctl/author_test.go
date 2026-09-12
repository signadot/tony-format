package libctl

import (
	"context"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// authorOf answers who logd recorded commit as written by, read from the store's own
// event stream: a watch replaying the commit carries the author the entry does.
func authorOf(t *testing.T, logdAddr string, commit int64) string {
	t.Helper()
	direct := NewLogdSession(&LogdSessionConfig{Addr: logdAddr, ClientID: "auditor"})
	defer direct.Close()
	from := commit - 1
	w, err := direct.Watch(context.Background(), "", &WatchOptions{FromCommit: &from})
	if err != nil {
		t.Fatalf("watch from %d: %v", from, err)
	}
	defer w.Close()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-w.Events():
			if ev == nil {
				t.Fatal("the watch ended before the commit was replayed")
			}
			if ev.Patch != nil && ev.Commit == commit {
				return ev.Author
			}
		case <-deadline:
			t.Fatalf("commit %d was not replayed", commit)
		}
	}
}

// A write's author reaches the commit through docd however docd routes it: on the
// client's own logd link, to a controller, or split into a transaction whose
// participants docd and the controllers write on sessions of their own. The client's
// hello names the default and a patch may name its own; docd resolves the two where the
// write leaves the client's session, so no hop commits it under a hello that is not the
// client's (cn1n32yph12ks5wrmhn0).
func TestAuthorRidesEveryRouteThroughDocd(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "a", newLogdController(t, logd.TCPAddr(), "cA"))
	runController(t, docd, "b", newLogdController(t, logd.TCPAddr(), "cB"))

	client := NewLogdSession(&LogdSessionConfig{Addr: docd.ClientTCPAddr(), ClientID: "client", Author: "verse"})
	defer client.Close()
	ctx := context.Background()

	cases := []struct {
		name  string
		path  string
		data  *ir.Node
		opts  PatchOpts
		want  string
		split bool
	}{
		{name: "base, the hello's", path: "cfg", data: vObj(1), want: "verse"},
		{name: "base, the patch's own", path: "cfg", data: vObj(2), opts: PatchOpts{Author: "alice"}, want: "alice"},
		{name: "a controller, the hello's", path: "a.x", data: vObj(3), want: "verse"},
		{name: "a controller, the patch's own", path: "a.x", data: vObj(4), opts: PatchOpts{Author: "bob"}, want: "bob"},
		{name: "split across two controllers and the base, the hello's", path: "",
			data: ir.FromMap(map[string]*ir.Node{"a": ir.FromMap(map[string]*ir.Node{"x": vObj(5)}), "b": ir.FromMap(map[string]*ir.Node{"x": vObj(5)}), "cfg": vObj(5)}),
			want: "verse", split: true},
		{name: "split, the patch's own", path: "",
			data: ir.FromMap(map[string]*ir.Node{"a": ir.FromMap(map[string]*ir.Node{"x": vObj(6)}), "b": ir.FromMap(map[string]*ir.Node{"x": vObj(6)}), "cfg": vObj(6)}),
			opts: PatchOpts{Author: "carol"}, want: "carol", split: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := client.PatchWith(ctx, tc.path, tc.data, tc.opts)
			if err != nil {
				t.Fatalf("patch: %v", err)
			}
			if got := authorOf(t, logd.TCPAddr(), res.Commit); got != tc.want {
				t.Errorf("commit %d is recorded as written by %q, want %q", res.Commit, got, tc.want)
			}
		})
	}
}

// A client's own transaction through docd carries one author on every participant, and a
// participant naming another is refused as it would be on logd -- the code crosses the
// hop, since it is about the write and not the connection.
func TestClientTransactionThroughDocdHasOneAuthor(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "verse.a", newLogdController(t, logd.TCPAddr(), "ctrlA"))
	runController(t, docd, "verse.b", newLogdController(t, logd.TCPAddr(), "ctrlB"))

	client := docdClient(t, docd, "client")
	ctx := context.Background()
	txID, err := client.NewTx(ctx, 2)
	if err != nil {
		t.Fatalf("newtx through docd: %s", err)
	}

	type res struct {
		r   *api.PatchResult
		err error
	}
	done := make(chan res, 2)
	// alice joins first and waits; bob is refused; alice's second participant completes.
	go func() {
		r, err := client.PatchWith(ctx, "verse.a.x", vObj(1), PatchOpts{TxID: &txID, Author: "alice"})
		done <- res{r, err}
	}()
	time.Sleep(200 * time.Millisecond)
	if _, err := client.PatchWith(ctx, "verse.b.y", vObj(2), PatchOpts{TxID: &txID, Author: "bob"}); api.ErrorCode(err) != api.ErrCodeTxAuthorMismatch {
		t.Fatalf("bob's participant in alice's transaction: err = %v, want %s", err, api.ErrCodeTxAuthorMismatch)
	}
	go func() {
		r, err := client.PatchWith(ctx, "verse.b.y", vObj(2), PatchOpts{TxID: &txID, Author: "alice"})
		done <- res{r, err}
	}()
	var commit int64
	for i := 0; i < 2; i++ {
		got := <-done
		if got.err != nil {
			t.Fatalf("alice's participant %d: %s", i, got.err)
		}
		commit = got.r.Commit
	}
	if got := authorOf(t, logd.TCPAddr(), commit); got != "alice" {
		t.Errorf("the transaction is recorded as written by %q, want alice", got)
	}
}
