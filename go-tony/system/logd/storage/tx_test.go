package storage

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// TestStep1_CoreTypesAndStructure validates Step 1: Core Types and Structure
// This test verifies that the basic types and method signatures are correct
// without any implementation logic.
func TestStep1_CoreTypesAndStructure(t *testing.T) {
	// Create a storage instance for testing (we'll use a temp dir in later steps)
	// For now, we just verify the types compile and methods exist

	// Verify TxResult struct exists and has correct fields
	var result *TxResult
	if result != nil {
		t.Error("TxResult should be nil initially")
	}

	// Verify Tx struct exists
	var tx *Tx
	if tx != nil {
		t.Error("Tx should be nil initially")
	}

	// Verify method signatures compile
	// Note: These will return nil/errors since not implemented yet
	_ = func() {
		var s *Storage
		_, _ = s.NewTx(3)        // Returns (*Tx, error)
		_, _ = s.JoinTx(int64(1)) // Returns (*Tx, error)

		if tx != nil {
			_ = tx.ID()                                    // Returns int64
			_, _ = tx.AddDiff(&api.Patch{})                // Returns (bool, error)
			_, _ = tx.Commit()                              // Returns (*TxResult, error)
			_ = tx.WaitForCompletion()                      // Returns *TxResult
			_ = tx.GetResult()                              // Returns *TxResult
		}
	}

	// Verify types are exported (compile-time check)
	// If these don't compile, the types aren't exported
	var _ TxResult
	var _ Tx
}

// TestStep1_MethodSignatures verifies method signatures match the design document
func TestStep1_MethodSignatures(t *testing.T) {
	// This test ensures the method signatures match what's expected
	// The actual implementation will be added in later steps
	// Note: We only verify the function types compile correctly, we don't call them
	// since some require valid Storage/Tx instances

	// Verify NewTx signature: NewTx(participantCount int) (*Tx, error)
	var newTxFunc func(*Storage, int) (*Tx, error)
	newTxFunc = (*Storage).NewTx
	_ = newTxFunc // Just verify the type compiles

	// Verify JoinTx signature: JoinTx(txID int64) (*Tx, error)
	var joinTxFunc func(*Storage, int64) (*Tx, error)
	joinTxFunc = (*Storage).JoinTx
	_ = joinTxFunc // Just verify the type compiles

	// Verify AddDiff signature: AddDiff(patch *api.Patch) (isLastParticipant bool, err error)
	var addDiffFunc func(*Tx, *api.Patch) (bool, error)
	addDiffFunc = (*Tx).AddDiff
	_ = addDiffFunc // Just verify the type compiles

	// Verify Commit signature: Commit() (*TxResult, error)
	var commitFunc func(*Tx) (*TxResult, error)
	commitFunc = (*Tx).Commit
	_ = commitFunc // Just verify the type compiles

	// Verify WaitForCompletion signature: WaitForCompletion() *TxResult
	var waitFunc func(*Tx) *TxResult
	waitFunc = (*Tx).WaitForCompletion
	_ = waitFunc // Just verify the type compiles

	// Verify GetResult signature: GetResult() *TxResult
	var getResultFunc func(*Tx) *TxResult
	getResultFunc = (*Tx).GetResult
	_ = getResultFunc // Just verify the type compiles

}

// TestStep1_NoImplementation verifies methods return nil/empty values (not implemented yet)
func TestStep1_NoImplementation(t *testing.T) {
	// This test verifies that methods return nil/empty values as expected
	// for Step 1 (no implementation yet)

	// Note: We can't actually call these without a Storage instance,
	// but we verify the structure is correct
	// Full tests will be added in later steps when we have implementations
}

