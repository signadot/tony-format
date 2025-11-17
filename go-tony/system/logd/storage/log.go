package storage

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// TransactionLogEntry represents a transaction commit log entry.
type TransactionLogEntry struct {
	CommitCount   int64 // Commit count assigned to this transaction
	TransactionID string
	Timestamp     string // RFC3339 timestamp
	PendingFiles  []PendingFileRef
}

// PendingFileRef references a pending file that needs to be renamed.
type PendingFileRef struct {
	VirtualPath string
	TxSeq       int64 // Transaction sequence number
}

// AppendTransactionLog appends a transaction commit log entry atomically.
func (s *Storage) AppendTransactionLog(entry *TransactionLogEntry) error {
	logFile := filepath.Join(s.root, "meta", "transactions.log")
	
	// Create the log entry structure using FromMap to preserve parent pointers
	commitCountNode := &ir.Node{Type: ir.NumberType, Int64: &entry.CommitCount, Number: strconv.FormatInt(entry.CommitCount, 10)}
	logEntry := ir.FromMap(map[string]*ir.Node{
		"commitCount":   commitCountNode,
		"transactionId": &ir.Node{Type: ir.StringType, String: entry.TransactionID},
		"timestamp":     &ir.Node{Type: ir.StringType, String: entry.Timestamp},
		"pendingFiles":  buildPendingFilesArray(entry.PendingFiles),
	})

	// Encode to Tony format with wire encoding
	var buf strings.Builder
	if err := encode.Encode(logEntry, &buf, encode.EncodeWire(true)); err != nil {
		return fmt.Errorf("failed to encode log entry: %w", err)
	}

	// Append to log file atomically
	tmpFile := logFile + ".tmp"
	
	// Read existing content if file exists
	var existingContent []byte
	if _, err := os.Stat(logFile); err == nil {
		existingContent, _ = os.ReadFile(logFile)
	}
	
	// Write existing content + new entry on new line
	entryContent := strings.TrimSpace(buf.String())
	var content string
	if len(existingContent) > 0 {
		content = string(existingContent) + "\n" + entryContent
	} else {
		content = entryContent
	}
	
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		return err
	}

	// Atomic rename
	if err := os.Rename(tmpFile, logFile); err != nil {
		os.Remove(tmpFile) // Clean up on error
		return err
	}

	return nil
}

// ReadTransactionLog reads transaction log entries.
// If minCommitCount is nil, reads all entries.
// If minCommitCount is provided, uses binary search to find entries at or after that commit count.
func (s *Storage) ReadTransactionLog(minCommitCount *int64) ([]*TransactionLogEntry, error) {
	logFile := filepath.Join(s.root, "meta", "transactions.log")
	
	file, err := os.Open(logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	
	// If no minimum commit count, read all entries
	if minCommitCount == nil {
		return s.readAllTransactionLog(file)
	}
	
	// Use binary search to find starting position
	startPos, err := s.binarySearchLog(file, *minCommitCount)
	if err != nil {
		return nil, err
	}
	
	// Read from startPos to end
	if _, err := file.Seek(startPos, 0); err != nil {
		return nil, err
	}
	
	return s.readTransactionLogFromPosition(file)
}

// readAllTransactionLog reads all entries from the file.
func (s *Storage) readAllTransactionLog(file *os.File) ([]*TransactionLogEntry, error) {
	data, err := os.ReadFile(file.Name())
	if err != nil {
		return nil, err
	}
	return s.parseTransactionLogLines(data)
}

// readTransactionLogFromPosition reads entries from the current file position to end.
func (s *Storage) readTransactionLogFromPosition(file *os.File) ([]*TransactionLogEntry, error) {
	// Read remaining data from current position
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	
	return s.parseTransactionLogLines(data)
}

// parseTransactionLogLines parses transaction log entries from byte data.
func (s *Storage) parseTransactionLogLines(data []byte) ([]*TransactionLogEntry, error) {
	entries := []*TransactionLogEntry{}
	lines := strings.Split(string(data), "\n")
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		
		node, err := parse.Parse([]byte(line))
		if err != nil {
			s.logger.Warn("skipping invalid log entry", "error", err)
			continue
		}
		
		entry, err := parseLogEntry(node)
		if err != nil {
			s.logger.Warn("skipping invalid log entry", "error", err)
			continue
		}
		
		entries = append(entries, entry)
	}
	
	return entries, nil
}

