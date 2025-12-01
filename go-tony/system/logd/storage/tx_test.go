package storage

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// createTestPatch creates a test api.Patch with the given path, diff, and optional match condition
func createTestPatch(path string, diff *ir.Node, match *ir.Node) *api.Patch {
	return &api.Patch{
		Body: api.Body{
			Path:  path,
			Patch: diff,
			Match: match,
		},
	}
}

// createTestDiffNode creates a simple test diff node
func createTestDiffNode() *ir.Node {
	return ir.FromMap(map[string]*ir.Node{
		"key": ir.FromString("value"),
	})
}

func TestNewTx(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Valid participant count
	tx, err := storage.NewTx(3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx == nil {
		t.Fatal("NewTx returned nil")
	}
	if tx.ID() == 0 {
		t.Error("transaction ID should not be zero")
	}

	// Invalid participant count
	_, err = storage.NewTx(0)
	if err == nil {
		t.Error("expected error for participantCount=0")
	}
	_, err = storage.NewTx(-1)
	if err == nil {
		t.Error("expected error for participantCount=-1")
	}

	// Verify state file is created
	state, err := storage.ReadTxState(tx.ID())
	if err != nil {
		t.Fatalf("failed to read transaction state: %v", err)
	}
	if state.ParticipantCount != 3 {
		t.Errorf("expected ParticipantCount 3, got %d", state.ParticipantCount)
	}
	if state.Status != "pending" {
		t.Errorf("expected Status 'pending', got %q", state.Status)
	}
}

func TestGetTx(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Create transaction
	tx1, err := storage.NewTx(2)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	txID := tx1.ID()

	// Get existing transaction
	tx2, err := storage.GetTx(txID)
	if err != nil {
		t.Fatalf("failed to get transaction: %v", err)
	}
	if tx2.ID() != txID {
		t.Errorf("expected transaction ID %d, got %d", txID, tx2.ID())
	}

	// Non-existent transaction
	_, err = storage.GetTx(99999)
	if err == nil {
		t.Error("expected error for non-existent transaction")
	}

	// Committed transaction
	err = storage.UpdateTxState(txID, func(state *TxState) {
		state.Status = "committed"
	})
	if err != nil {
		t.Fatalf("failed to update state: %v", err)
	}
	_, err = storage.GetTx(txID)
	if err == nil {
		t.Error("expected error for committed transaction")
	}
}

func TestAddPatch(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	tx, err := storage.NewTx(2)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	patcher := tx.NewPatcher()

	// Add patch
	patch := createTestPatch("/test/path", createTestDiffNode(), nil)
	isLast, err := patcher.AddPatch(patch)
	if err != nil {
		t.Fatalf("failed to add patch: %v", err)
	}
	if isLast {
		t.Error("expected isLast=false (1 of 2 participants)")
	}

	// Verify state updated
	state, err := storage.ReadTxState(tx.ID())
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}
	if state.ParticipantsReceived != 1 {
		t.Errorf("expected ParticipantsReceived 1, got %d", state.ParticipantsReceived)
	}
	if len(state.FileMetas) != 1 {
		t.Errorf("expected 1 diff, got %d", len(state.FileMetas))
	}

	// Error cases
	_, err = patcher.AddPatch(&api.Patch{Body: api.Body{Path: ""}})
	if err == nil || !strings.Contains(err.Error(), "Path is required") {
		t.Errorf("expected error for missing path, got: %v", err)
	}

	_, err = patcher.AddPatch(&api.Patch{Body: api.Body{Path: "/path", Patch: nil}})
	if err == nil || !strings.Contains(err.Error(), "Patch is required") {
		t.Errorf("expected error for missing patch, got: %v", err)
	}
}

func TestAddPatch_WithMatchCondition(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	tx, err := storage.NewTx(1)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	patcher := tx.NewPatcher()

	match := ir.FromMap(map[string]*ir.Node{
		"field": ir.FromString("value"),
	})
	patch := createTestPatch("/test/path", createTestDiffNode(), match)

	_, err = patcher.AddPatch(patch)
	if err != nil {
		t.Fatalf("failed to add patch: %v", err)
	}

	// Verify match condition stored
	state, err := storage.ReadTxState(tx.ID())
	if err != nil {
		t.Fatalf("failed to read state: %v", err)
	}
	if len(state.ParticipantMatches) != 1 {
		t.Errorf("expected 1 match condition, got %d", len(state.ParticipantMatches))
	}
}