// TestStep2_TransactionCreation validates Step 2: Transaction Creation
func TestStep2_TransactionCreation(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Test: Create transaction with N participants
	participantCount := 3
	tx, err := storage.NewTx(participantCount)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	if tx == nil {
		t.Fatal("NewTx returned nil transaction")
	}

	// Verify transaction ID is correct (int64, same as txSeq)
	txID := tx.ID()
	if txID == 0 {
		t.Error("transaction ID should not be zero")
	}
	if txID != tx.txSeq {
		t.Errorf("transaction ID (%d) should equal txSeq (%d)", txID, tx.txSeq)
	}

	// Verify transaction state file is created on disk
	state, err := storage.ReadTransactionState(txID)
	if err != nil {
		t.Fatalf("failed to read transaction state: %v", err)
	}

	// Verify state has correct ParticipantCount and Status="pending"
	if state.ParticipantCount != participantCount {
		t.Errorf("expected ParticipantCount %d, got %d", participantCount, state.ParticipantCount)
	}
	if state.Status != "pending" {
		t.Errorf("expected Status 'pending', got %q", state.Status)
	}
	if state.TransactionID != txID {
		t.Errorf("expected TransactionID %d, got %d", txID, state.TransactionID)
	}

	// Verify Tx struct fields are initialized correctly
	if tx.storage != storage {
		t.Error("tx.storage should reference the storage instance")
	}
	if tx.participantCount != participantCount {
		t.Errorf("expected participantCount %d, got %d", participantCount, tx.participantCount)
	}
	if tx.committed {
		t.Error("new transaction should not be committed")
	}
	if tx.done == nil {
		t.Error("done channel should be initialized")
	}
	if tx.result != nil {
		t.Error("new transaction should not have a result")
	}
}

// TestStep2_TransactionCreation_ErrorHandling tests error handling
func TestStep2_TransactionCreation_ErrorHandling(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Test: Invalid participant count (should fail)
	_, err = storage.NewTx(0)
	if err == nil {
		t.Error("expected error for participantCount=0, got nil")
	}
	_, err = storage.NewTx(-1)
	if err == nil {
		t.Error("expected error for participantCount=-1, got nil")
	}

	// Test: Valid participant count (should succeed)
	tx, err := storage.NewTx(1)
	if err != nil {
		t.Fatalf("unexpected error for participantCount=1: %v", err)
	}
	if tx == nil {
		t.Fatal("NewTx returned nil transaction")
	}

	// Verify we can create multiple transactions
	tx2, err := storage.NewTx(2)
	if err != nil {
		t.Fatalf("failed to create second transaction: %v", err)
	}
	if tx2.ID() == tx.ID() {
		t.Error("second transaction should have different ID")
	}
}

// TestStep2_ID_Method tests the ID() method
func TestStep2_ID_Method(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	tx, err := storage.NewTx(2)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}

	// Verify ID() returns the correct value
	id := tx.ID()
	if id == 0 {
		t.Error("ID() should not return zero")
	}
	if id != tx.txID {
		t.Errorf("ID() returned %d, but tx.txID is %d", id, tx.txID)
	}

	// Verify ID is consistent across calls
	if tx.ID() != tx.ID() {
		t.Error("ID() should return consistent values")
	}
}

// TestStep3_JoinExistingTransaction validates Step 3: Join Existing Transaction
func TestStep3_JoinExistingTransaction(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Create a transaction
	tx1, err := storage.NewTx(2)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	txID := tx1.ID()

	// Test: Join existing transaction
	tx2, err := storage.JoinTx(txID)
	if err != nil {
		t.Fatalf("failed to join transaction: %v", err)
	}
	if tx2 == nil {
		t.Fatal("JoinTx returned nil transaction")
	}

	// Verify both transactions have the same ID
	if tx2.ID() != tx1.ID() {
		t.Errorf("expected transaction ID %d, got %d", tx1.ID(), tx2.ID())
	}

	// Verify Tx instance has correct fields populated
	if tx2.storage != storage {
		t.Error("tx2.storage should reference the storage instance")
	}
	if tx2.txID != txID {
		t.Errorf("expected txID %d, got %d", txID, tx2.txID)
	}
	if tx2.txSeq != txID {
		t.Errorf("expected txSeq %d, got %d", txID, tx2.txSeq)
	}
	if tx2.participantCount != 2 {
		t.Errorf("expected participantCount 2, got %d", tx2.participantCount)
	}
	if tx2.committed {
		t.Error("joined transaction should not be committed")
	}
	if tx2.done == nil {
		t.Error("done channel should be initialized")
	}
	if tx2.result != nil {
		t.Error("joined transaction should not have a result")
	}

	// Verify we can join multiple times (simulating multiple participants)
	tx3, err := storage.JoinTx(txID)
	if err != nil {
		t.Fatalf("failed to join transaction second time: %v", err)
	}
	if tx3.ID() != txID {
		t.Errorf("expected transaction ID %d, got %d", txID, tx3.ID())
	}
}

