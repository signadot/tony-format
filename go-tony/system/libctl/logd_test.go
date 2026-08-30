package libctl

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	docdserver "github.com/signadot/tony-format/go-tony/system/docd/server"
	logdapi "github.com/signadot/tony-format/go-tony/system/logd/api"
	logdserver "github.com/signadot/tony-format/go-tony/system/logd/server"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

func startLogd(t *testing.T) *logdserver.Server {
	t.Helper()
	tmpDir := t.TempDir()
	store, err := storage.Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srv := logdserver.New(&logdserver.Spec{Storage: store})
	if err := srv.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start logd: %v", err)
	}
	t.Cleanup(func() { srv.StopTCP() })

	return srv
}

func startDocd(t *testing.T) *docdserver.Server {
	t.Helper()
	srv := docdserver.New(&docdserver.Spec{})
	if err := srv.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start docd: %v", err)
	}
	t.Cleanup(func() { srv.StopTCP() })
	return srv
}

func TestLogdSession_Connect(t *testing.T) {
	srv := startLogd(t)

	session := NewLogdSession(&LogdSessionConfig{
		Addr:     srv.TCPAddr(),
		ClientID: "test-client",
	})
	defer session.Close()

	ctx := context.Background()
	if err := session.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if !session.Connected() {
		t.Error("expected Connected() to return true")
	}
	if session.ServerID() == "" {
		t.Error("expected ServerID to be set")
	}
}

func TestLogdSession_Match(t *testing.T) {
	srv := startLogd(t)

	session := NewLogdSession(&LogdSessionConfig{
		Addr:     srv.TCPAddr(),
		ClientID: "test-client",
	})
	defer session.Close()

	ctx := context.Background()

	// A store where nothing has been written has nothing at any path, the empty one
	// included: a read answers null where a null was WRITTEN, and there is no such null
	// here (bymhrqz7h12ksas3jhn0). This used to answer null, which made a fresh store
	// and a root somebody wrote null to the same answer.
	if _, err := session.Match(ctx, ""); logdapi.ErrorCode(err) != logdapi.ErrCodeNotFound {
		t.Fatalf("match on an empty store: %v, want not_found", err)
	}

	// And once something is written, the root reads it.
	if _, err := session.Patch(ctx, "a", ir.FromInt(1)); err != nil {
		t.Fatalf("patch: %v", err)
	}
	result, err := session.Match(ctx, "")
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	if result == nil || result.Type != ir.ObjectType {
		t.Errorf("expected the written document, got %v", result)
	}
}

func TestLogdSession_Patch(t *testing.T) {
	srv := startLogd(t)

	session := NewLogdSession(&LogdSessionConfig{
		Addr:     srv.TCPAddr(),
		ClientID: "test-client",
	})
	defer session.Close()

	ctx := context.Background()

	// Patch some data
	data := ir.FromMap(map[string]*ir.Node{
		"name": ir.FromString("test"),
	})
	if _, err := session.Patch(ctx, "users/1", data); err != nil {
		t.Fatalf("Patch failed: %v", err)
	}

	// Match to verify
	result, err := session.Match(ctx, "users/1")
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	if result.Type != ir.ObjectType {
		t.Fatalf("expected object, got %v", result.Type)
	}

	// Check the value
	nameNode, err := result.GetPath("$.name")
	if err != nil {
		t.Fatalf("GetPath failed: %v", err)
	}
	if nameNode == nil || nameNode.String != "test" {
		t.Errorf("expected name='test', got %v", nameNode)
	}
}

func TestLogdSession_Transaction(t *testing.T) {
	srv := startLogd(t)
	// Two sessions: a multi-participant tx's participants must be on distinct
	// connections, since logd dispatches one connection's requests sequentially
	// and a participant patch blocks until the whole tx commits.
	a := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "txA"})
	defer a.Close()
	b := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "txB"})
	defer b.Close()

	ctx := context.Background()
	txID, err := a.NewTx(ctx, 2)
	if err != nil {
		t.Fatalf("NewTx failed: %v", err)
	}

	// Both participants join by writing with the tx id; each blocks until the
	// tx commits atomically.
	type joined struct {
		res *logdapi.PatchResult
		err error
	}
	resc := make(chan joined, 2)
	join := func(s *LogdSession, path string, v int64) {
		res, err := s.PatchTx(ctx, path, ir.FromMap(map[string]*ir.Node{"v": ir.FromInt(v)}), txID)
		resc <- joined{res, err}
	}
	go join(a, "a/1", 1)
	go join(b, "b/1", 2)

	// The write committed atomically, so every participant is told the same
	// commit — its own, and the transaction's.
	var commit int64
	for i := 0; i < 2; i++ {
		got := <-resc
		if got.err != nil {
			t.Fatalf("PatchTx failed: %v", got.err)
		}
		if got.res.Commit == 0 {
			t.Fatalf("PatchTx reported no commit")
		}
		if commit == 0 {
			commit = got.res.Commit
		} else if got.res.Commit != commit {
			t.Fatalf("participants disagree on commit: %d vs %d", commit, got.res.Commit)
		}
	}

	// Both writes are visible (committed together).
	for path, want := range map[string]int64{"a/1": 1, "b/1": 2} {
		res, err := a.Match(ctx, path)
		if err != nil {
			t.Fatalf("Match %s failed: %v", path, err)
		}
		v, err := res.GetPath("$.v")
		if err != nil || v == nil || v.Int64 == nil || *v.Int64 != want {
			t.Errorf("%s: expected v=%d, got %v (err %v)", path, want, v, err)
		}
	}
}

