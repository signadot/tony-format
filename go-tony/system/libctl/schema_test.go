package libctl

import (
	"context"
	"errors"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Setting the schema and reading it back, over the client library rather than by
// hand-writing the session protocol. Every one of these operations was on the wire and
// implemented by the server, and this package could not send any of them.
func TestSchemaFromTheClient(t *testing.T) {
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
	if st.Schema != nil || st.Commit != 0 {
		t.Errorf("a fresh store reports a schema: %+v", st)
	}

	// Set one: it is a commit, and the schema from that commit on.
	schema, err := parse.Parse([]byte(`{define: {items: {sku: !logd-key null}}}`))
	if err != nil {
		t.Fatal(err)
	}
	commit, err := s.SetSchema(ctx, schema, false)
	if err != nil {
		t.Fatalf("set schema: %s", err)
	}
	if commit == 0 {
		t.Fatal("setting a schema answered no commit")
	}
	st, err = s.Schema(ctx)
	if err != nil {
		t.Fatalf("schema after set: %s", err)
	}
	if st.Schema == nil || st.Commit != commit {
		t.Fatalf("after setting a schema at %d the store says %+v", commit, st)
	}
	if before, err := s.SchemaAt(ctx, commit-1); err != nil {
		t.Fatalf("schema at %d: %s", commit-1, err)
	} else if before.Schema != nil {
		t.Errorf("the commit before the schema commit has a schema: %+v", before)
	}

	// One the store cannot adopt is refused with its own code, and nothing changes.
	bad, err := parse.Parse([]byte(`{define: {items: {sku: !logd-key null, id: !logd-auto-id null}}}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SetSchema(ctx, bad, false)
	var se *api.SessionError
	if !errors.As(err, &se) || se.Code != api.ErrCodeSchemaRefused {
		t.Fatalf("a schema with two identities for one array was answered %v, want %s", err, api.ErrCodeSchemaRefused)
	}
	st, err = s.Schema(ctx)
	if err != nil {
		t.Fatalf("schema after the refusal: %s", err)
	}
	if st.Commit != commit {
		t.Errorf("a refused schema changed the store: %+v", st)
	}
}