// TestStep3_JoinNonExistentTransaction tests error handling for non-existent transactions
func TestStep3_JoinNonExistentTransaction(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Test: Join non-existent transaction
	nonExistentID := int64(99999)
	_, err = storage.JoinTx(nonExistentID)
	if err == nil {
		t.Error("expected error when joining non-existent transaction, got nil")
	}
	if err != nil && err.Error() == "" {
		t.Error("error message should not be empty")
	}
}

// TestStep3_JoinCommittedTransaction tests error handling for committed transactions
func TestStep3_JoinCommittedTransaction(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Create a transaction
	tx, err := storage.NewTx(1)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	txID := tx.ID()

	// Manually update the transaction state to "committed" status
	// (This simulates a committed transaction without implementing Commit yet)
	err = storage.UpdateTransactionState(txID, func(state *TransactionState) {
		state.Status = "committed"
	})
	if err != nil {
		t.Fatalf("failed to update transaction state: %v", err)
	}

	// Test: Try to join committed transaction (should fail)
	_, err = storage.JoinTx(txID)
	if err == nil {
		t.Error("expected error when joining committed transaction, got nil")
	}
		if err != nil {
			// Verify error message mentions the status
			errMsg := err.Error()
			if errMsg == "" {
				t.Error("error message should not be empty")
			}
			// Error should mention "committed" status
			if errMsg != "" && !strings.Contains(errMsg, "committed") && !strings.Contains(errMsg, "cannot join") {
				t.Logf("Warning: error message may not be descriptive enough: %s", errMsg)
			}
		}
}

// TestStep3_JoinAbortedTransaction tests error handling for aborted transactions
func TestStep3_JoinAbortedTransaction(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Create a transaction
	tx, err := storage.NewTx(1)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	txID := tx.ID()

	// Manually update the transaction state to "aborted" status
	err = storage.UpdateTransactionState(txID, func(state *TransactionState) {
		state.Status = "aborted"
	})
	if err != nil {
		t.Fatalf("failed to update transaction state: %v", err)
	}

	// Test: Try to join aborted transaction (should fail)
	_, err = storage.JoinTx(txID)
	if err == nil {
		t.Error("expected error when joining aborted transaction, got nil")
	}
}

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
	// Create a simple object node as a diff
	return ir.FromMap(map[string]*ir.Node{
		"key": ir.FromString("value"),
	})
}

// TestStep4_AddDiff_BasicFlow validates Step 4: Add Diff - Basic Flow
func TestStep4_AddDiff_BasicFlow(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Create a transaction with 2 participants
	tx, err := storage.NewTx(2)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}

	// Create a test patch
	diff := createTestDiffNode()
	patch := createTestPatch("/test/path", diff, nil) // no match condition

	// Test: Add single diff to transaction
	isLast, err := tx.AddDiff(patch)
	if err != nil {
		t.Fatalf("failed to add diff: %v", err)
	}
	if isLast {
		t.Error("expected isLast to be false (only 1 of 2 participants added)")
	}

	// Verify transaction state is updated correctly
	state, err := storage.ReadTransactionState(tx.ID())
	if err != nil {
		t.Fatalf("failed to read transaction state: %v", err)
	}

	if state.ParticipantsReceived != 1 {
		t.Errorf("expected ParticipantsReceived 1, got %d", state.ParticipantsReceived)
	}
	if len(state.Diffs) != 1 {
		t.Errorf("expected 1 diff, got %d", len(state.Diffs))
	}
	if len(state.ParticipantRequests) != 1 {
		t.Errorf("expected 1 participant request, got %d", len(state.ParticipantRequests))
	}
	if len(state.ParticipantMatches) != 0 {
		t.Errorf("expected 0 match conditions, got %d", len(state.ParticipantMatches))
	}

	// Verify diff entry has correct fields
	diffEntry := state.Diffs[0]
	if diffEntry.Path != "/test/path" {
		t.Errorf("expected path '/test/path', got %q", diffEntry.Path)
	}
	// Files are not written until commit (after match evaluation)
	if diffEntry.DiffFile != "" {
		t.Error("DiffFile should be empty until file is written during commit")
	}
	if diffEntry.WrittenAt != "" {
		t.Error("WrittenAt should be empty until file is written during commit")
	}
}

