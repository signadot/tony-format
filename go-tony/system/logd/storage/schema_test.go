package storage

import (
	"fmt"
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

// TestMigration_BasicLifecycle tests Start → Complete migration flow.
func TestMigration_BasicLifecycle(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Initially no schema
	activeSchema, activeCommit := s.GetActiveSchema()
	if activeSchema != nil {
		t.Error("expected nil active schema initially")
	}
	if activeCommit != 0 {
		t.Errorf("expected activeCommit=0, got %d", activeCommit)
	}
	if s.HasPendingMigration() {
		t.Error("expected no pending migration initially")
	}

	// Write some data first (schema snapshots are created at current commit)
	patch, _ := parse.Parse([]byte(`{initial: "data"}`))
	tx, _ := s.NewTx(1, nil)
	p, _ := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch}})
	result := p.Commit()
	if !result.Committed {
		t.Fatalf("initial commit failed: %v", result.Error)
	}

	// Start migration to new schema
	newSchema := testSchema(t, `{users: .[array]}`)
	startCommit, err := s.StartMigration(newSchema)
	if err != nil {
		t.Fatalf("StartMigration() error = %v", err)
	}
	// Schema snapshot is created at current commit (1), not incrementing
	if startCommit < 0 {
		t.Errorf("expected non-negative commit, got %d", startCommit)
	}

	// Verify pending state
	if !s.HasPendingMigration() {
		t.Error("expected pending migration after StartMigration")
	}
	pendingSchema, pendingCommit := s.GetPendingSchema()
	if pendingSchema == nil {
		t.Error("expected pending schema after StartMigration")
	}
	if pendingCommit != startCommit {
		t.Errorf("expected pendingCommit=%d, got %d", startCommit, pendingCommit)
	}

	// Active schema should still be nil
	activeSchema, _ = s.GetActiveSchema()
	if activeSchema != nil {
		t.Error("expected active schema still nil during migration")
	}

	// Complete migration
	completeCommit, err := s.CompleteMigration()
	if err != nil {
		t.Fatalf("CompleteMigration() error = %v", err)
	}
	// Complete snapshot is at same commit as start (no new data written)
	if completeCommit < startCommit {
		t.Errorf("expected completeCommit >= startCommit, got %d < %d", completeCommit, startCommit)
	}

	// Verify migration completed
	if s.HasPendingMigration() {
		t.Error("expected no pending migration after CompleteMigration")
	}
	activeSchema, activeCommit = s.GetActiveSchema()
	if activeSchema == nil {
		t.Error("expected active schema after CompleteMigration")
	}
	if activeCommit != completeCommit {
		t.Errorf("expected activeCommit=%d, got %d", completeCommit, activeCommit)
	}
}

// TestMigration_Abort tests Start → Abort migration flow.
func TestMigration_Abort(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Write some data first so we have a non-zero commit
	patch, _ := parse.Parse([]byte(`{initial: "data"}`))
	tx, _ := s.NewTx(1, nil)
	p, _ := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch}})
	result := p.Commit()
	if !result.Committed {
		t.Fatalf("initial commit failed: %v", result.Error)
	}

	// Start migration
	newSchema := testSchema(t, `{posts: .[array]}`)
	_, err = s.StartMigration(newSchema)
	if err != nil {
		t.Fatalf("StartMigration() error = %v", err)
	}

	if !s.HasPendingMigration() {
		t.Error("expected pending migration")
	}

	// Abort migration
	abortCommit, err := s.AbortMigration()
	if err != nil {
		t.Fatalf("AbortMigration() error = %v", err)
	}
	if abortCommit < 0 {
		t.Errorf("expected non-negative commit, got %d", abortCommit)
	}

	// Verify abort cleared state
	if s.HasPendingMigration() {
		t.Error("expected no pending migration after AbortMigration")
	}
	pendingSchema, _ := s.GetPendingSchema()
	if pendingSchema != nil {
		t.Error("expected nil pending schema after AbortMigration")
	}

	// Active schema should still be nil (never completed)
	activeSchema, _ := s.GetActiveSchema()
	if activeSchema != nil {
		t.Error("expected active schema still nil after abort")
	}
}

