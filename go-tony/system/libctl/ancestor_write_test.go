package libctl

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// A write above a mount whose content lies in that mount belongs to the mount's
// controller. docd split the patch, found one part, and routed the write "normally" by
// the client's path -- which no mount is a prefix of -- so it went to logd base: the
// controller never saw it, and a read at the mount (served by the controller) did not
// show it (05d8w3cjh12kswb1msn0, item 7).
func TestDocd_AncestorWriteReachesTheMount(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())

	mem := newMemController() // content lives in the controller, not in logd
	runController(t, docd, "org.users", mem)

	client := docdClient(t, docd, "client")
	ctx := context.Background()
	if _, err := client.Patch(ctx, "org", mustParseLibctl(t, `{users: {alice: {v: 1}}}`)); err != nil {
		t.Fatalf("write above the mount: %v", err)
	}

	mem.mu.Lock()
	got := mem.data["org.users"]
	mem.mu.Unlock()
	if v, _ := got.GetPath("$.alice.v"); got == nil || v == nil || v.Int64 == nil || *v.Int64 != 1 {
		t.Errorf("the controller at org.users holds %v, want {alice: {v: 1}}", got)
	}

	res, err := client.Match(ctx, "org.users")
	if err != nil {
		t.Fatalf("read at the mount: %v", err)
	}
	if v, _ := res.GetPath("$.alice.v"); v == nil || v.Int64 == nil || *v.Int64 != 1 {
		t.Errorf("read at the mount through docd: %v, want alice.v = 1", res)
	}

	// And nothing landed in logd base under the mount.
	base := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "base-reader"})
	t.Cleanup(func() { base.Close() })
	if inBase := baseValue(t, base, "org.users"); inBase != nil {
		t.Errorf("logd base holds %v at org.users; the write bypassed the controller", inBase)
	}
}

// baseValue reads path straight from logd: the value there, or nil when it holds
// nothing.
func baseValue(t *testing.T, base *LogdSession, path string) *ir.Node {
	t.Helper()
	v, err := base.Match(context.Background(), path)
	var se *api.SessionError
	if errors.As(err, &se) && se.Code == api.ErrCodeNotFound {
		return nil
	}
	if err != nil {
		t.Fatalf("read logd base at %s: %v", path, err)
	}
	if v == nil || v.Type == ir.NullType {
		return nil
	}
	return v
}

// A write above a tombstoned mount whose content lies in that mount is refused, as a
// write at the mount is: the content lived in the controller, and falling through to
// logd base is exactly what the tombstone exists to prevent (docd/server/doc.go). A
// split write with a tombstoned part commits none of it.
func TestDocd_AncestorWriteOverTombstoneIsRefused(t *testing.T) {
	logd := startLogd(t)
	docd := startDocdRouting(t, logd.TCPAddr())

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = RunController(ctx, &ControllerConfig{
			DocdAddr: docd.TCPAddr(), Controller: "users", Path: "org.users", Handler: newMemController(),
		})
	}()
	waitMount(t, docd, "org.users")
	runController(t, docd, "org.audit", newLogdController(t, logd.TCPAddr(), "audit"))
	cancel() // crash the users controller
	waitTombstone(t, docd, "org.users")

	client := docdClient(t, docd, "client")
	bg := context.Background()
	for _, tc := range []struct{ name, patch string }{
		{"the mount alone", `{users: {alice: {v: 1}}}`},
		{"the mount beside a live one", `{users: {alice: {v: 1}}, audit: {e: 1}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wctx, wcancel := context.WithTimeout(bg, 3*time.Second)
			defer wcancel()
			_, err := client.Patch(wctx, "org", mustParseLibctl(t, tc.patch))
			var se *api.SessionError
			if !errors.As(err, &se) || se.Code != api.ErrCodeUnavailable {
				t.Fatalf("write over the tombstone: err = %v, want unavailable", err)
			}
		})
	}

	base := NewLogdSession(&LogdSessionConfig{Addr: logd.TCPAddr(), ClientID: "base-reader"})
	t.Cleanup(func() { base.Close() })
	for _, p := range []string{"org.users", "org.audit"} {
		if inBase := baseValue(t, base, p); inBase != nil {
			t.Errorf("logd base holds %v at %s after a refused write", inBase, p)
		}
	}
}