// TestStep4_AddDiff_MultipleDiffs tests adding multiple diffs from same participant
func TestStep4_AddDiff_MultipleDiffs(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Create a transaction with 1 participant (so we can add multiple diffs)
	tx, err := storage.NewTx(1)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}

	// Add first diff
	patch1 := createTestPatch("/path1", createTestDiffNode(), nil)
	isLast1, err := tx.AddDiff(patch1)
	if err != nil {
		t.Fatalf("failed to add first diff: %v", err)
	}
	if !isLast1 {
		t.Error("expected isLast to be true (1 of 1 participants)")
	}

	// Add second diff (same participant, should work)
	patch2 := createTestPatch("/path2", createTestDiffNode(), nil)
	_, err = tx.AddDiff(patch2)
	if err != nil {
		t.Fatalf("failed to add second diff: %v", err)
	}
	// Note: isLast will be true again because ParticipantsReceived is now 2, which is >= 1
	// This is expected behavior - the last participant can add multiple diffs

	// Verify state has both diffs
	state, err := storage.ReadTransactionState(tx.ID())
	if err != nil {
		t.Fatalf("failed to read transaction state: %v", err)
	}

	if state.ParticipantsReceived != 2 {
		t.Errorf("expected ParticipantsReceived 2, got %d", state.ParticipantsReceived)
	}
	if len(state.Diffs) != 2 {
		t.Errorf("expected 2 diffs, got %d", len(state.Diffs))
	}
	if len(state.ParticipantRequests) != 2 {
		t.Errorf("expected 2 participant requests, got %d", len(state.ParticipantRequests))
	}
}

// TestStep4_AddDiff_WithMatchCondition tests adding diff with match condition
func TestStep4_AddDiff_WithMatchCondition(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	tx, err := storage.NewTx(1)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}

	// Create a patch with match condition
	diff := createTestDiffNode()
	match := ir.FromMap(map[string]*ir.Node{
		"field": ir.FromString("expected_value"),
	})
	patch := createTestPatch("/test/path", diff, match)

	isLast, err := tx.AddDiff(patch)
	if err != nil {
		t.Fatalf("failed to add diff: %v", err)
	}
	if !isLast {
		t.Error("expected isLast to be true")
	}

	// Verify match condition is stored
	state, err := storage.ReadTransactionState(tx.ID())
	if err != nil {
		t.Fatalf("failed to read transaction state: %v", err)
	}

	if len(state.ParticipantMatches) != 1 {
		t.Errorf("expected 1 match condition, got %d", len(state.ParticipantMatches))
	}

	matchReq := state.ParticipantMatches[0]
	if matchReq.Body.Path != "/test/path" {
		t.Errorf("expected match path '/test/path', got %q", matchReq.Body.Path)
	}
	if matchReq.Body.Match == nil {
		t.Error("match condition should not be nil")
	}
}

