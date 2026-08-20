package libctl

import (
	"context"
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// The schema and its migrations, from the client side.
//
// These operations were on the wire, implemented by the server, and named in the protocol
// documentation, and this package could not send them: a caller wanting to set a schema had
// to write the session protocol by hand. Advertising an operation nothing can invoke is the
// same as not having it.

// SchemaState is what a store says about its schema: the active one, and a pending one when
// a migration is in progress.
type SchemaState struct {
	Active        *ir.Node
	ActiveCommit  int64
	Pending       *ir.Node
	PendingCommit int64
}

// Migrating reports whether a migration is in progress -- a pending schema is one which has
// been proposed and not yet completed or abandoned.
func (s *SchemaState) Migrating() bool { return s != nil && s.Pending != nil }

// Schema answers the store's schema state.
func (s *LogdSession) Schema(ctx context.Context) (*SchemaState, error) {
	resp, err := s.request(ctx, &api.SessionRequest{
		Schema: &api.SchemaRequest{Get: &api.SchemaGetRequest{}},
	})
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("schema: %w", resp.Error)
	}
	if resp.Result == nil || resp.Result.Schema == nil {
		return nil, fmt.Errorf("unexpected response: no schema result")
	}
	r := resp.Result.Schema
	return &SchemaState{
		Active:        r.Active,
		ActiveCommit:  r.ActiveCommit,
		Pending:       r.Pending,
		PendingCommit: r.PendingCommit,
	}, nil
}

// SetSchema proposes a schema, which starts a migration: the schema becomes PENDING at the
// returned commit, and stays pending until CompleteMigration or AbortMigration. A session
// which wants to read and write against the pending schema before it is completed says so
// at hello (LogdSessionConfig.UsePending).
//
// Only a baseline session may set a schema; a scoped one is refused.
func (s *LogdSession) SetSchema(ctx context.Context, schema *ir.Node) (commit int64, err error) {
	resp, err := s.request(ctx, &api.SessionRequest{
		Schema: &api.SchemaRequest{Set: &api.SchemaSetRequest{Schema: schema}},
	})
	if err != nil {
		return 0, err
	}
	if resp.Error != nil {
		return 0, fmt.Errorf("set schema: %w", resp.Error)
	}
	if resp.Result == nil || resp.Result.Schema == nil {
		return 0, fmt.Errorf("unexpected response: no schema result")
	}
	return resp.Result.Schema.PendingCommit, nil
}

// CompleteMigration makes the pending schema the active one.
func (s *LogdSession) CompleteMigration(ctx context.Context) (commit int64, err error) {
	return s.migrate(ctx, api.MigrationComplete)
}

// AbortMigration discards the pending schema, leaving the active one as it was.
func (s *LogdSession) AbortMigration(ctx context.Context) (commit int64, err error) {
	return s.migrate(ctx, api.MigrationAbort)
}

func (s *LogdSession) migrate(ctx context.Context, action api.MigrationAction) (int64, error) {
	resp, err := s.request(ctx, &api.SessionRequest{Migration: &action})
	if err != nil {
		return 0, err
	}
	if resp.Error != nil {
		return 0, fmt.Errorf("migration %s: %w", action, resp.Error)
	}
	if resp.Result == nil || resp.Result.Migration == nil {
		return 0, fmt.Errorf("unexpected response: no migration result")
	}
	return resp.Result.Migration.Commit, nil
}
