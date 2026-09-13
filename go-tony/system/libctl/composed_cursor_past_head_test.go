package libctl

import (
	"context"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A composed watch from an absolute commit past the head is refused before it is
// confirmed, as logd refuses a single-route one (p478tacqh12krg32msn0 item 11).
func TestComposedWatchFromACommitPastTheHeadIsRefused(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())
	runController(t, docd, "org.users", newMemController())
	client := docdClient(t, docd, "client")
	ctx := context.Background()
	if _, err := client.Patch(ctx, "org.x", ir.FromInt(1)); err != nil {
		t.Fatal(err)
	}

	from := int64(100)
	w, err := client.Watch(ctx, "org", &WatchOptions{FromCommit: &from, WaitIfAbsent: true})
	if err == nil {
		w.Close()
		t.Fatal("a composed watch from commit 100 was confirmed")
	}
	if !strings.Contains(err.Error(), api.ErrCodeCommitNotFound) {
		t.Errorf("refused with %v, want %s", err, api.ErrCodeCommitNotFound)
	}
}