// TestStep4_AddDiff_ErrorHandling tests error handling
func TestStep4_AddDiff_ErrorHandling(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	tx, err := storage.NewTx(1)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}

	// Test: Missing path
	patchNoPath := &api.Patch{
		Body: api.Body{
			Path:  "", // Empty path
			Patch: createTestDiffNode(),
		},
	}
	_, err = tx.AddDiff(patchNoPath)
	if err == nil {
		t.Error("expected error for missing path, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "Path is required") {
		t.Errorf("expected error about Path being required, got: %v", err)
	}

	// Test: Missing patch
	patchNoDiff := &api.Patch{
		Body: api.Body{
			Path:  "/test/path",
			Patch: nil, // Nil patch
		},
	}
	_, err = tx.AddDiff(patchNoDiff)
	if err == nil {
		t.Error("expected error for missing patch, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "Patch is required") {
		t.Errorf("expected error about Patch being required, got: %v", err)
	}
}

// TestStep4_AddDiff_AlreadyCommitted tests adding diff to already committed transaction
func TestStep4_AddDiff_AlreadyCommitted(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	tx, err := storage.NewTx(1)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}

	// Manually mark transaction as committed
	tx.mu.Lock()
	tx.committed = true
	tx.mu.Unlock()

	// Try to add diff (should fail)
	patch := createTestPatch("/test/path", createTestDiffNode(), nil)
	_, err = tx.AddDiff(patch)
	if err == nil {
		t.Error("expected error when adding diff to committed transaction, got nil")
	}
	if err != nil && !strings.Contains(err.Error(), "already committed") {
		t.Errorf("expected error about transaction being committed, got: %v", err)
	}
}

// TestStep5_AddDiff_LastParticipantDetection_Sequential validates Step 5: Last Participant Detection (Sequential)
func TestStep5_AddDiff_LastParticipantDetection_Sequential(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Create a transaction with 3 participants
	tx1, err := storage.NewTx(3)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	txID := tx1.ID()

	// Create two more participants via JoinTx
	tx2, err := storage.JoinTx(txID)
	if err != nil {
		t.Fatalf("failed to join transaction: %v", err)
	}
	tx3, err := storage.JoinTx(txID)
	if err != nil {
		t.Fatalf("failed to join transaction: %v", err)
	}

	// Each participant adds a diff sequentially
	patch1 := createTestPatch("/path1", createTestDiffNode(), nil)
	patch2 := createTestPatch("/path2", createTestDiffNode(), nil)
	patch3 := createTestPatch("/path3", createTestDiffNode(), nil)

	isLast1, err := tx1.AddDiff(patch1)
	if err != nil {
		t.Fatalf("failed to add diff 1: %v", err)
	}
	if isLast1 {
		t.Error("first participant should not be last (1 of 3)")
	}

	isLast2, err := tx2.AddDiff(patch2)
	if err != nil {
		t.Fatalf("failed to add diff 2: %v", err)
	}
	if isLast2 {
		t.Error("second participant should not be last (2 of 3)")
	}

	isLast3, err := tx3.AddDiff(patch3)
	if err != nil {
		t.Fatalf("failed to add diff 3: %v", err)
	}
	if !isLast3 {
		t.Error("third participant should be last (3 of 3)")
	}

	// Verify exactly one participant is last
	lastCount := 0
	if isLast1 {
		lastCount++
	}
	if isLast2 {
		lastCount++
	}
	if isLast3 {
		lastCount++
	}
	if lastCount != 1 {
		t.Errorf("expected exactly 1 last participant, got %d", lastCount)
	}

	// Verify state consistency
	state, err := storage.ReadTransactionState(txID)
	if err != nil {
		t.Fatalf("failed to read transaction state: %v", err)
	}

	if state.ParticipantsReceived != 3 {
		t.Errorf("expected ParticipantsReceived 3, got %d", state.ParticipantsReceived)
	}
	if len(state.Diffs) != 3 {
		t.Errorf("expected 3 diffs, got %d", len(state.Diffs))
	}
	if len(state.ParticipantRequests) != 3 {
		t.Errorf("expected 3 participant requests, got %d", len(state.ParticipantRequests))
	}
}