// TestMigration_ErrorCases tests error conditions for migration operations.
func TestMigration_ErrorCases(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Complete without migration should fail
	_, err = s.CompleteMigration()
	if err != ErrNoMigrationInProgress {
		t.Errorf("expected ErrNoMigrationInProgress, got %v", err)
	}

	// Abort without migration should fail
	_, err = s.AbortMigration()
	if err != ErrNoMigrationInProgress {
		t.Errorf("expected ErrNoMigrationInProgress, got %v", err)
	}

	// Start migration
	schema := testSchema(t, `{data: .[string]}`)
	_, err = s.StartMigration(schema)
	if err != nil {
		t.Fatalf("StartMigration() error = %v", err)
	}

	// Start another migration should fail
	schema2 := testSchema(t, `{other: .[int]}`)
	_, err = s.StartMigration(schema2)
	if err != ErrMigrationInProgress {
		t.Errorf("expected ErrMigrationInProgress, got %v", err)
	}

	// Clean up
	s.AbortMigration()
}

// TestMigration_DualWrite verifies patches during migration go to both indexes.
func TestMigration_WritesDuringMigrationAreRead(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Write initial data before migration
	patch1, _ := parse.Parse([]byte(`{users: {alice: {name: "Alice"}}}`))
	tx1, _ := s.NewTx(1, nil)
	p1, _ := tx1.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch1}})
	result1 := p1.Commit()
	if !result1.Committed {
		t.Fatalf("initial commit failed: %v", result1.Error)
	}
	preCommit := result1.Commit

	// Start migration
	schema := testSchema(t, `{users: .[object]}`)
	migrationCommit, err := s.StartMigration(schema)
	if err != nil {
		t.Fatalf("StartMigration() error = %v", err)
	}

	// Write data during migration
	patch2, _ := parse.Parse([]byte(`{users: {bob: {name: "Bob"}}}`))
	tx2, _ := s.NewTx(1, nil)
	p2, _ := tx2.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch2}})
	result2 := p2.Commit()
	if !result2.Committed {
		t.Fatalf("during-migration commit failed: %v", result2.Error)
	}
	duringCommit := result2.Commit

	// Read from active index (baseline) - should see both users
	state, err := readStateAt(s, "", duringCommit, nil)
	if err != nil {
		t.Fatalf("ReadStateAt() error = %v", err)
	}
	aliceName := getString(state, "users", "alice", "name")
	if aliceName != "Alice" {
		t.Errorf("expected alice='Alice', got %q", aliceName)
	}
	bobName := getString(state, "users", "bob", "name")
	if bobName != "Bob" {
		t.Errorf("expected bob='Bob', got %q", bobName)
	}

	// Complete migration
	completeCommit, err := s.CompleteMigration()
	if err != nil {
		t.Fatalf("CompleteMigration() error = %v", err)
	}

	// Read after completing - should see both users
	stateAfter, err := readStateAt(s, "", completeCommit, nil)
	if err != nil {
		t.Fatalf("ReadStateAt after complete() error = %v", err)
	}
	aliceNameAfter := getString(stateAfter, "users", "alice", "name")
	if aliceNameAfter != "Alice" {
		t.Errorf("after complete: expected alice='Alice', got %q", aliceNameAfter)
	}
	bobNameAfter := getString(stateAfter, "users", "bob", "name")
	if bobNameAfter != "Bob" {
		t.Errorf("after complete: expected bob='Bob', got %q", bobNameAfter)
	}

	_ = preCommit
	_ = migrationCommit
}

// TestMigration_ReplayPendingState tests log replay restores pending migration state.
func TestMigration_ReplayPendingState(t *testing.T) {
	tmpDir := t.TempDir()

	// First session: start migration but don't complete
	s1, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// Write some initial data
	patch1, _ := parse.Parse([]byte(`{config: {version: 1}}`))
	tx1, _ := s1.NewTx(1, nil)
	p1, _ := tx1.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch1}})
	result1 := p1.Commit()
	if !result1.Committed {
		t.Fatalf("initial commit failed: %v", result1.Error)
	}

	// Start migration
	schema := testSchema(t, `{config: {version: .[int], feature: .[string]}}`)
	startCommit, err := s1.StartMigration(schema)
	if err != nil {
		t.Fatalf("StartMigration() error = %v", err)
	}

	// Write data during migration
	patch2, _ := parse.Parse([]byte(`{config: {version: 2}}`))
	tx2, _ := s1.NewTx(1, nil)
	p2, _ := tx2.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch2}})
	result2 := p2.Commit()
	if !result2.Committed {
		t.Fatalf("during-migration commit failed: %v", result2.Error)
	}
	duringCommit := result2.Commit

	// Close storage (simulating restart)
	if err := s1.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Reopen storage - should replay and restore pending migration state
	s2, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() second time error = %v", err)
	}
	defer s2.Close()

	// Verify pending migration was restored
	if !s2.HasPendingMigration() {
		t.Error("expected pending migration to be restored after reopen")
	}

	pendingSchema, pendingCommit := s2.GetPendingSchema()
	if pendingSchema == nil {
		t.Error("expected pending schema to be restored")
	}
	if pendingCommit != startCommit {
		t.Errorf("expected pendingCommit=%d, got %d", startCommit, pendingCommit)
	}

	// Verify active schema is still nil
	activeSchema, _ := s2.GetActiveSchema()
	if activeSchema != nil {
		t.Error("expected active schema still nil after replay")
	}

	// Verify data is accessible
	state, err := readStateAt(s2, "", duringCommit, nil)
	if err != nil {
		t.Fatalf("ReadStateAt() error = %v", err)
	}
	version := getInt(state, "config", "version")
	if version != 2 {
		t.Errorf("expected version=2, got %d", version)
	}

	// Complete migration after restart
	completeCommit, err := s2.CompleteMigration()
	if err != nil {
		t.Fatalf("CompleteMigration() after restart error = %v", err)
	}

	// Verify completion
	if s2.HasPendingMigration() {
		t.Error("expected no pending migration after CompleteMigration")
	}
	activeSchema, activeCommit := s2.GetActiveSchema()
	if activeSchema == nil {
		t.Error("expected active schema after CompleteMigration")
	}
	if activeCommit != completeCommit {
		t.Errorf("expected activeCommit=%d, got %d", completeCommit, activeCommit)
	}
}

