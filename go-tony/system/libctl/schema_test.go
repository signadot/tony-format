package libctl

import (
	"context"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// Setting a schema, testing it before it is live, and completing the migration -- over the
// client library rather than by hand-writing the session protocol. Every one of these
// operations was on the wire and implemented by the server, and this package could not send
// any of them.
func TestSchemaAndMigrationFromTheClient(t *testing.T) {
	srv := startLogd(t)
	ctx := context.Background()
	s := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "schema"})
	defer s.Close()
	if err := s.Connect(ctx); err != nil {
		t.Fatalf("connect: %s", err)
	}

	// A store with no schema says so.
	st, err := s.Schema(ctx)
	if err != nil {
		t.Fatalf("schema: %s", err)
	}
	if st.Migrating() {
		t.Errorf("a fresh store reports a migration in progress: %+v", st)
	}

	// Propose one: it becomes pending, not active.
	schema := ir.FromMap(map[string]*ir.Node{
		"verse": ir.FromMap(map[string]*ir.Node{"entities": ir.FromMap(map[string]*ir.Node{})}),
	})
	if _, err := s.SetSchema(ctx, schema); err != nil {
		t.Fatalf("set schema: %s", err)
	}
	st, err = s.Schema(ctx)
	if err != nil {
		t.Fatalf("schema after set: %s", err)
	}
	if !st.Migrating() {
		t.Fatalf("after setting a schema nothing is pending: %+v", st)
	}

	// A session may work against the pending schema before it is live.
	pending := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "pending", UsePending: true})
	defer pending.Close()
	if err := pending.Connect(ctx); err != nil {
		t.Fatalf("connect against the pending schema: %s", err)
	}

	// Complete it, and the pending one becomes active.
	if _, err := s.CompleteMigration(ctx); err != nil {
		t.Fatalf("complete migration: %s", err)
	}
	st, err = s.Schema(ctx)
	if err != nil {
		t.Fatalf("schema after completing: %s", err)
	}
	if st.Migrating() {
		t.Errorf("a migration is still pending after completing it: %+v", st)
	}
	if st.Active == nil {
		t.Error("no active schema after completing the migration")
	}
}

// And a migration can be abandoned.
func TestAbortMigrationFromTheClient(t *testing.T) {
	srv := startLogd(t)
	ctx := context.Background()
	s := NewLogdSession(&LogdSessionConfig{Addr: srv.TCPAddr(), ClientID: "abort"})
	defer s.Close()
	if err := s.Connect(ctx); err != nil {
		t.Fatalf("connect: %s", err)
	}
	if _, err := s.SetSchema(ctx, ir.FromMap(map[string]*ir.Node{"a": ir.FromMap(map[string]*ir.Node{})})); err != nil {
		t.Fatalf("set schema: %s", err)
	}
	if _, err := s.AbortMigration(ctx); err != nil {
		t.Fatalf("abort: %s", err)
	}
	st, err := s.Schema(ctx)
	if err != nil {
		t.Fatalf("schema: %s", err)
	}
	if st.Migrating() {
		t.Errorf("the abandoned migration is still pending: %+v", st)
	}
	if st.Active != nil {
		t.Errorf("an abandoned migration left an active schema: %v", st.Active)
	}
}