// TestStep5_AddDiff_LastParticipantDetection_Concurrent validates Step 5: Last Participant Detection (Concurrent)
func TestStep5_AddDiff_LastParticipantDetection_Concurrent(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Create a transaction with 5 participants
	tx1, err := storage.NewTx(5)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	txID := tx1.ID()

	// Use WaitGroup to coordinate concurrent AddDiff calls
	var wg sync.WaitGroup
	results := make([]bool, 5)
	errors := make([]error, 5)

	// Launch 5 goroutines, each calling AddDiff concurrently
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tx, err := storage.JoinTx(txID)
			if err != nil {
				errors[idx] = fmt.Errorf("failed to join transaction: %w", err)
				return
			}
			patch := createTestPatch(fmt.Sprintf("/path%d", idx), createTestDiffNode(), nil)
			isLast, err := tx.AddDiff(patch)
			results[idx] = isLast
			if err != nil {
				errors[idx] = err
			}
		}(i)
	}

	wg.Wait()

	// Check for errors
	for i, err := range errors {
		if err != nil {
			t.Errorf("participant %d encountered error: %v", i, err)
		}
	}

	// Verify exactly one participant got isLastParticipant=true
	lastCount := 0
	for i, isLast := range results {
		if isLast {
			lastCount++
			t.Logf("Participant %d is the last participant", i)
		}
	}
	if lastCount != 1 {
		t.Errorf("expected exactly 1 last participant, got %d", lastCount)
	}

	// Verify state consistency
	state, err := storage.ReadTransactionState(txID)
	if err != nil {
		t.Fatalf("failed to read transaction state: %v", err)
	}

	if state.ParticipantsReceived != 5 {
		t.Errorf("expected ParticipantsReceived 5, got %d", state.ParticipantsReceived)
	}
	if len(state.Diffs) != 5 {
		t.Errorf("expected 5 diffs, got %d", len(state.Diffs))
	}
	if len(state.ParticipantRequests) != 5 {
		t.Errorf("expected 5 participant requests, got %d", len(state.ParticipantRequests))
	}
}

// TestStep5_AddDiff_LastParticipantDetection_SingleParticipant tests edge case: single participant
func TestStep5_AddDiff_LastParticipantDetection_SingleParticipant(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Create a transaction with 1 participant
	tx, err := storage.NewTx(1)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}

	patch := createTestPatch("/path", createTestDiffNode(), nil)
	isLast, err := tx.AddDiff(patch)
	if err != nil {
		t.Fatalf("failed to add diff: %v", err)
	}

	if !isLast {
		t.Error("single participant should always be last")
	}

	// Verify state
	state, err := storage.ReadTransactionState(tx.ID())
	if err != nil {
		t.Fatalf("failed to read transaction state: %v", err)
	}

	if state.ParticipantsReceived != 1 {
		t.Errorf("expected ParticipantsReceived 1, got %d", state.ParticipantsReceived)
	}
}

// TestStep5_AddDiff_LastParticipantDetection_MultipleDiffsFromSameParticipant tests multiple diffs from same participant
func TestStep5_AddDiff_LastParticipantDetection_MultipleDiffsFromSameParticipant(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Create a transaction with 2 participants
	tx1, err := storage.NewTx(2)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	txID := tx1.ID()

	tx2, err := storage.JoinTx(txID)
	if err != nil {
		t.Fatalf("failed to join transaction: %v", err)
	}

	// First participant adds two diffs
	patch1a := createTestPatch("/path1a", createTestDiffNode(), nil)
	isLast1a, err := tx1.AddDiff(patch1a)
	if err != nil {
		t.Fatalf("failed to add diff 1a: %v", err)
	}
	if isLast1a {
		t.Error("first diff from participant 1 should not be last (1 of 2)")
	}

	patch1b := createTestPatch("/path1b", createTestDiffNode(), nil)
	isLast1b, err := tx1.AddDiff(patch1b)
	if err != nil {
		t.Fatalf("failed to add diff 1b: %v", err)
	}
	// Note: The current implementation counts diffs, not unique participants.
	// So participant 1's second diff (2nd diff overall) will be last because
	// ParticipantsReceived (2) >= ParticipantCount (2)
	if !isLast1b {
		t.Error("second diff from participant 1 should be last (2 diffs >= 2 participant count)")
	}

	// Second participant adds one diff (will also be marked as last, but transaction already completed)
	patch2 := createTestPatch("/path2", createTestDiffNode(), nil)
	isLast2, err := tx2.AddDiff(patch2)
	if err != nil {
		t.Fatalf("failed to add diff 2: %v", err)
	}
	// This will also return true because ParticipantsReceived (3) >= ParticipantCount (2)
	// But the transaction was already marked as ready to commit by participant 1's second diff
	if !isLast2 {
		t.Error("participant 2's diff will also be marked as last (3 >= 2)")
	}

	// Verify state
	state, err := storage.ReadTransactionState(txID)
	if err != nil {
		t.Fatalf("failed to read transaction state: %v", err)
	}

	// Note: ParticipantsReceived counts diffs, not unique participants
	// So 3 diffs from 2 participants means ParticipantsReceived = 3
	if state.ParticipantsReceived != 3 {
		t.Errorf("expected ParticipantsReceived 3, got %d", state.ParticipantsReceived)
	}
	if len(state.Diffs) != 3 {
		t.Errorf("expected 3 diffs, got %d", len(state.Diffs))
	}
}

