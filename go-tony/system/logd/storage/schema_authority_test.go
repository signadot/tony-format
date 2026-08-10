package storage

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Keying is decided by the PERSISTED active schema, with the configured resolver as
// bootstrap only. Before this, live keying came from the config file -- which is never in
// the log, changes under a restart without the store knowing, and bypasses the migration
// machinery that exists for exactly that kind of change -- while the pending dual-write
// index was already keyed from the persisted pending schema. Two sources, one commit path.

// schemaNode builds a schema document declaring an auto-id field, the only thing api.Schema
// can express today.
func schemaNode(t *testing.T, arrayPath, field string) string {
	t.Helper()
	// define: { <array>: { <field>: !logd-auto-id } } -- walkDefine takes the PARENT path
	// as the keyed array and the tagged field name as its key.
	return `{define: {` + arrayPath + `: {` + field + `: !logd-auto-id null}}}`
}

func TestSchemaAuthority_BootstrapsFromConfigWhenStoreHasNone(t *testing.T) {
	s := openTestStorage(t)
	s.SetSchemaResolver(&api.StaticSchemaResolver{Schema: &api.Schema{
		AutoIDFields: []api.AutoIDField{{Path: "items", Field: "id"}},
	}})

	mustCommit(t, s, nil, `{items: [{q: 1}]}`)

	paths := indexPathSet(s)
	t.Logf("paths: %v", paths)
	if !hasKeyedPath(paths, "items(") {
		t.Errorf("a store with no schema of its own should still key from configuration; got %v", paths)
	}
}

func TestSchemaAuthority_PersistedWinsOverConfig(t *testing.T) {
	s := openTestStorage(t)

	// Configuration says one thing...
	s.SetSchemaResolver(&api.StaticSchemaResolver{Schema: &api.Schema{
		AutoIDFields: []api.AutoIDField{{Path: "items", Field: "fromConfig"}},
	}})
	// ...and the store has been told another, through the migration path.
	node := mustParseBody(t, schemaNode(t, "items", "fromStore"))
	if _, err := s.StartMigration(node); err != nil {
		t.Fatalf("StartMigration: %v", err)
	}
	if _, err := s.CompleteMigration(); err != nil {
		t.Fatalf("CompleteMigration: %v", err)
	}

	got := s.schemaForScope(nil)
	if got == nil {
		t.Fatal("no schema after CompleteMigration")
	}
	if f := got.LookupKeyField("items"); f != "fromStore" {
		t.Errorf("keying used %q; the persisted schema says %q and it is the authority",
			f, "fromStore")
	}

	// And a scope keys the same way, since the persisted schema is per-store.
	scope := "s1"
	if f := s.schemaForScope(&scope).LookupKeyField("items"); f != "fromStore" {
		t.Errorf("scope keyed by %q, want the store's %q", f, "fromStore")
	}
}

func TestSchemaAuthority_SurvivesRestart(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	node := mustParseBody(t, schemaNode(t, "items", "fromStore"))
	if _, err := s.StartMigration(node); err != nil {
		t.Fatalf("StartMigration: %v", err)
	}
	if _, err := s.CompleteMigration(); err != nil {
		t.Fatalf("CompleteMigration: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopened with NO configuration at all: the schema is the store's own.
	re, err := Open(root, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer re.Close()
	got := re.schemaForScope(nil)
	if got == nil {
		t.Fatal("the persisted schema did not survive the restart")
	}
	if f := got.LookupKeyField("items"); f != "fromStore" {
		t.Errorf("after restart keying used %q, want %q", f, "fromStore")
	}
}

func hasKeyedPath(paths []string, prefix string) bool {
	for _, p := range paths {
		if len(p) >= len(prefix) && p[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
