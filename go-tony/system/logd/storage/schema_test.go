package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Helper to create a simple schema node for testing
func testSchema(t *testing.T, yaml string) *ir.Node {
	t.Helper()
	node, err := parse.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}
	return node
}

// migrateTo sets the schema, answering the commit that set it.
func migrateTo(t *testing.T, s *Storage, schema string) int64 {
	t.Helper()
	commit, err := s.SetSchema(testSchema(t, schema), false)
	if err != nil {
		t.Fatalf("SetSchema(%s): %v", schema, err)
	}
	return commit
}

// A schema change is a commit: it takes the next number in the one sequence, the schema
// is in force from it, and the commits before it were under the schema before.
func TestSchema_SetIsACommit(t *testing.T) {
	s := openTestStorage(t)
	if schema, at := s.GetActiveSchema(); schema != nil || at != 0 {
		t.Errorf("a fresh store has a schema %v at %d", schema != nil, at)
	}
	mustCommit(t, s, nil, `{initial: data}`)
	first := migrateTo(t, s, `{define: {items: {sku: !logd-key null}}}`)
	if first != 2 {
		t.Errorf("the schema commit is %d, want 2", first)
	}
	if head, _ := s.GetCurrentCommit(); head != first {
		t.Errorf("the head is %d after the schema commit %d", head, first)
	}
	if schema, at := s.GetActiveSchema(); schema == nil || at != first {
		t.Errorf("active schema %v at %d, want at %d", schema != nil, at, first)
	}
	// A write after it is lowered under it: the element is held under its name.
	mustCommit(t, s, nil, `{items: [{sku: A, q: 1}]}`)
	if paths := indexPathSet(s); !hasKeyedPath(paths, `items."(sku=A)"`) {
		t.Errorf("a write after the schema commit was not keyed: %v", paths)
	}
	second := migrateTo(t, s, `{define: {items: {sku: !logd-key null}, tags: {id: !logd-key null}}}`)
	for _, tc := range []struct{ at, want int64 }{{1, 0}, {first, first}, {first + 1, first}, {second, second}, {second + 5, second}} {
		if _, at := s.SchemaAt(tc.at); at != tc.want {
			t.Errorf("SchemaAt(%d) was set at %d, want %d", tc.at, at, tc.want)
		}
	}
	if h := s.SchemaHistory(); len(h) != 2 || h[0].Commit != first || h[1].Commit != second {
		t.Errorf("history %v, want commits %d and %d", h, first, second)
	}
}

// A schema the store cannot adopt is refused as the client's mistake, with nothing
// committed: one that cannot mean what it says, and one the data cannot follow.
func TestSchema_RefusalCommitsNothing(t *testing.T) {
	s := openTestStorage(t)
	mustCommit(t, s, nil, `{items: [{sku: A}]}`)
	for _, tc := range []struct{ name, schema, want string }{
		{"two identities", `{define: {items: {sku: !logd-key null, id: !logd-auto-id null}}}`, "one identity"},
		{"an element with no name", `{define: {items: {id: !logd-key null}}}`, "has no name"},
	} {
		_, err := s.SetSchema(testSchema(t, tc.schema), false)
		if err == nil {
			t.Errorf("%s: adopted", tc.name)
			continue
		}
		if !IsSchemaRefused(err) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	if head, _ := s.GetCurrentCommit(); head != 1 {
		t.Errorf("a refusal took a commit: head %d, want 1", head)
	}
	if schema, _ := s.GetActiveSchema(); schema != nil {
		t.Error("a refused schema became active")
	}
}

// Every commit survives every schema change, before a reopen and after it -- with the
// manifest, and rebuilt from the log without one. A second migration lost everything
// written before the first -- a: 1 and the scope's s: 1 below -- by installing an index
// backfilled only from the previous migration (4jw0pz1rh12ks9r8mhn0); a reopen after a
// write forgot the schema (faweqnhvh12ksynxmdn0).
func TestSchema_DataAndSchemaSurviveTwoChangesAndAReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	scope := "sc"
	first := mustCommit(t, s, nil, "{a: 1}")
	mustCommit(t, s, &scope, "{s: 1}")
	migrateTo(t, s, "{v1: .[string]}")
	mustCommit(t, s, nil, "{b: 2}")
	schemaAt := migrateTo(t, s, "{v2: .[string]}")
	mustCommit(t, s, nil, "{c: 3}")

	check := func(when string, s *Storage) {
		t.Helper()
		head, err := s.GetCurrentCommit()
		if err != nil {
			t.Fatalf("%s: GetCurrentCommit: %v", when, err)
		}
		for _, r := range []struct {
			at    int64
			scope *string
			want  string
		}{
			{head, nil, "a: 1 b: 2 c: 3"},
			{first, nil, "a: 1"},
			{head, &scope, "a: 1 b: 2 c: 3 s: 1"},
		} {
			if got := flatten(t, mustReadScope(t, s, r.at, r.scope)); got != r.want {
				t.Errorf("%s: at %d (scope %v): got %s, want %s", when, r.at, r.scope != nil, got, r.want)
			}
		}
		if schema, at := s.GetActiveSchema(); schema == nil || at != schemaAt {
			t.Errorf("%s: active schema %v at %d, want the second at %d", when, schema != nil, at, schemaAt)
		}
		if s.indexPersister.index != s.index {
			t.Errorf("%s: the persister persists an index that is not the store's", when)
		}
	}
	check("before the reopen", s)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s, err = Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	check("after the reopen", s)
	// And with no manifest: the history is rebuilt from the log's schema commits.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "index.manifest")); err != nil {
		t.Fatalf("remove index.manifest: %v", err)
	}
	s, err = Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen without the manifest: %v", err)
	}
	defer s.Close()
	check("rebuilt from the log", s)
}