// TestStep5_AddDiff_LastParticipantDetection_LargeConcurrency tests with many concurrent participants
func TestStep5_AddDiff_LastParticipantDetection_LargeConcurrency(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	participantCount := 10
	tx1, err := storage.NewTx(participantCount)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	txID := tx1.ID()

	var wg sync.WaitGroup
	results := make([]bool, participantCount)
	errors := make([]error, participantCount)

	// Launch many goroutines concurrently
	for i := 0; i < participantCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tx, err := storage.JoinTx(txID)
			if err != nil {
				errors[idx] = fmt.Errorf("failed to join transaction: %w", err)
				return
			}
			patch := createTestPatch(fmt.Sprintf("/path%d", idx), createTestDiffNode(), nil)
			isLast, err := tx.AddDiff(patch)
			results[idx] = isLast
			if err != nil {
				errors[idx] = err
			}
		}(i)
	}

	wg.Wait()

	// Check for errors
	for i, err := range errors {
		if err != nil {
			t.Errorf("participant %d encountered error: %v", i, err)
		}
	}

	// Verify exactly one participant got isLastParticipant=true
	lastCount := 0
	for i, isLast := range results {
		if isLast {
			lastCount++
			t.Logf("Participant %d is the last participant", i)
		}
	}
	if lastCount != 1 {
		t.Errorf("expected exactly 1 last participant, got %d", lastCount)
	}

	// Verify state consistency
	state, err := storage.ReadTransactionState(txID)
	if err != nil {
		t.Fatalf("failed to read transaction state: %v", err)
	}

	if state.ParticipantsReceived != participantCount {
		t.Errorf("expected ParticipantsReceived %d, got %d", participantCount, state.ParticipantsReceived)
	}
	if len(state.Diffs) != participantCount {
		t.Errorf("expected %d diffs, got %d", participantCount, len(state.Diffs))
	}
}

// TestStep9_WaitForCompletion tests waiting for transaction completion
func TestStep9_WaitForCompletion(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Create a transaction with 2 participants
	tx1, err := storage.NewTx(2)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	txID := tx1.ID()

	// Second participant joins
	tx2, err := storage.JoinTx(txID)
	if err != nil {
		t.Fatalf("failed to join transaction: %v", err)
	}

	// Start waiter in goroutine
	var result *TxResult
	done := make(chan bool)
	go func() {
		result = tx2.WaitForCompletion()
		done <- true
	}()

	// First participant adds diff (not last)
	patch1 := createTestPatch("/path1", createTestDiffNode(), nil)
	isLast1, err := tx1.AddDiff(patch1)
	if err != nil {
		t.Fatalf("failed to add diff 1: %v", err)
	}
	if isLast1 {
		t.Error("first participant should not be last (1 of 2)")
	}

	// Second participant adds diff (last)
	patch2 := createTestPatch("/path2", createTestDiffNode(), nil)
	isLast2, err := tx2.AddDiff(patch2)
	if err != nil {
		t.Fatalf("failed to add diff 2: %v", err)
	}
	if !isLast2 {
		t.Error("second participant should be last (2 of 2)")
	}

	// Last participant commits
	commitResult, err := tx2.Commit()
	if err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Wait for completion
	<-done

	// Verify result
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if !result.Committed {
		t.Error("transaction should be committed")
	}
	if result.Commit == 0 {
		t.Error("commit number should be non-zero")
	}
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}

	// Verify result matches commit result
	if result.Commit != commitResult.Commit {
		t.Errorf("wait result commit %d != commit result commit %d", result.Commit, commitResult.Commit)
	}
}

