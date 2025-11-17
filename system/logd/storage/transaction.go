package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/signadot/tony-format/tony/encode"
	"github.com/signadot/tony-format/tony/ir"
	"github.com/signadot/tony-format/tony/parse"
)

// TransactionState represents the state of a transaction.
type TransactionState struct {
	TransactionID        string
	ParticipantCount     int
	ParticipantsReceived int
	Status               string // "pending", "committed", "aborted"
	CreatedAt            string // RFC3339 timestamp
	Diffs                []PendingDiff
}

// PendingDiff represents a pending diff in a transaction.
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
	filePath := filepath.Join(s.root, "meta", "transactions", filename)

	// Create the transaction state file structure using FromMap to preserve parent pointers
	participantCountNode := &ir.Node{Type: ir.NumberType, Int64: intPtr(int64(state.ParticipantCount)), Number: strconv.Itoa(state.ParticipantCount)}
	participantsReceivedNode := &ir.Node{Type: ir.NumberType, Int64: intPtr(int64(state.ParticipantsReceived)), Number: strconv.Itoa(state.ParticipantsReceived)}
	
	stateFile := ir.FromMap(map[string]*ir.Node{
		"transactionId":      &ir.Node{Type: ir.StringType, String: state.TransactionID},
		"participantCount":   participantCountNode,
		"participantsReceived": participantsReceivedNode,
		"status":             &ir.Node{Type: ir.StringType, String: state.Status},
		"createdAt":          &ir.Node{Type: ir.StringType, String: state.CreatedAt},
		"diff":               buildDiffsArray(state.Diffs),
	})

	// Encode to Tony format
	var buf strings.Builder
	if err := encode.Encode(stateFile, &buf); err != nil {
		return fmt.Errorf("failed to encode transaction state: %w", err)
	}

	// Fix empty array formatting: diff:[] -> diff: [] (Tony syntax requires space)
	encoded := buf.String()
	if len(state.Diffs) == 0 {
		encoded = strings.ReplaceAll(encoded, "diff:[]", "diff: []")
	}

	// Write to temp file first, then rename atomically
	tmpFile := filePath + ".tmp"
	if err := os.WriteFile(tmpFile, []byte(encoded), 0644); err != nil {
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
	filePath := filepath.Join(s.root, "meta", "transactions", filename)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Parse Tony document
	node, err := parse.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse transaction state: %w", err)
	}

	if node.Type != ir.ObjectType {
		return nil, fmt.Errorf("expected object, got %v", node.Type)
	}

	state := &TransactionState{}
	var diffNode *ir.Node

	for i, field := range node.Fields {
		if i >= len(node.Values) {
			break
		}
		value := node.Values[i]

		switch field.String {
		case "transactionId":
			if value.Type == ir.StringType {
				state.TransactionID = value.String
			}
		case "participantCount":
			if value.Type == ir.NumberType && value.Int64 != nil {
				state.ParticipantCount = int(*value.Int64)
			}
		case "participantsReceived":
			if value.Type == ir.NumberType && value.Int64 != nil {
				state.ParticipantsReceived = int(*value.Int64)
			}
		case "status":
			if value.Type == ir.StringType {
				state.Status = value.String
			}
		case "createdAt":
			if value.Type == ir.StringType {
				state.CreatedAt = value.String
			}
		case "diff":
			diffNode = value
		}
	}

	// Parse diffs array
	if diffNode != nil && diffNode.Type == ir.ArrayType {
		state.Diffs = parseDiffsArray(diffNode)
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
	filePath := filepath.Join(s.root, "meta", "transactions", filename)
	return os.Remove(filePath)
}

// buildDiffsArray builds an IR array node from PendingDiff slice.
func buildDiffsArray(diffs []PendingDiff) *ir.Node {
	values := make([]*ir.Node, len(diffs))
	for i, diff := range diffs {
		values[i] = ir.FromMap(map[string]*ir.Node{
			"path":      &ir.Node{Type: ir.StringType, String: diff.Path},
			"diffFile":  &ir.Node{Type: ir.StringType, String: diff.DiffFile},
			"writtenAt": &ir.Node{Type: ir.StringType, String: diff.WrittenAt},
		})
	}
	return ir.FromSlice(values)
}

// parseDiffsArray parses an IR array node into PendingDiff slice.
func parseDiffsArray(node *ir.Node) []PendingDiff {
	if node.Type != ir.ArrayType {
		return nil
	}

	diffs := make([]PendingDiff, 0, len(node.Values))
	for _, value := range node.Values {
		if value.Type != ir.ObjectType {
			continue
		}

		diff := PendingDiff{}
		for i, field := range value.Fields {
			if i >= len(value.Values) {
				break
			}
			fieldValue := value.Values[i]

			switch field.String {
			case "path":
				if fieldValue.Type == ir.StringType {
					diff.Path = fieldValue.String
				}
			case "diffFile":
				if fieldValue.Type == ir.StringType {
					diff.DiffFile = fieldValue.String
				}
			case "writtenAt":
				if fieldValue.Type == ir.StringType {
					diff.WrittenAt = fieldValue.String
				}
			}
		}
		diffs = append(diffs, diff)
	}
	return diffs
}

// intPtr returns a pointer to the given int64.
func intPtr(i int64) *int64 {
	return &i
}

// NewTransactionState creates a new TransactionState with the given transaction ID and participant count.
func NewTransactionState(transactionID string, participantCount int) *TransactionState {
	return &TransactionState{
		TransactionID:        transactionID,
		ParticipantCount:     participantCount,
		ParticipantsReceived: 0,
		Status:               "pending",
		CreatedAt:            time.Now().UTC().Format(time.RFC3339),
		Diffs:                []PendingDiff{},
	}
}
