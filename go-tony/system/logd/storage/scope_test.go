package storage

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// getString extracts a string value from a node at the given field path
func getString(n *ir.Node, fields ...string) string {
	if n == nil {
		return ""
	}
	current := n
	for _, field := range fields {
		if current.Type != ir.ObjectType {
			return ""
		}
		found := false
		for i, f := range current.Fields {
			if f.String == field {
				current = current.Values[i]
				found = true
				break
			}
		}
		if !found {
			return ""
		}
	}
	if current.Type == ir.StringType {
		return current.String
	}
	return ""
}

// getInt extracts an int64 value from a node at the given field path
func getInt(n *ir.Node, fields ...string) int64 {
	if n == nil {
		return -1
	}
	current := n
	for _, field := range fields {
		if current.Type != ir.ObjectType {
			return -1
		}
		found := false
		for i, f := range current.Fields {
			if f.String == field {
				current = current.Values[i]
				found = true
				break
			}
		}
		if !found {
			return -1
		}
	}
	if current.Type == ir.NumberType && current.Int64 != nil {
		return *current.Int64
	}
	return -1
}

// TestScope_Isolation verifies that scoped writes are isolated from baseline.
func TestScope_Isolation(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// 1. Write to baseline: {users: {alice: {name: "Alice"}}}
	baselinePatch, _ := parse.Parse([]byte(`{users: {alice: {name: "Alice"}}}`))
	tx1, _ := s.NewTx(1, nil) // nil meta = baseline
	p1, _ := tx1.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: baselinePatch}})
	result1 := p1.Commit()
	if !result1.Committed {
		t.Fatalf("baseline commit failed: %v", result1.Error)
	}
	baselineCommit := result1.Commit

	// 2. Write to scope "sandbox1": {users: {alice: {name: "Alice in Sandbox"}}}
	scope1 := "sandbox1"
	scopePatch, _ := parse.Parse([]byte(`{users: {alice: {name: "Alice in Sandbox"}}}`))
	tx2, _ := s.NewTx(1, &api.PatchMeta{Scope: &scope1})
	p2, _ := tx2.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: scopePatch}})
	result2 := p2.Commit()
	if !result2.Committed {
		t.Fatalf("scope commit failed: %v", result2.Error)
	}
	scopeCommit := result2.Commit

	// 3. Verify baseline read sees only baseline data
	baselineState, err := s.ReadStateAt("", scopeCommit, nil)
	if err != nil {
		t.Fatalf("baseline read error: %v", err)
	}
	baselineName := getString(baselineState, "users", "alice", "name")
	if baselineName != "Alice" {
		t.Errorf("baseline read: expected 'Alice', got %q", baselineName)
	}

	// 4. Verify scope read sees scope data (overrides baseline)
	scopeState, err := s.ReadStateAt("", scopeCommit, &scope1)
	if err != nil {
		t.Fatalf("scope read error: %v", err)
	}
	scopeName := getString(scopeState, "users", "alice", "name")
	if scopeName != "Alice in Sandbox" {
		t.Errorf("scope read: expected 'Alice in Sandbox', got %q", scopeName)
	}

	// 5. Verify historical baseline read still works
	historicalState, err := s.ReadStateAt("", baselineCommit, nil)
	if err != nil {
		t.Fatalf("historical read error: %v", err)
	}
	historicalName := getString(historicalState, "users", "alice", "name")
	if historicalName != "Alice" {
		t.Errorf("historical read: expected 'Alice', got %q", historicalName)
	}
}

// TestScope_COWSemantics verifies copy-on-write: scope reads include baseline data.
func TestScope_COWSemantics(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// 1. Write baseline data at different paths
	baselinePatch, _ := parse.Parse([]byte(`{
		users: {alice: {name: "Alice"}, bob: {name: "Bob"}},
		config: {theme: "dark"}
	}`))
	tx1, _ := s.NewTx(1, nil)
	p1, _ := tx1.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: baselinePatch}})
	result1 := p1.Commit()
	if !result1.Committed {
		t.Fatalf("baseline commit failed: %v", result1.Error)
	}

	// 2. Write scope data that only modifies users.alice
	scope := "sandbox1"
	scopePatch, _ := parse.Parse([]byte(`{users: {alice: {name: "Alice Modified"}}}`))
	tx2, _ := s.NewTx(1, &api.PatchMeta{Scope: &scope})
	p2, _ := tx2.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: scopePatch}})
	result2 := p2.Commit()
	if !result2.Committed {
		t.Fatalf("scope commit failed: %v", result2.Error)
	}
	commit := result2.Commit

	// 3. Read full state with scope
	scopeState, err := s.ReadStateAt("", commit, &scope)
	if err != nil {
		t.Fatalf("scope read error: %v", err)
	}

	// 4. Scope read of modified path should see scope value
	aliceName := getString(scopeState, "users", "alice", "name")
	if aliceName != "Alice Modified" {
		t.Errorf("scope read alice: expected 'Alice Modified', got %q", aliceName)
	}

	// 5. Scope read of unmodified path should see baseline value (COW)
	bobName := getString(scopeState, "users", "bob", "name")
	if bobName != "Bob" {
		t.Errorf("scope read bob: expected 'Bob' (from baseline), got %q", bobName)
	}

	// 6. Scope read of another unmodified path should see baseline value
	theme := getString(scopeState, "config", "theme")
	if theme != "dark" {
		t.Errorf("scope read config: expected 'dark' (from baseline), got %q", theme)
	}
}