// TestMigration_ReplayActiveState tests log replay restores completed migration state.
func TestMigration_ReplayActiveState(t *testing.T) {
	tmpDir := t.TempDir()

	// First session: complete a migration
	s1, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// Write some data first
	patch, _ := parse.Parse([]byte(`{initial: "data"}`))
	tx, _ := s1.NewTx(1, nil)
	p, _ := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch}})
	result := p.Commit()
	if !result.Committed {
		t.Fatalf("initial commit failed: %v", result.Error)
	}

	// Start and complete migration
	schema := testSchema(t, `{settings: .[object]}`)
	_, err = s1.StartMigration(schema)
	if err != nil {
		t.Fatalf("StartMigration() error = %v", err)
	}

	completeCommit, err := s1.CompleteMigration()
	if err != nil {
		t.Fatalf("CompleteMigration() error = %v", err)
	}

	// Close storage
	if err := s1.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Reopen storage
	s2, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() second time error = %v", err)
	}
	defer s2.Close()

	// Verify active schema was restored
	activeSchema, activeCommit := s2.GetActiveSchema()
	if activeSchema == nil {
		t.Error("expected active schema to be restored after reopen")
	}
	if activeCommit != completeCommit {
		t.Errorf("expected activeCommit=%d, got %d", completeCommit, activeCommit)
	}

	// Verify no pending migration
	if s2.HasPendingMigration() {
		t.Error("expected no pending migration after replay of completed migration")
	}
}

// TestMigration_ReplayAbortedState tests log replay after aborted migration.
func TestMigration_ReplayAbortedState(t *testing.T) {
	tmpDir := t.TempDir()

	// First session: start and abort migration
	s1, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	// Write some data first
	patch, _ := parse.Parse([]byte(`{initial: "data"}`))
	tx, _ := s1.NewTx(1, nil)
	p, _ := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch}})
	result := p.Commit()
	if !result.Committed {
		t.Fatalf("initial commit failed: %v", result.Error)
	}

	// Start migration
	schema := testSchema(t, `{temp: .[string]}`)
	_, err = s1.StartMigration(schema)
	if err != nil {
		t.Fatalf("StartMigration() error = %v", err)
	}

	// Abort migration
	_, err = s1.AbortMigration()
	if err != nil {
		t.Fatalf("AbortMigration() error = %v", err)
	}

	// Close storage
	if err := s1.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Reopen storage
	s2, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() second time error = %v", err)
	}
	defer s2.Close()

	// Verify no pending migration
	if s2.HasPendingMigration() {
		t.Error("expected no pending migration after replay of aborted migration")
	}

	// Verify no active schema (never completed)
	activeSchema, _ := s2.GetActiveSchema()
	if activeSchema != nil {
		t.Error("expected no active schema after replay of aborted migration")
	}
}