func TestLogdSession_CAS(t *testing.T) {
	srv := startLogd(t)
	s := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "cas"})
	defer s.Close()
	ctx := context.Background()

	vNode := func(n int64) *ir.Node { return ir.FromMap(map[string]*ir.Node{"v": ir.FromInt(n)}) }
	assertV := func(path string, want int64) {
		t.Helper()
		res, err := s.Match(ctx, path)
		if err != nil {
			t.Fatalf("match %s: %v", path, err)
		}
		v, err := res.GetPath("$.v")
		if err != nil || v == nil || v.Int64 == nil || *v.Int64 != want {
			t.Errorf("%s: expected v=%d, got %v (err %v)", path, want, v, err)
		}
	}

	// Seed.
	if _, err := s.Patch(ctx, "users/1", vNode(1)); err != nil {
		t.Fatalf("seed Patch: %v", err)
	}

	matchV1 := &logdapi.PathData{Path: "users/1", Data: vNode(1)}

	// CAS succeeds while the precondition (v==1) holds.
	if _, err := s.PatchIf(ctx, "users/1", vNode(2), matchV1); err != nil {
		t.Fatalf("expected CAS to succeed: %v", err)
	}
	assertV("users/1", 2)

	// CAS fails once the precondition no longer holds (v is now 2).
	if _, err := s.PatchIf(ctx, "users/1", vNode(3), matchV1); !errors.Is(err, ErrMatchFailed) {
		t.Fatalf("expected ErrMatchFailed, got %v", err)
	}
	assertV("users/1", 2) // unchanged

	// Match path independent of patch path: gate a write to a/1 on b/1's state.
	if _, err := s.Patch(ctx, "b/1", vNode(7)); err != nil {
		t.Fatalf("seed b/1: %v", err)
	}
	if _, err := s.PatchIf(ctx, "a/1", vNode(1), &logdapi.PathData{Path: "b/1", Data: vNode(7)}); err != nil {
		t.Fatalf("cross-path CAS should succeed: %v", err)
	}
	assertV("a/1", 1)
}

func TestLogdSession_Reconnect(t *testing.T) {
	srv := startLogd(t)

	session := NewLogdSession(&LogdSessionConfig{
		Addr:     srv.TCPAddr(),
		ClientID: "test-client",
	})
	defer session.Close()

	ctx := context.Background()

	// First connect
	if err := session.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Ping, not a read: the question is whether the session works, and ping answers it
	// from memory without asking about any path.
	if _, err := session.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}

	// Simulate connection break by closing internally
	session.mu.Lock()
	session.disconnect()
	session.mu.Unlock()

	// Next operation should reconnect
	if _, err := session.Ping(ctx); err != nil {
		t.Fatalf("Ping after reconnect failed: %v", err)
	}

	if !session.Connected() {
		t.Error("expected Connected() to return true after reconnect")
	}
}

func TestLogdSession_ConnectionRefused(t *testing.T) {
	session := NewLogdSession(&LogdSessionConfig{
		Addr:     "127.0.0.1:1", // Port 1 is unlikely to be listening
		ClientID: "test-client",
	})
	defer session.Close()

	// Use a context with timeout to avoid long waits
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := session.Connect(ctx)
	if err == nil {
		t.Error("expected connection error")
	}
	// Should be context deadline exceeded due to retry loop
	if err != context.DeadlineExceeded {
		t.Logf("Got error: %v", err)
	}
}

func TestLogdSession_MountClientIntegration(t *testing.T) {
	// Start both docd and logd
	logdSrv := startLogd(t)
	docdSrv := startDocd(t)

	// Mount with logd address
	client, err := Mount(&MountConfig{
		DocdAddr:   docdSrv.TCPAddr(),
		LogdAddr:   logdSrv.TCPAddr(),
		Controller: "test-ctrl",
		Path:       "/users",
	})
	if err != nil {
		t.Fatalf("Mount failed: %v", err)
	}
	defer client.Close()

	// Verify logd session exists
	logd := client.Logd()
	if logd == nil {
		t.Fatal("expected Logd() to return session")
	}

	// Use logd session
	ctx := context.Background()
	data := ir.FromMap(map[string]*ir.Node{
		"id":   ir.FromString("1"),
		"name": ir.FromString("Alice"),
	})
	if _, err := logd.Patch(ctx, "users/1", data); err != nil {
		t.Fatalf("Patch via MountClient.Logd() failed: %v", err)
	}

	// Verify data
	result, err := logd.Match(ctx, "users/1")
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}
	nameNode, err := result.GetPath("$.name")
	if err != nil {
		t.Fatalf("GetPath failed: %v", err)
	}
	if nameNode.String != "Alice" {
		t.Errorf("expected name='Alice', got %v", nameNode.String)
	}
}

