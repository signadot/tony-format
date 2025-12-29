package storage

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

func TestSwitchAndSnapshot(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Commit a few patches to have some state
	tx1, err := s.NewTx(1, nil)
	if err != nil {
		t.Fatalf("NewTx() error = %v", err)
	}

	patch1Data, err := parse.Parse([]byte(`{name: "alice"}`))
	if err != nil {
		t.Fatalf("parse patch1: %v", err)
	}

	patch1 := &api.Patch{
		PathData: api.PathData{
			Path: "",
			Data: patch1Data,
		},
	}
	p1, err := tx1.NewPatcher(patch1)
	if err != nil {
		t.Fatalf("NewPatcher() error = %v", err)
	}
	result1 := p1.Commit()
	if !result1.Committed {
		t.Fatalf("first commit failed: %v", result1.Error)
	}

	tx2, err := s.NewTx(1, nil)
	if err != nil {
		t.Fatalf("NewTx() error = %v", err)
	}

	patch2Data, err := parse.Parse([]byte(`{age: 30}`))
	if err != nil {
		t.Fatalf("parse patch2: %v", err)
	}

	patch2 := &api.Patch{
		PathData: api.PathData{
			Path: "",
			Data: patch2Data,
		},
	}
	p2, err := tx2.NewPatcher(patch2)
	if err != nil {
		t.Fatalf("NewPatcher() error = %v", err)
	}
	result2 := p2.Commit()
	if !result2.Committed {
		t.Fatalf("second commit failed: %v", result2.Error)
	}

	// Get current commit
	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit() error = %v", err)
	}
	if commit != 2 {
		t.Errorf("expected commit 2, got %d", commit)
	}

	// Switch and create snapshot
	if err := s.SwitchAndSnapshot(); err != nil {
		t.Fatalf("SwitchAndSnapshot() error = %v", err)
	}

	// Verify snapshot entry was added to index
	// Query for snapshot at commit 2
	segments := s.index.LookupWithin("", commit, nil)
	var foundSnapshot *index.LogSegment
	for i := range segments {
		seg := &segments[i]
		if seg.StartCommit == seg.EndCommit && seg.StartCommit == commit {
			foundSnapshot = seg
			break
		}
	}

	if foundSnapshot == nil {
		t.Fatal("snapshot entry not found in index")
	}

	// Verify snapshot entry has correct fields
	if foundSnapshot.StartCommit != commit {
		t.Errorf("snapshot StartCommit = %d, want %d", foundSnapshot.StartCommit, commit)
	}
	if foundSnapshot.EndCommit != commit {
		t.Errorf("snapshot EndCommit = %d, want %d", foundSnapshot.EndCommit, commit)
	}
	if foundSnapshot.KindedPath != "" {
		t.Errorf("snapshot KindedPath = %q, want empty string", foundSnapshot.KindedPath)
	}

	// Verify we can read the snapshot entry
	entry, err := s.dLog.ReadEntryAt(s.dLog.GetInactiveLog(), foundSnapshot.LogPosition)
	if err != nil {
		t.Fatalf("ReadEntryAt() error = %v", err)
	}

	if entry.SnapPos == nil {
		t.Error("entry.SnapPos is nil, expected non-nil for snapshot entry")
	}
	if entry.Commit != commit {
		t.Errorf("entry.Commit = %d, want %d", entry.Commit, commit)
	}
	if entry.Patch != nil {
		t.Error("entry.Patch should be nil for snapshot entry")
	}

	t.Logf("Snapshot created successfully at commit %d, logFile %s, position %d, snapPos %d",
		entry.Commit, foundSnapshot.LogFile, foundSnapshot.LogPosition, *entry.SnapPos)
}

func TestSnapshotRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Helper to commit a patch
	commitPatch := func(patchStr string) int64 {
		tx, err := s.NewTx(1, nil)
		if err != nil {
			t.Fatalf("NewTx() error = %v", err)
		}

		patchData, err := parse.Parse([]byte(patchStr))
		if err != nil {
			t.Fatalf("parse patch: %v", err)
		}

		patch := &api.Patch{
			PathData: api.PathData{
				Path: "",
				Data: patchData,
			},
		}
		patcher, err := tx.NewPatcher(patch)
		if err != nil {
			t.Fatalf("NewPatcher() error = %v", err)
		}
		result := patcher.Commit()
		if !result.Committed {
			t.Fatalf("commit failed: %v", result.Error)
		}
		return result.Commit
	}

	// Stage 1: Create initial state (commits 1-3)
	commitPatch(`{name: "alice"}`)
	commitPatch(`{age: 30}`)
	commit3 := commitPatch(`{city: "NYC"}`)

	// Verify state before snapshot
	stateBefore, err := s.ReadStateAt("", commit3, nil)
	if err != nil {
		t.Fatalf("ReadStateAt(commit3) error = %v", err)
	}
	// Fields are in alphabetical order after patching
	expectedBefore := `{age: 30, city: "NYC", name: "alice"}`
	expectedNode, _ := parse.Parse([]byte(expectedBefore))
	expectedNode.Tag = "" // Remove formatting tag for comparison
	if !stateBefore.DeepEqual(expectedNode) {
		t.Errorf("state before snapshot mismatch:\ngot:  %s\nwant: %s",
			encode.MustString(stateBefore), expectedBefore)
	}

	// Stage 2: Create snapshot at commit 3
	if err := s.createSnapshot(commit3, nil); err != nil {
		t.Fatalf("createSnapshot(commit3) error = %v", err)
	}

	// Verify snapshot was created in index
	segments := s.index.LookupWithin("", commit3, nil)
	var foundSnapshot bool
	for i := range segments {
		seg := &segments[i]
		if seg.StartCommit == seg.EndCommit && seg.StartCommit == commit3 {
			foundSnapshot = true
			t.Logf("Found snapshot at commit %d, logFile %s, position %d",
				seg.StartCommit, seg.LogFile, seg.LogPosition)
			break
		}
	}
	if !foundSnapshot {
		t.Fatal("snapshot entry not found in index")
	}

	// Stage 3: Add more patches after snapshot (commits 4-6)
	commitPatch(`{country: "USA"}`)
	commitPatch(`{zipcode: "10001"}`)
	commit6 := commitPatch(`{verified: true}`)

	// Stage 4: Read state at various points and verify snapshot is used

	// Read at commit3 (should use snapshot, no patches needed)
	stateAt3, err := s.ReadStateAt("", commit3, nil)
	if err != nil {
		t.Fatalf("ReadStateAt(commit3) error = %v", err)
	}
	if !stateAt3.DeepEqual(expectedNode) {
		t.Errorf("state at commit3 mismatch:\ngot:  %s\nwant: %s",
			encode.MustString(stateAt3), expectedBefore)
	}

	// Read at commit5 (should use snapshot + 2 patches)
	stateAt5, err := s.ReadStateAt("", 5, nil)
	if err != nil {
		t.Fatalf("ReadStateAt(commit5) error = %v", err)
	}
	// Fields in alphabetical order
	expectedAt5 := `{age: 30, city: "NYC", country: "USA", name: "alice", zipcode: "10001"}`
	expectedAt5Node, _ := parse.Parse([]byte(expectedAt5))
	expectedAt5Node.Tag = "" // Remove formatting tag for comparison
	if !stateAt5.DeepEqual(expectedAt5Node) {
		t.Errorf("state at commit5 mismatch:\ngot:  %s\nwant: %s",
			encode.MustString(stateAt5), expectedAt5)
	}

	// Read at commit6 (should use snapshot + 3 patches)
	stateAt6, err := s.ReadStateAt("", commit6, nil)
	if err != nil {
		t.Fatalf("ReadStateAt(commit6) error = %v", err)
	}
	// Fields in alphabetical order
	expectedAt6 := `{age: 30, city: "NYC", country: "USA", name: "alice", verified: true, zipcode: "10001"}`
	expectedAt6Node, _ := parse.Parse([]byte(expectedAt6))
	expectedAt6Node.Tag = "" // Remove formatting tag for comparison
	if !stateAt6.DeepEqual(expectedAt6Node) {
		t.Errorf("state at commit6 mismatch:\ngot:  %s\nwant: %s",
			encode.MustString(stateAt6), expectedAt6)
	}

	// Stage 5: Create another snapshot and verify layering works
	if err := s.createSnapshot(commit6, nil); err != nil {
		t.Fatalf("createSnapshot(commit6) error = %v", err)
	}

	// Add one more patch after second snapshot
	commit7 := commitPatch(`{premium: true}`)

	// Read at commit7 (should use second snapshot + 1 patch)
	stateAt7, err := s.ReadStateAt("", commit7, nil)
	if err != nil {
		t.Fatalf("ReadStateAt(commit7) error = %v", err)
	}
	// Fields in alphabetical order
	expectedAt7 := `{age: 30, city: "NYC", country: "USA", name: "alice", premium: true, verified: true, zipcode: "10001"}`
	expectedAt7Node, _ := parse.Parse([]byte(expectedAt7))
	expectedAt7Node.Tag = "" // Remove formatting tag for comparison
	if !stateAt7.DeepEqual(expectedAt7Node) {
		t.Errorf("state at commit7 mismatch:\ngot:  %s\nwant: %s",
			encode.MustString(stateAt7), expectedAt7)
	}

	// Stage 6: Verify reading at old commits still works with multiple snapshots
	stateAt3Again, err := s.ReadStateAt("", commit3, nil)
	if err != nil {
		t.Fatalf("ReadStateAt(commit3 again) error = %v", err)
	}
	if !stateAt3Again.DeepEqual(expectedNode) {
		t.Errorf("state at commit3 (after multiple snapshots) mismatch:\ngot:  %s\nwant: %s",
			encode.MustString(stateAt3Again), expectedBefore)
	}

	t.Logf("Round-trip test successful: created 2 snapshots, verified state reconstruction at all commits")
}

func TestScopedSnapshotRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := Open(tmpDir, nil)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	// Helper to commit a patch
	commitPatch := func(patchStr string, scopeID *string) int64 {
		tx, err := s.NewTx(1, scopeID)
		if err != nil {
			t.Fatalf("NewTx() error = %v", err)
		}

		patchData, err := parse.Parse([]byte(patchStr))
		if err != nil {
			t.Fatalf("parse patch: %v", err)
		}

		patch := &api.Patch{
			PathData: api.PathData{
				Path: "",
				Data: patchData,
			},
		}
		patcher, err := tx.NewPatcher(patch)
		if err != nil {
			t.Fatalf("NewPatcher() error = %v", err)
		}
		result := patcher.Commit()
		if !result.Committed {
			t.Fatalf("commit failed: %v", result.Error)
		}
		return result.Commit
	}

	// Stage 1: Create baseline state
	commitPatch(`{name: "baseline"}`, nil)
	commitPatch(`{version: 1}`, nil)
	commit3 := commitPatch(`{status: "active"}`, nil)

	// Stage 2: Create scope "sandbox1" with modifications
	scope := "sandbox1"
	commitPatch(`{name: "scoped"}`, &scope)
	commit5 := commitPatch(`{extra: "sandbox-data"}`, &scope)

	// Verify baseline state (should not include scope changes)
	baselineState, err := s.ReadStateAt("", commit5, nil)
	if err != nil {
		t.Fatalf("baseline read error: %v", err)
	}
	expectedBaseline := `{name: "baseline", status: "active", version: 1}`
	expectedBaselineNode, _ := parse.Parse([]byte(expectedBaseline))
	expectedBaselineNode.Tag = ""
	if !baselineState.DeepEqual(expectedBaselineNode) {
		t.Errorf("baseline state mismatch:\ngot:  %s\nwant: %s",
			encode.MustString(baselineState), expectedBaseline)
	}

	// Verify scope state (should include baseline + scope changes)
	scopeState, err := s.ReadStateAt("", commit5, &scope)
	if err != nil {
		t.Fatalf("scope read error: %v", err)
	}
	expectedScope := `{extra: "sandbox-data", name: "scoped", status: "active", version: 1}`
	expectedScopeNode, _ := parse.Parse([]byte(expectedScope))
	expectedScopeNode.Tag = ""
	if !scopeState.DeepEqual(expectedScopeNode) {
		t.Errorf("scope state mismatch:\ngot:  %s\nwant: %s",
			encode.MustString(scopeState), expectedScope)
	}

	// Stage 3: Create scope snapshot at commit5
	if err := s.CreateScopeSnapshot(scope, commit5); err != nil {
		t.Fatalf("CreateScopeSnapshot error = %v", err)
	}

	// Verify scope snapshot was created in index
	segments := s.index.LookupWithin("", commit5, &scope)
	var foundScopeSnapshot *index.LogSegment
	for i := range segments {
		seg := &segments[i]
		if seg.StartCommit == seg.EndCommit && seg.StartCommit == commit5 && seg.ScopeID != nil && *seg.ScopeID == scope {
			foundScopeSnapshot = seg
			break
		}
	}
	if foundScopeSnapshot == nil {
		t.Fatal("scope snapshot entry not found in index")
	}
	t.Logf("Found scope snapshot at commit %d, scopeID %s, logFile %s",
		foundScopeSnapshot.StartCommit, *foundScopeSnapshot.ScopeID, foundScopeSnapshot.LogFile)

	// Stage 4: Add more scope patches after snapshot
	commitPatch(`{extra2: "more-data"}`, &scope)
	commit7 := commitPatch(`{counter: 42}`, &scope)

	// Verify scope state uses snapshot + new patches
	scopeStateAfter, err := s.ReadStateAt("", commit7, &scope)
	if err != nil {
		t.Fatalf("scope read after snapshot error: %v", err)
	}
	expectedScopeAfter := `{counter: 42, extra: "sandbox-data", extra2: "more-data", name: "scoped", status: "active", version: 1}`
	expectedScopeAfterNode, _ := parse.Parse([]byte(expectedScopeAfter))
	expectedScopeAfterNode.Tag = ""
	if !scopeStateAfter.DeepEqual(expectedScopeAfterNode) {
		t.Errorf("scope state after snapshot mismatch:\ngot:  %s\nwant: %s",
			encode.MustString(scopeStateAfter), expectedScopeAfter)
	}

	// Stage 5: Verify baseline is unaffected by scope snapshot
	baselineStateAfter, err := s.ReadStateAt("", commit7, nil)
	if err != nil {
		t.Fatalf("baseline read after scope snapshot error: %v", err)
	}
	if !baselineStateAfter.DeepEqual(expectedBaselineNode) {
		t.Errorf("baseline state after scope snapshot mismatch:\ngot:  %s\nwant: %s",
			encode.MustString(baselineStateAfter), expectedBaseline)
	}

	// Stage 6: Verify old scope reads still work (should use baseline snapshot + scope patches)
	scopeStateAt3, err := s.ReadStateAt("", commit3, &scope)
	if err != nil {
		t.Fatalf("scope read at commit3 error: %v", err)
	}
	// At commit3, no scope patches yet, so should match baseline
	if !scopeStateAt3.DeepEqual(expectedBaselineNode) {
		t.Errorf("scope state at commit3 mismatch:\ngot:  %s\nwant: %s",
			encode.MustString(scopeStateAt3), expectedBaseline)
	}

	t.Logf("Scoped snapshot round-trip successful: scope snapshot created and used correctly")
}