// TestMigration_MultipleMigrations tests multiple sequential migrations.
func TestMigration_MultipleMigrations(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Write initial data first
	initPatch, _ := parse.Parse([]byte(`{initial: "data"}`))
	initTx, _ := s.NewTx(1, nil)
	initP, _ := initTx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: initPatch}})
	initResult := initP.Commit()
	if !initResult.Committed {
		t.Fatalf("initial commit failed: %v", initResult.Error)
	}

	// First migration
	schema1 := testSchema(t, `{v1: .[string]}`)
	_, err = s.StartMigration(schema1)
	if err != nil {
		t.Fatalf("StartMigration 1 error = %v", err)
	}
	commit1, err := s.CompleteMigration()
	if err != nil {
		t.Fatalf("CompleteMigration 1 error = %v", err)
	}

	// Write some data
	patch, _ := parse.Parse([]byte(`{v1: "data"}`))
	tx, _ := s.NewTx(1, nil)
	p, _ := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: patch}})
	p.Commit()

	// Second migration
	schema2 := testSchema(t, `{v2: .[string]}`)
	_, err = s.StartMigration(schema2)
	if err != nil {
		t.Fatalf("StartMigration 2 error = %v", err)
	}
	commit2, err := s.CompleteMigration()
	if err != nil {
		t.Fatalf("CompleteMigration 2 error = %v", err)
	}

	// Verify second schema is now active
	activeSchema, activeCommit := s.GetActiveSchema()
	if activeSchema == nil {
		t.Error("expected active schema after second migration")
	}
	if activeCommit != commit2 {
		t.Errorf("expected activeCommit=%d, got %d", commit2, activeCommit)
	}
	if activeCommit <= commit1 {
		t.Errorf("expected commit2 > commit1, got %d <= %d", commit2, commit1)
	}
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

// migrateTo starts and completes a migration to schema, answering the commit it completed at.
func migrateTo(t *testing.T, s *Storage, schema string) int64 {
	t.Helper()
	if _, err := s.StartMigration(testSchema(t, schema)); err != nil {
		t.Fatalf("StartMigration(%s): %v", schema, err)
	}
	commit, err := s.CompleteMigration()
	if err != nil {
		t.Fatalf("CompleteMigration(%s): %v", schema, err)
	}
	return commit
}

// Every commit survives every migration, before a reopen and after it. A second
// migration lost everything written before the first -- a: 1 and the scope's s: 1 below --
// by installing an index backfilled only from the previous migration, whose snapshots
// were in the index that one had retired (4jw0pz1rh12ks9r8mhn0). A reopen after a write
// forgot the schema, its snapshots being in a retired index too (faweqnhvh12ksynxmdn0),
// and the persister went on persisting the retired index (rgpn3v9vh12ksynxmdn0).
func TestMigration_DataAndSchemaSurviveTwoMigrationsAndAReopen(t *testing.T) {
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
	defer s.Close()
	check("after the reopen", s)
}

// A commit that lands while a migration runs is kept. Commits during StartMigration's
// snapshot, and during CompleteMigration before its index swap, were missing from the
// index the migration installed, acknowledged and then gone -- across a reopen too
// (gdpv3fsvh12ksynxmdn0).
func TestMigration_WritesAlongsideAMigrationAreKept(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Big enough that a schema snapshot takes a while.
	var seed strings.Builder
	seed.WriteString("{big: {")
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&seed, "f%d: %d, ", i, i)
	}
	seed.WriteString("}}")
	mustCommit(t, s, nil, seed.String())

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
	// Writes before the migration, queued behind it, and after it.
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
	migrateTo(t, s, "{v1: .[string]}")
	waitFor(n.Load() + 20)
	close(stop)
	<-done
	if writeErr != nil {
		t.Fatalf("a write alongside the migration failed: %v", writeErr)
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
	t.Logf("%d writes alongside the migration", len(acked))
}

// StartMigration's identity check is asked again where the schema takes effect. Writes
// between Start and Complete are lowered under the active schema, so one can put
// elements by position into an array the pending schema keys; completing then would give
// them an identity they have no names under.
func TestMigration_CompleteRefusesIdentityOverElementsWrittenSinceStart(t *testing.T) {
	s := openTestStorage(t)
	mustCommit(t, s, nil, "{other: 1}")
	if _, err := s.StartMigration(testSchema(t, `{define: {items: {name: !logd-key null}}}`)); err != nil {
		t.Fatalf("StartMigration: %v", err)
	}
	mustCommit(t, s, nil, "{items: [{name: a}]}")
	if _, err := s.CompleteMigration(); err == nil {
		t.Fatal("CompleteMigration gave items an identity over an element written by position")
	} else if !strings.Contains(err.Error(), "written by position") {
		t.Errorf("the refusal does not say why: %v", err)
	}
	if schema, _ := s.GetActiveSchema(); schema != nil {
		t.Error("the refused schema became active")
	}
	if !s.HasPendingMigration() {
		t.Error("the refusal dropped the pending migration; it is the operator's to abort")
	}
}