func TestLogdSession_Scope(t *testing.T) {
	srv := startLogd(t)
	ctx := context.Background()

	baseline := NewLogdSession(&LogdSessionConfig{
		Addr:     srv.TCPAddr(),
		ClientID: "baseline",
	})
	defer baseline.Close()

	scoped := NewLogdSession(&LogdSessionConfig{
		Addr:     srv.TCPAddr(),
		ClientID: "scoped",
		Scope:    "sandbox1",
	})
	defer scoped.Close()

	if baseline.Scope() != "" {
		t.Errorf("baseline Scope() = %q, want empty", baseline.Scope())
	}
	if scoped.Scope() != "sandbox1" {
		t.Errorf("scoped Scope() = %q, want %q", scoped.Scope(), "sandbox1")
	}

	// Baseline writes two records.
	if _, err := baseline.Patch(ctx, "users/alice", ir.FromMap(map[string]*ir.Node{
		"name": ir.FromString("Alice"),
	})); err != nil {
		t.Fatalf("baseline Patch alice failed: %v", err)
	}
	if _, err := baseline.Patch(ctx, "users/bob", ir.FromMap(map[string]*ir.Node{
		"name": ir.FromString("Bob"),
	})); err != nil {
		t.Fatalf("baseline Patch bob failed: %v", err)
	}

	// Scope overrides alice only.
	if _, err := scoped.Patch(ctx, "users/alice", ir.FromMap(map[string]*ir.Node{
		"name": ir.FromString("Alice in Sandbox"),
	})); err != nil {
		t.Fatalf("scoped Patch alice failed: %v", err)
	}

	matchName := func(t *testing.T, s *LogdSession, path string) string {
		t.Helper()
		result, err := s.Match(ctx, path)
		if err != nil {
			t.Fatalf("Match %q failed: %v", path, err)
		}
		nameNode, err := result.GetPath("$.name")
		if err != nil {
			t.Fatalf("GetPath on %q failed: %v", path, err)
		}
		if nameNode == nil {
			return ""
		}
		return nameNode.String
	}

	// Isolation: baseline still sees the baseline value.
	if got := matchName(t, baseline, "users/alice"); got != "Alice" {
		t.Errorf("baseline alice = %q, want %q", got, "Alice")
	}
	// Scope sees its own override.
	if got := matchName(t, scoped, "users/alice"); got != "Alice in Sandbox" {
		t.Errorf("scoped alice = %q, want %q", got, "Alice in Sandbox")
	}
	// COW: scope sees baseline data for paths it hasn't modified.
	if got := matchName(t, scoped, "users/bob"); got != "Bob" {
		t.Errorf("scoped bob = %q, want %q (COW from baseline)", got, "Bob")
	}
}

func TestLogdSession_DeleteScope(t *testing.T) {
	srv := startLogd(t)
	ctx := context.Background()

	scoped := NewLogdSession(&LogdSessionConfig{
		Addr:     srv.TCPAddr(),
		ClientID: "scoped",
		Scope:    "to-delete",
	})
	defer scoped.Close()

	if _, err := scoped.Patch(ctx, "data", ir.FromString("scoped")); err != nil {
		t.Fatalf("scoped Patch failed: %v", err)
	}

	// A scoped session cannot delete scopes.
	if err := scoped.DeleteScope(ctx, "to-delete"); err == nil {
		t.Error("expected DeleteScope from a scoped session to fail")
	}

	// A baseline session can.
	baseline := NewLogdSession(&LogdSessionConfig{
		Addr:     srv.TCPAddr(),
		ClientID: "baseline",
	})
	defer baseline.Close()

	if err := baseline.DeleteScope(ctx, "to-delete"); err != nil {
		t.Fatalf("baseline DeleteScope failed: %v", err)
	}
}

// newLogdOn starts a logd server on an existing store, so a test can reach the storage
// directly (to compact, inspect the replay floor, and so on) while the client talks to it
// over TCP. startLogd owns its store; this one does not.
func newLogdOn(t *testing.T, store *storage.Storage) *logdserver.Server {
	t.Helper()
	srv := logdserver.New(&logdserver.Spec{Storage: store})
	if err := srv.StartTCP("127.0.0.1:0"); err != nil {
		t.Fatalf("failed to start logd: %v", err)
	}
	t.Cleanup(func() { srv.StopTCP() })
	return srv
}