// TestScope_MultipleScopes verifies isolation between different scopes.
func TestScope_MultipleScopes(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// 1. Write baseline
	baselinePatch, _ := parse.Parse([]byte(`{counter: 0}`))
	tx1, _ := s.NewTx(1, nil)
	p1, _ := tx1.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: baselinePatch}})
	p1.Commit()

	// 2. Write to scope1
	scope1 := "sandbox1"
	scope1Patch, _ := parse.Parse([]byte(`{counter: 100}`))
	tx2, _ := s.NewTx(1, &api.PatchMeta{Scope: &scope1})
	p2, _ := tx2.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: scope1Patch}})
	p2.Commit()

	// 3. Write to scope2
	scope2 := "sandbox2"
	scope2Patch, _ := parse.Parse([]byte(`{counter: 200}`))
	tx3, _ := s.NewTx(1, &api.PatchMeta{Scope: &scope2})
	p3, _ := tx3.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: scope2Patch}})
	result3 := p3.Commit()
	commit := result3.Commit

	// 4. Verify each scope sees its own value
	baselineState, _ := s.ReadStateAt("", commit, nil)
	baselineVal := getInt(baselineState, "counter")
	if baselineVal != 0 {
		t.Errorf("baseline: expected 0, got %d", baselineVal)
	}

	scope1State, _ := s.ReadStateAt("", commit, &scope1)
	scope1Val := getInt(scope1State, "counter")
	if scope1Val != 100 {
		t.Errorf("scope1: expected 100, got %d", scope1Val)
	}

	scope2State, _ := s.ReadStateAt("", commit, &scope2)
	scope2Val := getInt(scope2State, "counter")
	if scope2Val != 200 {
		t.Errorf("scope2: expected 200, got %d", scope2Val)
	}
}

// TestScope_DeleteScope verifies that DeleteScope removes scope data from index.
func TestScope_DeleteScope(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// 1. Write baseline
	baselinePatch, _ := parse.Parse([]byte(`{data: "baseline"}`))
	tx1, _ := s.NewTx(1, nil)
	p1, _ := tx1.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: baselinePatch}})
	p1.Commit()

	// 2. Write to scope
	scope := "to-delete"
	scopePatch, _ := parse.Parse([]byte(`{data: "scoped"}`))
	tx2, _ := s.NewTx(1, &api.PatchMeta{Scope: &scope})
	p2, _ := tx2.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: scopePatch}})
	result2 := p2.Commit()
	commit := result2.Commit

	// 3. Verify scope data is visible
	scopeState, _ := s.ReadStateAt("", commit, &scope)
	scopeVal := getString(scopeState, "data")
	if scopeVal != "scoped" {
		t.Errorf("before delete: expected 'scoped', got %q", scopeVal)
	}

	// 4. Delete scope
	if err := s.DeleteScope(scope); err != nil {
		t.Fatalf("DeleteScope error: %v", err)
	}

	// 5. Verify scope read now falls back to baseline (no scope data)
	afterDeleteState, _ := s.ReadStateAt("", commit, &scope)
	afterDeleteVal := getString(afterDeleteState, "data")
	if afterDeleteVal != "baseline" {
		t.Errorf("after delete: expected 'baseline', got %q", afterDeleteVal)
	}

	// 6. Verify DeleteScope on non-existent scope returns error
	if err := s.DeleteScope("nonexistent"); err == nil {
		t.Error("expected error when deleting non-existent scope")
	}
}

// TestScope_CommitNotification verifies notifications include scope ID.
func TestScope_CommitNotification(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	var notifications []*CommitNotification
	s.SetCommitNotifier(func(n *CommitNotification) {
		notifications = append(notifications, n)
	})

	// 1. Baseline commit
	baselinePatch, _ := parse.Parse([]byte(`{x: 1}`))
	tx1, _ := s.NewTx(1, nil)
	p1, _ := tx1.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: baselinePatch}})
	p1.Commit()

	// 2. Scoped commit
	scope := "test-scope"
	scopePatch, _ := parse.Parse([]byte(`{x: 2}`))
	tx2, _ := s.NewTx(1, &api.PatchMeta{Scope: &scope})
	p2, _ := tx2.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: scopePatch}})
	p2.Commit()

	// 3. Verify notifications
	if len(notifications) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(notifications))
	}

	// Baseline notification should have nil ScopeID
	if notifications[0].ScopeID != nil {
		t.Errorf("baseline notification: expected nil ScopeID, got %v", notifications[0].ScopeID)
	}

	// Scoped notification should have scope ID
	if notifications[1].ScopeID == nil || *notifications[1].ScopeID != scope {
		t.Errorf("scoped notification: expected ScopeID=%q, got %v", scope, notifications[1].ScopeID)
	}
}