// binarySearchLog performs binary search to find the file position of the first entry
// with commitCount >= targetCommitCount.
func (s *Storage) binarySearchLog(file *os.File, targetCommitCount int64) (int64, error) {
	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return 0, err
	}
	fileSize := stat.Size()
	
	if fileSize == 0 {
		return 0, nil
	}
	
	left := int64(0)
	right := fileSize
	var bestPos int64 = fileSize // Default to end if not found
	
	for left < right {
		mid := (left + right) / 2
		
		// Seek to mid position
		if _, err := file.Seek(mid, 0); err != nil {
			return 0, err
		}
		
		// Read forward to find start of next line
		lineStart, line, err := s.readNextLine(file)
		if err != nil {
			if err == io.EOF {
				// Reached end, search left half
				right = mid
				continue
			}
			return 0, err
		}
		
		// Parse entry to get commit count
		line = strings.TrimSpace(line)
		if line == "" {
			// Empty line, try next
			left = lineStart + 1
			continue
		}
		
		node, err := parse.Parse([]byte(line))
		if err != nil {
			// Invalid entry, search right half
			left = lineStart + 1
			continue
		}
		
		entry, err := parseLogEntry(node)
		if err != nil {
			// Invalid entry, search right half
			left = lineStart + 1
			continue
		}
		
		// Compare commit count
		if entry.CommitCount >= targetCommitCount {
			// This entry or earlier might be what we want
			bestPos = lineStart
			right = mid
		} else {
			// Need to search right half
			left = lineStart + int64(len(line)) + 1 // Move past this line
		}
	}
	
	// Ensure we're at the start of a line
	if bestPos < fileSize {
		if _, err := file.Seek(bestPos, 0); err != nil {
			return 0, err
		}
		// Read forward to find line start (readNextLine handles finding the start)
		actualStart, _, err := s.readNextLine(file)
		if err == nil {
			bestPos = actualStart
		}
	}
	
	return bestPos, nil
}

// readNextLine reads from current position to the end of the next line.
// Returns the position where the line starts, the line content, and any error.
// If the current position is in the middle of a line, it finds the start of that line.
func (s *Storage) readNextLine(file *os.File) (int64, string, error) {
	currentPos, err := file.Seek(0, 1) // Get current position
	if err != nil {
		return 0, "", err
	}
	
	startPos := currentPos
	
	// If not at start of file, might be in middle of line - read backward to find line start
	if currentPos > 0 {
		// Read a small buffer backward to find newline
		bufSize := int64(256)
		if currentPos < bufSize {
			bufSize = currentPos
		}
		
		readPos := currentPos - bufSize
		if _, err := file.Seek(readPos, 0); err != nil {
			return 0, "", err
		}
		
		buf := make([]byte, bufSize)
		n, err := file.Read(buf)
		if err != nil && err != io.EOF {
			return 0, "", err
		}
		
		// Find last newline before currentPos
		content := string(buf[:n])
		lastNewline := strings.LastIndex(content, "\n")
		if lastNewline >= 0 {
			startPos = readPos + int64(lastNewline) + 1
		} else {
			// No newline found in buffer, we're at start of file or this is the first line
			startPos = 0
		}
		
		// Seek to line start
		if _, err := file.Seek(startPos, 0); err != nil {
			return 0, "", err
		}
	}
	
	// Read the line
	reader := bufio.NewReader(file)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return 0, "", err
	}
	
	// Remove trailing newline
	line = strings.TrimSuffix(line, "\n")
	
	return startPos, line, err
}

// buildPendingFilesArray builds an IR array node from PendingFileRef slice.
func buildPendingFilesArray(refs []PendingFileRef) *ir.Node {
	values := make([]*ir.Node, len(refs))
	for i, ref := range refs {
		txSeqNode := &ir.Node{Type: ir.NumberType, Int64: &ref.TxSeq, Number: strconv.FormatInt(ref.TxSeq, 10)}
		values[i] = ir.FromMap(map[string]*ir.Node{
			"path":  &ir.Node{Type: ir.StringType, String: ref.VirtualPath},
			"txSeq": txSeqNode,
		})
	}
	return ir.FromSlice(values)
}