func TestLastParticipantDetection(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Sequential
	tx1, err := storage.NewTx(3)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	txID := tx1.ID()

	tx2, err := storage.GetTx(txID)
	if err != nil {
		t.Fatalf("failed to get transaction: %v", err)
	}
	tx3, err := storage.GetTx(txID)
	if err != nil {
		t.Fatalf("failed to get transaction: %v", err)
	}

	patcher1 := tx1.NewPatcher()
	patcher2 := tx2.NewPatcher()
	patcher3 := tx3.NewPatcher()

	isLast1, _ := patcher1.AddPatch(createTestPatch("/path1", createTestDiffNode(), nil))
	isLast2, _ := patcher2.AddPatch(createTestPatch("/path2", createTestDiffNode(), nil))
	isLast3, _ := patcher3.AddPatch(createTestPatch("/path3", createTestDiffNode(), nil))

	if isLast1 || isLast2 {
		t.Error("first two participants should not be last")
	}
	if !isLast3 {
		t.Error("third participant should be last")
	}

	// Concurrent
	tx4, err := storage.NewTx(5)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	txID2 := tx4.ID()

	var wg sync.WaitGroup
	results := make([]bool, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tx, err := storage.GetTx(txID2)
			if err != nil {
				t.Errorf("failed to get transaction: %v", err)
				return
			}
			patcher := tx.NewPatcher()
			patch := createTestPatch(fmt.Sprintf("/path%d", idx), createTestDiffNode(), nil)
			isLast, err := patcher.AddPatch(patch)
			if err != nil {
				t.Errorf("failed to add patch: %v", err)
				return
			}
			results[idx] = isLast
		}(i)
	}
	wg.Wait()

	lastCount := 0
	for _, isLast := range results {
		if isLast {
			lastCount++
		}
	}
	if lastCount != 1 {
		t.Errorf("expected exactly 1 last participant, got %d", lastCount)
	}
}

func TestCommit(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	tx, err := storage.NewTx(1)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	patcher := tx.NewPatcher()

	patch := createTestPatch("/test/path", createTestDiffNode(), nil)
	isLast, err := patcher.AddPatch(patch)
	if err != nil {
		t.Fatalf("failed to add patch: %v", err)
	}
	if !isLast {
		t.Fatal("single participant should be last")
	}

	result := patcher.Commit()
	if !result.Committed {
		t.Error("transaction should be committed")
	}
	if result.Commit == 0 {
		t.Error("commit number should be non-zero")
	}
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestWaitForCompletion(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	tx1, err := storage.NewTx(2)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	txID := tx1.ID()

	tx2, err := storage.GetTx(txID)
	if err != nil {
		t.Fatalf("failed to get transaction: %v", err)
	}

	patcher1 := tx1.NewPatcher()
	patcher2 := tx2.NewPatcher()

	// Start waiter
	var result *TxResult
	done := make(chan bool)
	go func() {
		result = patcher2.WaitForCompletion()
		done <- true
	}()

	// Add patches
	patcher1.AddPatch(createTestPatch("/path1", createTestDiffNode(), nil))
	isLast, _ := patcher2.AddPatch(createTestPatch("/path2", createTestDiffNode(), nil))
	if !isLast {
		t.Fatal("second participant should be last")
	}

	// Commit
	commitResult := patcher2.Commit()

	// Wait for completion
	<-done

	if result == nil {
		t.Fatal("result should not be nil")
	}
	if !result.Committed {
		t.Error("transaction should be committed")
	}
	if result.Commit != commitResult.Commit {
		t.Errorf("wait result commit %d != commit result commit %d", result.Commit, commitResult.Commit)
	}
}

func TestGetResult(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	tx, err := storage.NewTx(1)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	patcher := tx.NewPatcher()

	// Before completion
	if patcher.GetResult() != nil {
		t.Error("GetResult should return nil before completion")
	}

	// Add and commit
	patch := createTestPatch("/path", createTestDiffNode(), nil)
	isLast, _ := patcher.AddPatch(patch)
	if !isLast {
		t.Fatal("single participant should be last")
	}

	commitResult := patcher.Commit()

	// After completion
	result := patcher.GetResult()
	if result == nil {
		t.Fatal("GetResult should return result after commit")
	}
	if result.Commit != commitResult.Commit {
		t.Errorf("GetResult commit %d != commit result commit %d", result.Commit, commitResult.Commit)
	}
}