// A commit that lands beside a schema change is kept, and is lowered under the schema
// of its side of it. Commits during a migration's snapshots were missing from the index
// the migration installed, acknowledged and then gone (gdpv3fsvh12ksynxmdn0).
func TestSchema_WritesAlongsideAChangeAreKept(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	mustCommit(t, s, nil, `{seed: 1}`)

	stop := make(chan struct{})
	done := make(chan struct{})
	var acked []string
	var writeErr error
	var n atomic.Int64 // writes acknowledged so far
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			key := fmt.Sprintf("k%d", i)
			txn, err := s.NewTx(1, nil)
			if err != nil {
				writeErr = err
				return
			}
			p, err := txn.NewPatcher(&api.Patch{PathData: api.PathData{Path: "w." + key, Data: ir.FromInt(int64(i))}})
			if err != nil {
				writeErr = err
				return
			}
			if res := p.Commit(); res.Committed {
				acked = append(acked, key)
				n.Add(1)
			} else {
				writeErr = res.Error
				return
			}
		}
	}()
	// Writes before the change, queued behind it, and after it.
	waitFor := func(k int64) {
		for n.Load() < k {
			select {
			case <-done:
				return
			case <-time.After(time.Millisecond):
			}
		}
	}
	waitFor(20)
	define := ""
	for i := 0; i < 5; i++ {
		define += fmt.Sprintf("items%d: {id: !logd-key null}, ", i)
		migrateTo(t, s, "{define: {"+define+"}}")
		waitFor(n.Load() + 5)
	}
	close(stop)
	<-done
	if writeErr != nil {
		t.Fatalf("a write alongside the schema change failed: %v", writeErr)
	}

	check := func(when string, s *Storage) {
		t.Helper()
		head, err := s.GetCurrentCommit()
		if err != nil {
			t.Fatalf("%s: GetCurrentCommit: %v", when, err)
		}
		w, _, err := readSubtreeAt(s, "w", head, nil)
		if err != nil {
			t.Fatalf("%s: read w: %v", when, err)
		}
		var missing []string
		for _, k := range acked {
			if getField(w, k) == nil {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			t.Errorf("%s: %d of %d acknowledged writes do not read back, e.g. %v", when, len(missing), len(acked), missing[:min(5, len(missing))])
		}
	}
	check("before the reopen", s)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	s, err = Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	check("after the reopen", s)
	t.Logf("%d writes alongside 5 schema changes", len(acked))
}

// Helper to get a field from an object node
func getField(n *ir.Node, field string) *ir.Node {
	if n == nil || n.Type != ir.ObjectType {
		return nil
	}
	for i, f := range n.Fields {
		if f.String == field {
			return n.Values[i]
		}
	}
	return nil
}

// A scope's statements are lowered under the identity their path had when they were
// made, and a schema change reaches every scope; so a change to an identity at a path a
// scope has statements under is refused, naming the scope, until scopes and schema are
// worked through. It was accepted, the check reading baseline alone, and the scope's
// positional elements were left under a schema that reads them as names
// (090mbrhsh12ksfr8mhn0).
func TestSchema_IdentityChangeUnderAScopesStatementsIsRefused(t *testing.T) {
	s := openTestStorage(t)
	scope, other := "sc", "other"
	mustCommit(t, s, nil, `{settled: 1}`)
	mustCommit(t, s, &scope, `{items: [{sku: A}]}`)
	mustCommit(t, s, &other, `{elsewhere: 1}`)

	_, err := s.SetSchema(testSchema(t, `{define: {items: {sku: !logd-key null}}}`), false)
	if err == nil {
		t.Fatal("items gained an identity under a scope's positional elements")
	}
	if !IsSchemaRefused(err) || !strings.Contains(err.Error(), `"sc"`) && !strings.Contains(err.Error(), "sc") ||
		strings.Contains(err.Error(), "other") {
		t.Errorf("the refusal does not name the scope, and only it: %v", err)
	}
	if schema, _ := s.GetActiveSchema(); schema != nil {
		t.Error("a refused schema became active")
	}

	// An identity elsewhere is fine: no scope has statements under tags.
	if _, err := s.SetSchema(testSchema(t, `{define: {tags: {id: !logd-key null}}}`), false); err != nil {
		t.Errorf("an identity under no scope's statements was refused: %v", err)
	}
	// And once the scope is gone, so is the objection.
	if err := s.DeleteScope(scope); err != nil {
		t.Fatalf("DeleteScope: %v", err)
	}
	if _, err := s.SetSchema(testSchema(t, `{define: {items: {sku: !logd-key null}, tags: {id: !logd-key null}}}`), false); err != nil {
		t.Errorf("refused after the scope was deleted: %v", err)
	}
}
