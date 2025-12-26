package tx

import (
	"errors"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// mockCommitOps is a mock implementation of CommitOps for testing
type mockCommitOps struct {
	currentCommit int64
	commits       []int64
	readState     map[string]map[int64]*ir.Node // kpath -> commit -> state
	written       []writtenEntry
}

type writtenEntry struct {
	commit      int64
	txSeq       int64
	timestamp   string
	mergedPatch *ir.Node
	txState     *State
	lastCommit  int64
}

func newMockCommitOps() *mockCommitOps {
	return &mockCommitOps{
		currentCommit: 0,
		commits:       []int64{},
		readState:     make(map[string]map[int64]*ir.Node),
		written:       []writtenEntry{},
	}
}

func (m *mockCommitOps) ReadStateAt(kp string, commit int64, scopeID *string) (*ir.Node, error) {
	if commitMap, ok := m.readState[kp]; ok {
		if state, ok := commitMap[commit]; ok {
			return state, nil
		}
	}
	return nil, nil // Return nil for missing state (not an error)
}

func (m *mockCommitOps) GetCurrentCommit() (int64, error) {
	return m.currentCommit, nil
}

func (m *mockCommitOps) NextCommit() (int64, error) {
	m.currentCommit++
	m.commits = append(m.commits, m.currentCommit)
	return m.currentCommit, nil
}

func (m *mockCommitOps) GetSchema(scopeID *string) *api.Schema {
	return nil // No auto-id schema in tests by default
}

func (m *mockCommitOps) WriteAndIndex(commit, txSeq int64, timestamp string, mergedPatch *ir.Node, txState *State, lastCommit int64) (string, int64, error) {
	m.written = append(m.written, writtenEntry{
		commit:      commit,
		txSeq:       txSeq,
		timestamp:   timestamp,
		mergedPatch: mergedPatch,
		txState:     txState,
		lastCommit:  lastCommit,
	})
	return "A", 0, nil
}

func (m *mockCommitOps) setState(kpath string, commit int64, state *ir.Node) {
	if m.readState[kpath] == nil {
		m.readState[kpath] = make(map[int64]*ir.Node)
	}
	m.readState[kpath][commit] = state
}

func TestNew(t *testing.T) {
	store := NewInMemoryTxStore()
	commitOps := newMockCommitOps()
	state := &State{
		TxID:        1,
		CreatedAt:   time.Now(),
		PatcherData: make([]*PatcherData, 0, 2),
	}

	tx := New(store, commitOps, state)
	if tx == nil {
		t.Fatal("New returned nil")
	}

	if tx.ID() != 1 {
		t.Errorf("Expected TxID 1, got %d", tx.ID())
	}

	// With capacity 2, expectedCount is 2, so transaction is not complete until 2 patchers are added
	if tx.IsComplete() {
		t.Error("Expected incomplete transaction (expectedCount=cap=2, no patchers added yet)")
	}
}

func TestNewPatcher(t *testing.T) {
	store := NewInMemoryTxStore()
	commitOps := newMockCommitOps()
	// Initialize with capacity 1, so expectedCount will be 1
	state := &State{
		TxID:        1,
		CreatedAt:   time.Now(),
		PatcherData: make([]*PatcherData, 0, 1),
	}

	tx := New(store, commitOps, state)
	// Put tx in store before calling NewPatcher (UpdateState needs to find it)
	if err := store.Put(tx); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	patch1 := &api.Patch{
		PathData: api.PathData{
			Path: "foo",
			Data: ir.FromString("bar"),
		},
	}

	patcher1, err := tx.NewPatcher(patch1)
	if err != nil {
		t.Fatalf("NewPatcher failed: %v", err)
	}
	if patcher1 == nil {
		t.Fatal("NewPatcher returned nil")
	}

	if !tx.IsComplete() {
		t.Error("Expected complete transaction after adding 1 patcher (expectedCount=1), got incomplete")
	}

	// Try to add another patcher when capacity is full
	patch2 := &api.Patch{
		PathData: api.PathData{
			Path: "baz",
			Data: ir.FromString("qux"),
		},
	}
	_, err = tx.NewPatcher(patch2)
	if err == nil {
		t.Error("Expected error when adding patcher beyond capacity, got nil")
	}
}

func TestCommit_Success(t *testing.T) {
	store := NewInMemoryTxStore()
	commitOps := newMockCommitOps()
	commitOps.currentCommit = 10
	commitOps.setState("foo", 10, ir.FromString("old"))

	state := &State{
		TxID:        1,
		CreatedAt:   time.Now(),
		PatcherData: make([]*PatcherData, 0, 1),
	}

	tx := New(store, commitOps, state)
	_ = store.Put(tx)

	patch := &api.Patch{
		PathData: api.PathData{
			Path: "foo",
			Data: ir.FromString("bar"),
		},
	}

	patcher, err := tx.NewPatcher(patch)
	if err != nil {
		t.Fatalf("NewPatcher failed: %v", err)
	}

	result := patcher.Commit()
	if !result.Committed {
		t.Errorf("Expected committed=true, got false. Error: %v", result.Error)
	}
	if !result.Matched {
		t.Error("Expected matched=true, got false")
	}
	if result.Commit != 11 {
		t.Errorf("Expected commit 11, got %d", result.Commit)
	}
	if result.Error != nil {
		t.Errorf("Expected no error, got %v", result.Error)
	}

	// Verify entry was written
	if len(commitOps.written) != 1 {
		t.Fatalf("Expected 1 written entry, got %d", len(commitOps.written))
	}
	written := commitOps.written[0]
	if written.commit != 11 {
		t.Errorf("Expected commit 11, got %d", written.commit)
	}
	if written.lastCommit != 10 {
		t.Errorf("Expected lastCommit 10, got %d", written.lastCommit)
	}
	if written.txSeq != 1 {
		t.Errorf("Expected txSeq 1, got %d", written.txSeq)
	}
	if written.mergedPatch == nil {
		t.Error("Expected mergedPatch to be set")
	}
	if written.txState == nil {
		t.Error("Expected txState to be set")
	}
}

func TestCommit_FirstCommit(t *testing.T) {
	store := NewInMemoryTxStore()
	commitOps := newMockCommitOps()
	commitOps.currentCommit = 0

	state := &State{
		TxID:        1,
		CreatedAt:   time.Now(),
		PatcherData: make([]*PatcherData, 0, 1),
	}

	tx := New(store, commitOps, state)
	_ = store.Put(tx)

	patch := &api.Patch{
		PathData: api.PathData{
			Path: "foo",
			Data: ir.FromString("bar"),
		},
	}

	patcher, err := tx.NewPatcher(patch)
	if err != nil {
		t.Fatalf("NewPatcher failed: %v", err)
	}

	result := patcher.Commit()
	if !result.Committed {
		t.Errorf("Expected committed=true, got false. Error: %v", result.Error)
	}
	if result.Commit != 1 {
		t.Errorf("Expected commit 1, got %d", result.Commit)
	}

	// Verify lastCommit is 0 for first commit
	if len(commitOps.written) != 1 {
		t.Fatalf("Expected 1 written entry, got %d", len(commitOps.written))
	}
	written := commitOps.written[0]
	if written.lastCommit != 0 {
		t.Errorf("Expected lastCommit 0 for first commit, got %d", written.lastCommit)
	}
}

func TestCommit_MatchFailure(t *testing.T) {
	store := NewInMemoryTxStore()
	commitOps := newMockCommitOps()
	commitOps.currentCommit = 10
	commitOps.setState("foo", 10, ir.FromString("wrong"))

	state := &State{
		TxID:        1,
		CreatedAt:   time.Now(),
		PatcherData: make([]*PatcherData, 0, 1),
	}

	tx := New(store, commitOps, state)
	_ = store.Put(tx)

	patch := &api.Patch{
		Match: &api.PathData{
			Path: "foo",
			Data: ir.FromString("expected"),
		},
		PathData: api.PathData{
			Path: "foo",
			Data: ir.FromString("bar"),
		},
	}

	patcher, err := tx.NewPatcher(patch)
	if err != nil {
		t.Fatalf("NewPatcher failed: %v", err)
	}

	result := patcher.Commit()
	if result.Committed {
		t.Error("Expected committed=false, got true")
	}
	if result.Matched {
		t.Error("Expected matched=false, got true")
	}
	if result.Error != nil {
		t.Errorf("Expected no error for match failure, got %v", result.Error)
	}

	// Verify transaction was deleted
	txAfter, _ := store.Get(1)
	if txAfter != nil {
		t.Error("Expected transaction to be deleted after match failure")
	}

	// Verify no entry was written
	if len(commitOps.written) != 0 {
		t.Errorf("Expected 0 written entries, got %d", len(commitOps.written))
	}
}

func TestCommit_MultiplePatches(t *testing.T) {
	store := NewInMemoryTxStore()
	commitOps := newMockCommitOps()
	commitOps.currentCommit = 10

	// Initialize with 2 expected patchers (capacity 2, but expectedCount will be 0 initially)
	// We'll add both patchers before committing
	state := &State{
		TxID:        1,
		CreatedAt:   time.Now(),
		PatcherData: make([]*PatcherData, 0, 2),
	}

	tx := New(store, commitOps, state)
	if err := store.Put(tx); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	patch1 := &api.Patch{
		PathData: api.PathData{
			Path: "foo",
			Data: ir.FromString("bar"),
		},
	}

	patch2 := &api.Patch{
		PathData: api.PathData{
			Path: "baz",
			Data: ir.FromInt(42),
		},
	}

	_, err := tx.NewPatcher(patch1)
	if err != nil {
		t.Fatalf("NewPatcher 1 failed: %v", err)
	}

	// Add second patcher before committing
	patcher2, err := tx.NewPatcher(patch2)
	if err != nil {
		t.Fatalf("NewPatcher 2 failed: %v", err)
	}

	// Now commit should succeed (either patcher can commit)
	result := patcher2.Commit()
	if !result.Committed {
		t.Errorf("Expected committed=true, got false. Error: %v", result.Error)
	}

	// Verify merged patch contains both changes
	if len(commitOps.written) != 1 {
		t.Fatalf("Expected 1 written entry, got %d", len(commitOps.written))
	}
	written := commitOps.written[0]
	if written.mergedPatch == nil {
		t.Fatal("Expected mergedPatch to be set")
	}

	// Verify merged patch has both paths
	mergedMap := ir.ToMap(written.mergedPatch)
	if mergedMap["foo"] == nil {
		t.Error("Expected merged patch to contain 'foo'")
	}
	if mergedMap["baz"] == nil {
		t.Error("Expected merged patch to contain 'baz'")
	}
}

func TestCommit_Idempotent(t *testing.T) {
	store := NewInMemoryTxStore()
	commitOps := newMockCommitOps()
	commitOps.currentCommit = 10

	state := &State{
		TxID:        1,
		CreatedAt:   time.Now(),
		PatcherData: make([]*PatcherData, 0, 1),
	}

	tx := New(store, commitOps, state)
	_ = store.Put(tx)

	patch := &api.Patch{
		PathData: api.PathData{
			Path: "foo",
			Data: ir.FromString("bar"),
		},
	}

	patcher, err := tx.NewPatcher(patch)
	if err != nil {
		t.Fatalf("NewPatcher failed: %v", err)
	}

	result1 := patcher.Commit()
	if !result1.Committed {
		t.Fatalf("First commit failed: %v", result1.Error)
	}

	// Commit again - should be idempotent
	result2 := patcher.Commit()
	if !result2.Committed {
		t.Errorf("Second commit failed: %v", result2.Error)
	}
	if result2.Commit != result1.Commit {
		t.Errorf("Expected same commit number, got %d vs %d", result2.Commit, result1.Commit)
	}

	// Verify only one entry was written
	if len(commitOps.written) != 1 {
		t.Errorf("Expected 1 written entry (idempotent), got %d", len(commitOps.written))
	}
}

func TestCommit_NoCommitOps(t *testing.T) {
	store := NewInMemoryTxStore()
	state := &State{
		TxID:        1,
		CreatedAt:   time.Now(),
		PatcherData: make([]*PatcherData, 0, 1),
	}

	tx := New(store, nil, state) // nil commitOps
	_ = store.Put(tx)

	patch := &api.Patch{
		PathData: api.PathData{
			Path: "foo",
			Data: ir.FromString("bar"),
		},
	}

	patcher, err := tx.NewPatcher(patch)
	if err != nil {
		t.Fatalf("NewPatcher failed: %v", err)
	}

	result := patcher.Commit()
	if result.Committed {
		t.Error("Expected committed=false when commitOps is nil, got true")
	}
	if result.Error == nil {
		t.Error("Expected error when commitOps is nil, got nil")
	}
}

func TestCommit_ReadStateError(t *testing.T) {
	store := NewInMemoryTxStore()
	commitOps := &mockCommitOpsWithError{
		mockCommitOps:  *newMockCommitOps(),
		readStateError: errors.New("read state failed"),
	}
	commitOps.currentCommit = 10

	state := &State{
		TxID:        1,
		CreatedAt:   time.Now(),
		PatcherData: make([]*PatcherData, 0, 1),
	}

	tx := New(store, commitOps, state)
	_ = store.Put(tx)

	patch := &api.Patch{
		Match: &api.PathData{
			Path: "foo",
			Data: ir.FromString("expected"),
		},
		PathData: api.PathData{
			Path: "foo",
			Data: ir.FromString("bar"),
		},
	}

	patcher, err := tx.NewPatcher(patch)
	if err != nil {
		t.Fatalf("NewPatcher failed: %v", err)
	}

	result := patcher.Commit()
	if result.Committed {
		t.Error("Expected committed=false when ReadStateAt fails, got true")
	}
	if result.Error == nil {
		t.Error("Expected error when ReadStateAt fails, got nil")
	}
}

type mockCommitOpsWithError struct {
	mockCommitOps
	readStateError error
}

func (m *mockCommitOpsWithError) ReadStateAt(kp string, commit int64, scopeID *string) (*ir.Node, error) {
	if m.readStateError != nil {
		return nil, m.readStateError
	}
	return m.mockCommitOps.ReadStateAt(kp, commit, scopeID)
}

func TestCommit_Timeout(t *testing.T) {
	store := NewInMemoryTxStore()
	commitOps := newMockCommitOps()
	commitOps.currentCommit = 10

	// Create a transaction expecting 2 participants with short timeout
	state := &State{
		TxID:        1,
		CreatedAt:   time.Now(),
		Timeout:     50 * time.Millisecond, // Very short timeout for test
		PatcherData: make([]*PatcherData, 0, 2),
	}

	tx := New(store, commitOps, state)
	if err := store.Put(tx); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Only add one patcher (expecting 2)
	patch := &api.Patch{
		PathData: api.PathData{
			Path: "foo",
			Data: ir.FromString("bar"),
		},
	}

	patcher, err := tx.NewPatcher(patch)
	if err != nil {
		t.Fatalf("NewPatcher failed: %v", err)
	}

	// Commit should timeout because we only have 1 of 2 expected participants
	start := time.Now()
	result := patcher.Commit()
	elapsed := time.Since(start)

	if result.Committed {
		t.Error("Expected committed=false on timeout, got true")
	}
	if result.Error == nil {
		t.Error("Expected timeout error, got nil")
	}
	if result.Error != nil && !errors.Is(result.Error, nil) {
		// Error should mention timeout
		if !containsTimeout(result.Error.Error()) {
			t.Errorf("Expected timeout error message, got: %v", result.Error)
		}
	}

	// Should have taken about the timeout duration
	if elapsed < 40*time.Millisecond || elapsed > 200*time.Millisecond {
		t.Errorf("Expected elapsed time around 50ms, got %v", elapsed)
	}

	// Verify transaction was deleted
	txAfter, _ := store.Get(1)
	if txAfter != nil {
		t.Error("Expected transaction to be deleted after timeout")
	}
}

func containsTimeout(s string) bool {
	return len(s) > 0 && (s == "transaction timeout: not all participants joined within 50ms" ||
		len(s) >= 7 && s[:7] == "timeout" ||
		len(s) >= 11 && s[:11] == "transaction")
}

func TestCommit_NoTimeout(t *testing.T) {
	store := NewInMemoryTxStore()
	commitOps := newMockCommitOps()
	commitOps.currentCommit = 10

	// Create a transaction expecting 2 participants with no timeout
	state := &State{
		TxID:        1,
		CreatedAt:   time.Now(),
		Timeout:     0, // No timeout
		PatcherData: make([]*PatcherData, 0, 2),
	}

	tx := New(store, commitOps, state)
	if err := store.Put(tx); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Add both patchers
	patch1 := &api.Patch{
		PathData: api.PathData{
			Path: "foo",
			Data: ir.FromString("bar"),
		},
	}
	patch2 := &api.Patch{
		PathData: api.PathData{
			Path: "baz",
			Data: ir.FromString("qux"),
		},
	}

	patcher1, err := tx.NewPatcher(patch1)
	if err != nil {
		t.Fatalf("NewPatcher 1 failed: %v", err)
	}

	_, err = tx.NewPatcher(patch2)
	if err != nil {
		t.Fatalf("NewPatcher 2 failed: %v", err)
	}

	// With no timeout and all participants present, commit should succeed
	result := patcher1.Commit()
	if !result.Committed {
		t.Errorf("Expected committed=true, got false. Error: %v", result.Error)
	}
	if result.Error != nil {
		t.Errorf("Expected no error, got %v", result.Error)
	}
}
