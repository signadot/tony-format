package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// TxState represents the state of a transaction.
//
//tony:schemagen=transaction-state
type TxState struct {
	TxID                 int64
	ParticipantCount     int
	ParticipantRequests  []*api.Patch
	ParticipantMatches   []*api.Match
	ParticipantsReceived int
	Status               string // "pending", "committed", "aborted"
	CreatedAt            string // RFC3339 timestamp
	ExpiresAt            string
	FileMetas            []FileMeta
}

// FileMeta represents metadata about a diff file in a transaction.
//
//tony:schemagen=pending-diff
type FileMeta struct {
	Path      string
	FSPath    string // Full filesystem path to the .pending file (set when file is written)
	WrittenAt string // RFC3339 timestamp (set when file is written)
}

// WriteTxState writes a transaction state file to disk.
func (s *Storage) WriteTxState(state *TxState) error {
	// Filename format: {txID}.pending (e.g., 12345.pending)
	filename := fmt.Sprintf("%d.pending", state.TxID)
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

// ReadTxState reads a transaction state file from disk.
func (s *Storage) ReadTxState(txID int64) (*TxState, error) {
	filename := fmt.Sprintf("%d.pending", txID)
	filePath := filepath.Join(s.Root, "meta", "transactions", filename)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Parse Tony document
	state := &TxState{}
	if err := state.FromTony(data); err != nil {
		return nil, err
	}
	return state, nil
}

// UpdateTxState updates an existing transaction state file.
// This method is thread-safe and uses per-transaction locking to serialize updates.
func (s *Storage) UpdateTxState(txID int64, updateFn func(*TxState)) error {
	// Get or create a mutex for this transaction ID
	muInterface, _ := s.txLocks.LoadOrStore(txID, &sync.Mutex{})
	mu := muInterface.(*sync.Mutex)

	mu.Lock()
	defer mu.Unlock()

	state, err := s.ReadTxState(txID)
	if err != nil {
		return err
	}

	updateFn(state)

	return s.WriteTxState(state)
}

// DeleteTxState deletes a transaction state file.
func (s *Storage) DeleteTxState(txID int64) error {
	filename := fmt.Sprintf("%d.pending", txID)
	filePath := filepath.Join(s.Root, "meta", "transactions", filename)
	return os.Remove(filePath)
}

// NewTxState creates a new TxState with the given transaction ID and participant count.
func NewTxState(txID int64, participantCount int) *TxState {
	return &TxState{
		TxID:             txID,
		ParticipantCount: participantCount,
		Status:           "pending",
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
		FileMetas:        []FileMeta{},
	}
}
