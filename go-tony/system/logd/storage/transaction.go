package storage

import (
	"os"
	"path/filepath"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// TransactionState represents the state of a transaction.
//
//tony:schemagen=transaction-state
type TransactionState struct {
	TransactionID        string
	ParticipantCount     int
	ParticipantRequests  []*api.Patch
	ParticipantMatches   []*api.Match
	ParticipantsReceived int
	Status               string // "pending", "committed", "aborted"
	CreatedAt            string // RFC3339 timestamp
	ExpiresAt            string
	Diffs                []PendingDiff
}

// PendingDiff represents a pending diff in a transaction.
//
//tony:schemagen=pending-diff
type PendingDiff struct {
	Path      string
	DiffFile  string // Full filesystem path to the .pending file
	WrittenAt string // RFC3339 timestamp
}

// WriteTransactionState writes a transaction state file to disk.
func (s *Storage) WriteTransactionState(state *TransactionState) error {
	// Transaction ID format: tx-{seq}-{participant_count}
	// Extract seq from transaction ID for filename
	// Format: tx-12345-2 -> tx-12345-2.pending
	filename := state.TransactionID + ".pending"
	filePath := filepath.Join(s.Root, "meta", "transactions", filename)
	d, err := state.ToTony()
	if err != nil {
		return err
	}

	// Write to temp file first, then rename atomically
	tmpFile := filePath + ".tmp"
	if err := os.WriteFile(tmpFile, []byte(d), 0644); err != nil {
		return err
	}

	// Atomic rename
	if err := os.Rename(tmpFile, filePath); err != nil {
		os.Remove(tmpFile) // Clean up on error
		return err
	}

	return nil
}

// ReadTransactionState reads a transaction state file from disk.
func (s *Storage) ReadTransactionState(transactionID string) (*TransactionState, error) {
	filename := transactionID + ".pending"
	filePath := filepath.Join(s.Root, "meta", "transactions", filename)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Parse Tony document
	state := &TransactionState{}
	if err := state.FromTony(data); err != nil {
		return nil, err
	}
	return state, nil
}

// UpdateTransactionState updates an existing transaction state file.
func (s *Storage) UpdateTransactionState(transactionID string, updateFn func(*TransactionState)) error {
	state, err := s.ReadTransactionState(transactionID)
	if err != nil {
		return err
	}

	updateFn(state)

	return s.WriteTransactionState(state)
}

// DeleteTransactionState deletes a transaction state file.
func (s *Storage) DeleteTransactionState(transactionID string) error {
	filename := transactionID + ".pending"
	filePath := filepath.Join(s.Root, "meta", "transactions", filename)
	return os.Remove(filePath)
}

// NewTransactionState creates a new TransactionState with the given transaction ID and participant count.
func NewTransactionState(transactionID string, participantCount int) *TransactionState {
	return &TransactionState{
		TransactionID:    transactionID,
		ParticipantCount: participantCount,
		Status:           "pending",
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
		Diffs:            []PendingDiff{},
	}
}