// TestScope_ReadPatchesInRange verifies patch reading respects scope filtering.
func TestScope_ReadPatchesInRange(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// 1. Baseline commit at path "data"
	baselinePatch, _ := parse.Parse([]byte(`{data: {v: 1}}`))
	tx1, _ := s.NewTx(1, nil)
	p1, _ := tx1.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: baselinePatch}})
	p1.Commit()

	// 2. Scope commit at path "data"
	scope := "test"
	scopePatch, _ := parse.Parse([]byte(`{data: {v: 2}}`))
	tx2, _ := s.NewTx(1, &api.PatchMeta{Scope: &scope})
	p2, _ := tx2.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: scopePatch}})
	p2.Commit()

	// 3. Another baseline commit
	baselinePatch2, _ := parse.Parse([]byte(`{data: {v: 3}}`))
	tx3, _ := s.NewTx(1, nil)
	p3, _ := tx3.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: baselinePatch2}})
	result3 := p3.Commit()
	endCommit := result3.Commit

	// 4. Read patches for baseline - should see commits 1 and 3 only
	baselinePatches, err := s.ReadPatchesInRange("data", 1, endCommit, nil)
	if err != nil {
		t.Fatalf("ReadPatchesInRange baseline error: %v", err)
	}
	// Should have 2 patches (commits 1 and 3)
	if len(baselinePatches) != 2 {
		t.Errorf("baseline patches: expected 2, got %d", len(baselinePatches))
	}

	// 5. Read patches for scope - should see commits 1, 2, and 3
	scopePatches, err := s.ReadPatchesInRange("data", 1, endCommit, &scope)
	if err != nil {
		t.Fatalf("ReadPatchesInRange scope error: %v", err)
	}
	// Should have 3 patches (commits 1, 2, and 3)
	if len(scopePatches) != 3 {
		t.Errorf("scope patches: expected 3, got %d", len(scopePatches))
	}
}

// TestScope_BaselineAndScopePaths verifies that scoped reads can access both
// baseline paths and scope-specific paths.
func TestScope_BaselineAndScopePaths(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Create baseline data at path1
	scope := "filter-test"

	baselinePatch, _ := parse.Parse([]byte(`{path1: "baseline"}`))
	tx1, _ := s.NewTx(1, nil)
	p1, _ := tx1.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: baselinePatch}})
	p1.Commit()

	// Create scope data at path2 (different path)
	scopePatch, _ := parse.Parse([]byte(`{path2: "scoped"}`))
	tx2, _ := s.NewTx(1, &api.PatchMeta{Scope: &scope})
	p2, _ := tx2.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: scopePatch}})
	result2 := p2.Commit()
	commit := result2.Commit

	// Baseline read should only see path1, not path2
	baselineState, _ := s.ReadStateAt("", commit, nil)
	baselinePath1 := getString(baselineState, "path1")
	if baselinePath1 != "baseline" {
		t.Errorf("baseline read path1: expected 'baseline', got %q", baselinePath1)
	}
	baselinePath2 := getString(baselineState, "path2")
	if baselinePath2 != "" {
		t.Errorf("baseline read path2: expected empty (not visible), got %q", baselinePath2)
	}

	// Scoped read should see both path1 (from baseline) and path2 (from scope)
	scopeState, _ := s.ReadStateAt("", commit, &scope)
	scopePath1 := getString(scopeState, "path1")
	if scopePath1 != "baseline" {
		t.Errorf("scope read path1: expected 'baseline', got %q", scopePath1)
	}
	scopePath2 := getString(scopeState, "path2")
	if scopePath2 != "scoped" {
		t.Errorf("scope read path2: expected 'scoped', got %q", scopePath2)
	}
}

// TestScope_EmptyScope verifies empty string scope is treated as a valid scope.
func TestScope_EmptyScope(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Empty string is a valid scope (different from nil)
	emptyScope := ""

	baselinePatch, _ := parse.Parse([]byte(`{val: "baseline"}`))
	tx1, _ := s.NewTx(1, nil)
	p1, _ := tx1.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: baselinePatch}})
	p1.Commit()

	scopePatch, _ := parse.Parse([]byte(`{val: "empty-scope"}`))
	tx2, _ := s.NewTx(1, &api.PatchMeta{Scope: &emptyScope})
	p2, _ := tx2.NewPatcher(&api.Patch{Patch: api.Body{Path: "", Data: scopePatch}})
	result2 := p2.Commit()
	commit := result2.Commit

	// Baseline read
	baselineState, _ := s.ReadStateAt("", commit, nil)
	baselineVal := getString(baselineState, "val")
	if baselineVal != "baseline" {
		t.Errorf("baseline: expected 'baseline', got %q", baselineVal)
	}

	// Empty scope read should see scope value
	emptyState, _ := s.ReadStateAt("", commit, &emptyScope)
	emptyVal := getString(emptyState, "val")
	if emptyVal != "empty-scope" {
		t.Errorf("empty scope: expected 'empty-scope', got %q", emptyVal)
	}
}