// parseLogEntry parses an IR node into a TransactionLogEntry.
func parseLogEntry(node *ir.Node) (*TransactionLogEntry, error) {
	if node.Type != ir.ObjectType {
		return nil, fmt.Errorf("expected object, got %v", node.Type)
	}

	entry := &TransactionLogEntry{}
	var pendingFilesNode *ir.Node

	for i, field := range node.Fields {
		if i >= len(node.Values) {
			break
		}
		value := node.Values[i]

		switch field.String {
		case "commitCount":
			if value.Type == ir.NumberType && value.Int64 != nil {
				entry.CommitCount = *value.Int64
			}
		case "transactionId":
			if value.Type == ir.StringType {
				entry.TransactionID = value.String
			}
		case "timestamp":
			if value.Type == ir.StringType {
				entry.Timestamp = value.String
			}
		case "pendingFiles":
			pendingFilesNode = value
		}
	}

	// Parse pending files array
	if pendingFilesNode != nil && pendingFilesNode.Type == ir.ArrayType {
		entry.PendingFiles = parsePendingFilesArray(pendingFilesNode)
	}

	return entry, nil
}

// parsePendingFilesArray parses an IR array node into PendingFileRef slice.
func parsePendingFilesArray(node *ir.Node) []PendingFileRef {
	if node.Type != ir.ArrayType {
		return nil
	}

	refs := make([]PendingFileRef, 0, len(node.Values))
	for _, value := range node.Values {
		if value.Type != ir.ObjectType {
			continue
		}

		ref := PendingFileRef{}
		for i, field := range value.Fields {
			if i >= len(value.Values) {
				break
			}
			fieldValue := value.Values[i]

			switch field.String {
			case "path":
				if fieldValue.Type == ir.StringType {
					ref.VirtualPath = fieldValue.String
				}
			case "txSeq":
				if fieldValue.Type == ir.NumberType && fieldValue.Int64 != nil {
					ref.TxSeq = *fieldValue.Int64
				}
			}
		}
		refs = append(refs, ref)
	}
	return refs
}

// RecoverTransactions replays the transaction log to complete any partial commits.
func (s *Storage) RecoverTransactions() error {
	entries, err := s.ReadTransactionLog(nil)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		// Check if all pending files have been renamed to .diff
		allCommitted := true
		for _, ref := range entry.PendingFiles {
			fsPath := s.PathToFilesystem(ref.VirtualPath)
			// Pending files don't have commit count prefix
			pendingFilename := formatDiffFilename(0, ref.TxSeq, "pending")
			diffFilename := formatDiffFilename(entry.CommitCount, ref.TxSeq, "diff")
			pendingFile := filepath.Join(fsPath, pendingFilename)
			diffFile := filepath.Join(fsPath, diffFilename)
			
			// Check if .pending still exists
			if _, err := os.Stat(pendingFile); err == nil {
				// .pending exists, check if .diff also exists (partial commit)
				if _, err := os.Stat(diffFile); err == nil {
					// Both exist - remove .pending (commit completed)
					os.Remove(pendingFile)
				} else {
					// Only .pending exists - rename to .diff (complete the commit)
					if err := os.Rename(pendingFile, diffFile); err != nil {
						s.logger.Warn("failed to recover pending file", "path", ref.VirtualPath, "txSeq", ref.TxSeq, "error", err)
						allCommitted = false
					}
				}
			} else {
				// .pending doesn't exist, check if .diff exists
				if _, err := os.Stat(diffFile); err != nil {
					// Neither exists - log warning
					s.logger.Warn("log entry references missing file", "path", ref.VirtualPath, "txSeq", ref.TxSeq)
				}
			}
		}
		
		// If all files are committed, delete the transaction state file
		if allCommitted {
			if err := s.DeleteTransactionState(entry.TransactionID); err != nil {
				s.logger.Warn("failed to delete committed transaction state", "transactionId", entry.TransactionID, "error", err)
			}
		}
	}

	return nil
}

// NewTransactionLogEntry creates a new TransactionLogEntry.
func NewTransactionLogEntry(commitCount int64, transactionID string, pendingFiles []PendingFileRef) *TransactionLogEntry {
	return &TransactionLogEntry{
		CommitCount:   commitCount,
		TransactionID: transactionID,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		PendingFiles:  pendingFiles,
	}
}
