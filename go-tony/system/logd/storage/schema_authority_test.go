package storage

import (
	"strings"
	"testing"
)

// Keying is decided by the store's own schema, which is in the log. A configured schema
// reaches the store once, as a commit, when the store has none (BootstrapSchema): the
// config file is not in the log, changes under a restart without the store knowing, and
// bypasses the checks a schema commit is held to. Before this the live keying came from
// the config file directly, and a file edited between restarts re-keyed the store.

// schemaNode builds a schema document declaring an auto-id field.
func schemaNode(t *testing.T, arrayPath, field string) string {
	t.Helper()
	// define: { <array>: { <field>: !logd-auto-id } } -- walkDefine takes the PARENT path
	// as the keyed array and the tagged field name as its key.
	return `{define: {` + arrayPath + `: {` + field + `: !logd-auto-id null}}}`
}

func TestSchemaAuthority_BootstrapsFromConfigWhenStoreHasNone(t *testing.T) {
	s := openTestStorage(t)
	if err := s.BootstrapSchema(mustParseBody(t, schemaNode(t, "items", "id"))); err != nil {
		t.Fatalf("BootstrapSchema: %v", err)
	}
	if _, at := s.GetActiveSchema(); at != 1 {
		t.Errorf("the configured schema was adopted at commit %d, want 1: it is a commit", at)
	}

	mustCommit(t, s, nil, `{items: [{q: 1}]}`)

	paths := indexPathSet(s)
	t.Logf("paths: %v", paths)
	if !hasKeyedPath(paths, `items."(id=`) {
		t.Errorf("a store with no schema of its own should key from the configured one; got %v", paths)
	}
}

func TestSchemaAuthority_TheSameConfigIsNothingToDo(t *testing.T) {
	s := openTestStorage(t)
	node := mustParseBody(t, schemaNode(t, "items", "id"))
	if err := s.BootstrapSchema(node); err != nil {
		t.Fatalf("BootstrapSchema: %v", err)
	}
	if err := s.BootstrapSchema(mustParseBody(t, schemaNode(t, "items", "id"))); err != nil {
		t.Fatalf("the same schema again: %v", err)
	}
	if head, _ := s.GetCurrentCommit(); head != 1 {
		t.Errorf("head is %d after bootstrapping the same schema twice, want 1: nothing to commit", head)
	}
}

func TestSchemaAuthority_AConfigThatDiffersIsRefused(t *testing.T) {
	s := openTestStorage(t)
	// The store has been told one thing, through a schema commit...
	if _, err := s.SetSchema(mustParseBody(t, schemaNode(t, "items", "fromStore")), false); err != nil {
		t.Fatalf("SetSchema: %v", err)
	}
	// ...and the configuration says another.
	err := s.BootstrapSchema(mustParseBody(t, schemaNode(t, "items", "fromConfig")))
	if err == nil {
		t.Fatal("a configured schema that differs from the store's was adopted")
	}
	if !strings.Contains(err.Error(), "not the configured one") {
		t.Errorf("the refusal does not say what is wrong: %v", err)
	}
	if f := strings.Join(s.schemaForScope(nil).Identity("items"), ","); f != "fromStore" {
		t.Errorf("keying used %q after the refusal; the store's schema says %q", f, "fromStore")
	}
	// And a scope keys the same way, since the schema is per-store.
	scope := "s1"
	if f := strings.Join(s.schemaForScope(&scope).Identity("items"), ","); f != "fromStore" {
		t.Errorf("scope keyed by %q, want the store's %q", f, "fromStore")
	}
}

func TestSchemaAuthority_SurvivesRestart(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.SetSchema(mustParseBody(t, schemaNode(t, "items", "fromStore")), false); err != nil {
		t.Fatalf("SetSchema: %v", err)
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
	if f := strings.Join(got.Identity("items"), ","); f != "fromStore" {
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
