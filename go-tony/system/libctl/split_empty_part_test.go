package libctl

import (
	"context"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
)

// An empty object at a mount is nothing to write, so the mount is not a participant
// of the split write, as the base remainder is not one when it is empty. docd handed
// it on as a part with no data; logd refused that participant, and the other waited
// for a transaction that could not commit (4ynqp7wqh12krg32msn0 item 8).
func TestSplitWrite_EmptyPartIsNotAParticipant(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "org.users", newWatchableLogdController(t, logd.TCPAddr(), "cU"))
	runController(t, docd, "org.audit", newWatchableLogdController(t, logd.TCPAddr(), "cA"))

	client := docdClient(t, docd, "client")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := client.Patch(ctx, "org", mustParseLibctl(t, `{users: {}, audit: {e: 2}}`)); err != nil {
		t.Fatalf("a split write with an empty part: %v", err)
	}
	res, err := client.Match(ctx, "org.audit")
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if v, _ := res.GetPath("$.e"); v == nil || v.Int64 == nil || *v.Int64 != 2 {
		t.Errorf("org.audit = %v, want {e: 2}", res)
	}

	// Every part empty: nothing to split, and the write is the no-op it is to logd.
	if _, err := client.Patch(ctx, "org", mustParseLibctl(t, `{users: {}, audit: {}}`)); err != nil {
		t.Fatalf("a write of empty parts: %v", err)
	}
	// An empty part beside a base write: one participant, the base.
	if _, err := client.Patch(ctx, "org", mustParseLibctl(t, `{users: {}, note: 1}`)); err != nil {
		t.Fatalf("an empty part beside a base write: %v", err)
	}
	base, err := client.Match(ctx, "org.note")
	if err != nil || base == nil || base.Type != ir.NumberType {
		t.Errorf("org.note = %v (%v), want 1", base, err)
	}
}