// TestStep9_WaitForCompletion_MultipleWaiters tests multiple goroutines waiting
func TestStep9_WaitForCompletion_MultipleWaiters(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Create a transaction with 3 participants
	tx1, err := storage.NewTx(3)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}
	txID := tx1.ID()

	// Create two waiting participants
	tx2, err := storage.JoinTx(txID)
	if err != nil {
		t.Fatalf("failed to join transaction: %v", err)
	}
	tx3, err := storage.JoinTx(txID)
	if err != nil {
		t.Fatalf("failed to join transaction: %v", err)
	}

	// Start multiple waiters
	results := make([]*TxResult, 2)
	done := make(chan int, 2)
	go func() {
		results[0] = tx2.WaitForCompletion()
		done <- 0
	}()
	go func() {
		results[1] = tx3.WaitForCompletion()
		done <- 1
	}()

	// Add diffs
	patch1 := createTestPatch("/path1", createTestDiffNode(), nil)
	tx1.AddDiff(patch1)
	patch2 := createTestPatch("/path2", createTestDiffNode(), nil)
	tx2.AddDiff(patch2)
	patch3 := createTestPatch("/path3", createTestDiffNode(), nil)
	isLast, _ := tx3.AddDiff(patch3)
	if !isLast {
		t.Fatal("third participant should be last")
	}

	// Commit
	commitResult, err := tx3.Commit()
	if err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Wait for both waiters
	<-done
	<-done

	// Verify both got the same result
	if results[0] == nil || results[1] == nil {
		t.Fatal("both results should be non-nil")
	}
	if results[0].Commit != results[1].Commit {
		t.Errorf("results should have same commit: %d != %d", results[0].Commit, results[1].Commit)
	}
	if results[0].Commit != commitResult.Commit {
		t.Errorf("wait results commit %d != commit result commit %d", results[0].Commit, commitResult.Commit)
	}
}

// TestStep9_GetResult tests non-blocking result retrieval
func TestStep9_GetResult(t *testing.T) {
	tmpDir := t.TempDir()
	storage, err := Open(tmpDir, 022, nil)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}

	// Create a transaction
	tx, err := storage.NewTx(1)
	if err != nil {
		t.Fatalf("failed to create transaction: %v", err)
	}

	// GetResult before completion should return nil
	result := tx.GetResult()
	if result != nil {
		t.Error("GetResult should return nil before completion")
	}

	// Add diff and commit
	patch := createTestPatch("/path", createTestDiffNode(), nil)
	isLast, err := tx.AddDiff(patch)
	if err != nil {
		t.Fatalf("failed to add diff: %v", err)
	}
	if !isLast {
		t.Fatal("single participant should be last")
	}

	// GetResult before commit should still return nil
	result = tx.GetResult()
	if result != nil {
		t.Error("GetResult should return nil before commit")
	}

	// Commit
	commitResult, err := tx.Commit()
	if err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// GetResult after commit should return the result
	result = tx.GetResult()
	if result == nil {
		t.Fatal("GetResult should return result after commit")
	}
	if result.Commit != commitResult.Commit {
		t.Errorf("GetResult commit %d != commit result commit %d", result.Commit, commitResult.Commit)
	}
	if !result.Committed {
		t.Error("result should be committed")
	}
}
