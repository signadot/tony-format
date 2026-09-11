package libctl

import (
	"context"
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// The schema, from the client side.
//
// These operations were on the wire, implemented by the server, and named in the protocol
// documentation, and this package could not send them: a caller wanting to set a schema had
// to write the session protocol by hand. Advertising an operation nothing can invoke is the
// same as not having it.

// SchemaState is what a store says about its schema: the schema, and the commit that set
// it -- 0, and a nil schema, for a store that has none.
type SchemaState struct {
	Schema *ir.Node
	Commit int64
}

// Schema answers the store's schema, the one in force now.
func (s *LogdSession) Schema(ctx context.Context) (*SchemaState, error) {
	return s.schemaGet(ctx, &api.SchemaGetRequest{})
}

// SchemaAt answers the schema in force at commit: the last one set at or before it.
func (s *LogdSession) SchemaAt(ctx context.Context, commit int64) (*SchemaState, error) {
	return s.schemaGet(ctx, &api.SchemaGetRequest{At: &commit})
}

func (s *LogdSession) schemaGet(ctx context.Context, get *api.SchemaGetRequest) (*SchemaState, error) {
	resp, err := s.request(ctx, &api.SessionRequest{Schema: &api.SchemaRequest{Get: get}})
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("schema: %w", resp.Error)
	}
	if resp.Result == nil || resp.Result.Schema == nil {
		return nil, fmt.Errorf("unexpected response: no schema result")
	}
	return &SchemaState{Schema: resp.Result.Schema.Schema, Commit: resp.Result.Schema.Commit}, nil
}

// SetSchema sets the store's schema, as one commit, and answers it: every write after
// that commit is lowered under the new schema. A schema the store cannot adopt is refused
// with api.ErrCodeSchemaRefused (see api.SchemaSetRequest); force lets an array lose its
// identity.
//
// Only a baseline session may set a schema; a scoped one is refused.
func (s *LogdSession) SetSchema(ctx context.Context, schema *ir.Node, force bool) (commit int64, err error) {
	resp, err := s.request(ctx, &api.SessionRequest{
		Schema: &api.SchemaRequest{Set: &api.SchemaSetRequest{Schema: schema, Force: force}},
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
	return resp.Result.Schema.Commit, nil
}
