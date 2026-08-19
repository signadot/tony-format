package libctl

import (
	"context"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A client's own transaction spans mounts, because mounts share the commit sequence: each
// participant is routed to its controller, which joins that transaction on the one logd.
func TestClientTransactionAcrossMounts(t *testing.T) {
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
	for i, p := range []string{"verse.a.x", "verse.b.y"} {
		go func(p string, n int64) {
			r, err := client.PatchTx(ctx, p, ir.FromMap(map[string]*ir.Node{"n": ir.FromInt(n)}), txID)
			done <- res{r, err}
		}(p, int64(i))
	}
	var commits []int64
	for i := 0; i < 2; i++ {
		got := <-done
		if got.err != nil {
			t.Fatalf("participant %d: %s", i, got.err)
		}
		commits = append(commits, got.r.Commit)
	}
	if commits[0] != commits[1] {
		t.Errorf("participants committed at %d and %d; a transaction is one commit", commits[0], commits[1])
	}
}

// A participant patch which itself spans mounts is refused, because it cannot be honoured:
// docd would decompose it into several participants, and the transaction's count was fixed
// by the client, which counted its patches rather than docd's decomposition of one of them.
//
// It used to be allocated a transaction of docd's own and COMMITTED -- while the client's
// other participants waited for a participant that would never come. A write the client was
// told was atomic with another landed alone (zh3bm3msh12kscpygnn0).
func TestSpanningPatchInATransactionIsRefused(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "verse.a", newLogdController(t, logd.TCPAddr(), "ctrlA"))
	runController(t, docd, "verse.b", newLogdController(t, logd.TCPAddr(), "ctrlB"))

	client := docdClient(t, docd, "client")
	ctx := context.Background()
	txID, err := client.NewTx(ctx, 2)
	if err != nil {
		t.Fatalf("newtx: %s", err)
	}

	_, err = client.PatchTx(ctx, "verse", ir.FromMap(map[string]*ir.Node{
		"a": ir.FromMap(map[string]*ir.Node{"x": ir.FromInt(1)}),
		"b": ir.FromMap(map[string]*ir.Node{"y": ir.FromInt(2)}),
	}), txID)
	if err == nil {
		t.Fatal("a patch spanning two mounts was accepted into a transaction it cannot join")
	}
	if code := api.ErrorCode(err); code != api.ErrCodeInvalidTx {
		t.Errorf("refused with %q, want %q: %s", code, api.ErrCodeInvalidTx, err)
	}
	if !strings.Contains(err.Error(), "one patch per mount") {
		t.Errorf("the refusal does not say what to do instead: %s", err)
	}

	// The store is untouched: the refusal happens before anything is written. An absent
	// path reads as null rather than as an error.
	got, err := client.Match(ctx, "verse.a.x")
	if err == nil && got != nil && got.Type != ir.NullType {
		t.Errorf("the refused patch wrote anyway: %v", got)
	}
}
