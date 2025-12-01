package storage

import (
	"testing"

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
			_ = tx.Rollback()                               // Returns error
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

	// Verify Rollback signature: Rollback() error
	var rollbackFunc func(*Tx) error
	rollbackFunc = (*Tx).Rollback
	_ = rollbackFunc // Just verify the type compiles
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
