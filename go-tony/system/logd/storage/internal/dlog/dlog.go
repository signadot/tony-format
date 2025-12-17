package dlog

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"log/slog"
)

// DLog manages the logA/logB double-buffered log files.
// Follows the double-buffering pattern where one log is active for writes
// and the other is inactive (being snapshotted or ready for compaction).
type DLog struct {
	baseDir   string       // Base directory for log files
	logA      *DLogFile    // First log file
	logB      *DLogFile    // Second log file
	activeLog LogFileID    // Which log is currently active ("A" or "B")
	mu        sync.RWMutex // Protects activeLog and file operations

	// Metadata
	logger *slog.Logger // Logger for operations
}

// LogFileID identifies which log file (A or B)
type LogFileID string

const (
	LogFileA LogFileID = "A"
	LogFileB LogFileID = "B"
)

// DLogFile represents a single log file with its operations
type DLogFile struct {
	id       LogFileID    // "A" or "B"
	path     string       // Full path to log file (e.g., "logA" or "logB")
	file     *os.File     // Open file handle
	mu       sync.RWMutex // Protects file operations
	position int64        // Current write position (for appends)

	// Metadata
	logger *slog.Logger
}

// DLogIter provides sequential iteration over log entries using streaming reads.
// Uses streaming parsing to avoid loading entire entries into memory.
// Iterates over both logA and logB, switching between them based on commit order.
type DLogIter struct {
	dlog  *DLog
	iterA *singleFileIter
	iterB *singleFileIter
	nextA *Entry
	nextB *Entry
	posA  int64
	posB  int64
	done  bool
}

type singleFileIter struct {
	logFile  *DLogFile
	position int64
	done     bool
	fileSize int64
}

// NewDLog creates a new double-buffered log manager.
// Initializes both logA and logB files, determines active log from state.
// Defaults to LogA as active if no state exists.
func NewDLog(baseDir string, logger *slog.Logger) (*DLog, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Create base directory if it doesn't exist
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	// Initialize logA
	logAPath := filepath.Join(baseDir, "logA")
	logA, err := newDLogFile(LogFileA, logAPath, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize logA: %w", err)
	}

	// Initialize logB
	logBPath := filepath.Join(baseDir, "logB")
	logB, err := newDLogFile(LogFileB, logBPath, logger)
	if err != nil {
		logA.Close() // Clean up on error
		return nil, fmt.Errorf("failed to initialize logB: %w", err)
	}

	// Determine active log from state file (if exists)
	// For now, default to LogA. State persistence can be added later.
	activeLog := LogFileA
	statePath := filepath.Join(baseDir, "dlog.state")
	if stateData, err := os.ReadFile(statePath); err == nil && len(stateData) > 0 {
		if string(stateData) == "B" {
			activeLog = LogFileB
		}
	}

	dl := &DLog{
		baseDir:   baseDir,
		logA:      logA,
		logB:      logB,
		activeLog: activeLog,
		logger:    logger,
	}

	return dl, nil
}

// newDLogFile creates and opens a log file for appending.
func newDLogFile(id LogFileID, path string, logger *slog.Logger) (*DLogFile, error) {
	// Open file in append mode (create if doesn't exist)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %q: %w", path, err)
	}

	// Get current file size (position for appends)
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat log file %q: %w", path, err)
	}

	return &DLogFile{
		id:       id,
		path:     path,
		file:     file,
		position: stat.Size(),
		logger:   logger,
	}, nil
}

// AppendEntry appends an Entry to the active log file.
// The Entry.Commit is provided by the caller (from seq.Seq.NextCommit()).
// Returns the log position and which log file (A or B) it was written to.
// Does NOT automatically switch active log - that's handled by the caller
// when compaction boundaries are reached.
func (dl *DLog) AppendEntry(entry *Entry) (logPosition int64, logFile LogFileID, err error) {
	dl.mu.Lock()
	activeLog := dl.activeLog
	dl.mu.Unlock()

	var logFileObj *DLogFile
	if activeLog == LogFileA {
		logFileObj = dl.logA
	} else {
		logFileObj = dl.logB
	}

	position, err := logFileObj.AppendEntry(entry)
	if err != nil {
		return 0, "", fmt.Errorf("failed to append entry to %s: %w", activeLog, err)
	}

	return position, activeLog, nil
}

// ReadEntryAt reads an Entry from the specified log file at the given position.
// logFile must be "A" or "B".
func (dl *DLog) ReadEntryAt(logFile LogFileID, position int64) (*Entry, error) {
	var logFileObj *DLogFile
	switch logFile {
	case LogFileA:
		logFileObj = dl.logA
	case LogFileB:
		logFileObj = dl.logB
	default:
		return nil, fmt.Errorf("invalid log file ID: %q (must be A or B)", logFile)
	}

	return logFileObj.ReadEntryAt(position)
}

// GetActiveLog returns the currently active log file ID.
func (dl *DLog) GetActiveLog() LogFileID {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	return dl.activeLog
}

// GetInactiveLog returns the currently inactive log file ID.
func (dl *DLog) GetInactiveLog() LogFileID {
	dl.mu.RLock()
	defer dl.mu.RUnlock()
	if dl.activeLog == LogFileA {
		return LogFileB
	}
	return LogFileA
}

// AppendToInactive appends an entry to the inactive log file.
// Returns the position and log file ID where the entry was written.
func (dl *DLog) AppendToInactive(entry *Entry) (logPosition int64, logFile LogFileID, err error) {
	dl.mu.Lock()
	inactiveLog := dl.GetInactiveLog()
	dl.mu.Unlock()

	var logFileObj *DLogFile
	if inactiveLog == LogFileA {
		logFileObj = dl.logA
	} else {
		logFileObj = dl.logB
	}

	position, err := logFileObj.AppendEntry(entry)
	if err != nil {
		return 0, "", fmt.Errorf("failed to append entry to %s: %w", inactiveLog, err)
	}

	return position, inactiveLog, nil
}

// WriteSnapshotToInactive writes snapshot event data to the inactive log and creates a snapshot entry.
// eventsData contains the serialized event stream.
// Returns the position of the snapshot entry and log file ID.
func (dl *DLog) WriteSnapshotToInactive(commit int64, timestamp string, eventsData []byte) (entryPosition int64, logFile LogFileID, err error) {
	dl.mu.RLock()
	activeLog := dl.activeLog
	dl.mu.RUnlock()

	// Determine inactive log
	var inactiveLog LogFileID
	var logFileObj *DLogFile
	if activeLog == LogFileA {
		inactiveLog = LogFileB
		logFileObj = dl.logB
	} else {
		inactiveLog = LogFileA
		logFileObj = dl.logA
	}

	logFileObj.mu.Lock()
	defer logFileObj.mu.Unlock()

	// Write event stream data at current position
	snapshotPos := logFileObj.position
	n, err := logFileObj.file.Write(eventsData)
	if err != nil {
		return 0, "", fmt.Errorf("failed to write snapshot events: %w", err)
	}
	logFileObj.position += int64(n)

	// Create snapshot entry pointing to the event data
	entry := &Entry{
		Commit:     commit,
		Timestamp:  timestamp,
		Patch:      nil,
		SnapPos:    &snapshotPos,
		TxSource:   nil,
		LastCommit: nil,
	}

	// Serialize entry to Tony format
	entryBytes, err := entry.ToTony()
	if err != nil {
		return 0, "", fmt.Errorf("failed to serialize entry: %w", err)
	}

	// Check length fits in uint32
	if len(entryBytes) > 0xFFFFFFFF {
		return 0, "", fmt.Errorf("entry too large: %d bytes (max %d)", len(entryBytes), 0xFFFFFFFF)
	}

	// Write length prefix (4 bytes, big-endian uint32)
	lengthBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBytes, uint32(len(entryBytes)))

	// Get current position before writing
	entryPos := logFileObj.position

	// Write length prefix
	if _, err := logFileObj.file.Write(lengthBytes); err != nil {
		return 0, "", fmt.Errorf("failed to write entry length prefix: %w", err)
	}

	// Write entry data
	if _, err := logFileObj.file.Write(entryBytes); err != nil {
		return 0, "", fmt.Errorf("failed to write entry data: %w", err)
	}

	// Update position
	logFileObj.position = entryPos + 4 + int64(len(entryBytes))

	return entryPos, inactiveLog, nil
}

// SwitchActive switches the active log (A ↔ B).
// Called by the caller when compaction boundaries are reached.
func (dl *DLog) SwitchActive() error {
	dl.mu.Lock()
	defer dl.mu.Unlock()

	// Switch active log
	if dl.activeLog == LogFileA {
		dl.activeLog = LogFileB
	} else {
		dl.activeLog = LogFileA
	}

	// Persist state to disk
	statePath := filepath.Join(dl.baseDir, "dlog.state")
	if err := os.WriteFile(statePath, []byte(string(dl.activeLog)), 0644); err != nil {
		// Log error but don't fail - state can be recovered
		dl.logger.Warn("failed to persist active log state", "error", err)
	}

	return nil
}

// Iterator creates an iterator for reading entries from both log files in commit order.
// Starts at position 0 for both files.
// Note: Currently uses non-streaming reads. Streaming support can be added later.
func (dl *DLog) Iterator() (*DLogIter, error) {
	sizeA, err := dl.logA.Size()
	if err != nil {
		return nil, fmt.Errorf("failed to get logA size: %w", err)
	}
	sizeB, err := dl.logB.Size()
	if err != nil {
		return nil, fmt.Errorf("failed to get logB size: %w", err)
	}

	iterA := &singleFileIter{
		logFile:  dl.logA,
		position: 0,
		done:     sizeA == 0,
		fileSize: sizeA,
	}
	iterB := &singleFileIter{
		logFile:  dl.logB,
		position: 0,
		done:     sizeB == 0,
		fileSize: sizeB,
	}

	it := &DLogIter{
		dlog:  dl,
		iterA: iterA,
		iterB: iterB,
	}

	// Peek at first entries from both files
	if !iterA.done {
		entry, pos, err := iterA.next()
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read first entry from logA: %w", err)
		}
		if err == nil {
			it.nextA = entry
			it.posA = pos
		}
	}
	if !iterB.done {
		entry, pos, err := iterB.next()
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read first entry from logB: %w", err)
		}
		if err == nil {
			it.nextB = entry
			it.posB = pos
		}
	}

	if it.nextA == nil && it.nextB == nil {
		it.done = true
	}

	return it, nil
}

// Close closes both log files.
func (dl *DLog) Close() error {
	var errs error

	if err := dl.logA.Close(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("failed to close logA: %w", err))
	}

	if err := dl.logB.Close(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("failed to close logB: %w", err))
	}
	return errs
}

// AppendEntry appends an Entry to this log file.
// Format: [4 bytes: uint32 length (big-endian)][entry data in Tony wire format]
// Returns the byte position where the entry was written.
func (dlf *DLogFile) AppendEntry(entry *Entry) (position int64, err error) {
	dlf.mu.Lock()
	defer dlf.mu.Unlock()

	// Serialize entry to Tony format
	entryBytes, err := entry.ToTony()
	if err != nil {
		return 0, fmt.Errorf("failed to serialize entry: %w", err)
	}

	// Check length fits in uint32
	if len(entryBytes) > 0xFFFFFFFF {
		return 0, fmt.Errorf("entry too large: %d bytes (max %d)", len(entryBytes), 0xFFFFFFFF)
	}

	// Write length prefix (4 bytes, big-endian uint32)
	lengthBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBytes, uint32(len(entryBytes)))

	// Get current position before writing
	currentPos := dlf.position

	// Write length prefix
	if _, err := dlf.file.Write(lengthBytes); err != nil {
		return 0, fmt.Errorf("failed to write length prefix: %w", err)
	}

	// Write entry data
	if _, err := dlf.file.Write(entryBytes); err != nil {
		return 0, fmt.Errorf("failed to write entry data: %w", err)
	}

	// Update position
	dlf.position = currentPos + 4 + int64(len(entryBytes))

	return currentPos, nil
}

// ReadEntryAt reads an Entry from the specified byte position.
// Reads length prefix, then entry data, deserializes to Entry.
func (dlf *DLogFile) ReadEntryAt(position int64) (*Entry, error) {
	dlf.mu.RLock()
	defer dlf.mu.RUnlock()

	// Read length prefix (4 bytes, big-endian uint32)
	lengthBytes := make([]byte, 4)
	if _, err := dlf.file.ReadAt(lengthBytes, position); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("position %d: reached EOF while reading length prefix", position)
		}
		return nil, fmt.Errorf("failed to read length prefix at position %d: %w", position, err)
	}

	length := int64(binary.BigEndian.Uint32(lengthBytes))

	// Read entry data
	entryBytes := make([]byte, length)
	if _, err := dlf.file.ReadAt(entryBytes, position+4); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("position %d: reached EOF while reading entry data (expected %d bytes)", position+4, length)
		}
		return nil, fmt.Errorf("failed to read entry data at position %d: %w", position+4, err)
	}

	// Deserialize entry
	entry := &Entry{}
	if err := entry.FromTony(entryBytes); err != nil {
		return nil, fmt.Errorf("failed to deserialize entry at position %d: %w", position, err)
	}

	return entry, nil
}

// Size returns the current size of the log file.
func (dlf *DLogFile) Size() (int64, error) {
	dlf.mu.RLock()
	defer dlf.mu.RUnlock()

	stat, err := dlf.file.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to stat log file: %w", err)
	}

	return stat.Size(), nil
}

// Position returns the current write position (for appends).
func (dlf *DLogFile) Position() int64 {
	dlf.mu.RLock()
	defer dlf.mu.RUnlock()
	return dlf.position
}

// Close closes the log file.
func (dlf *DLogFile) Close() error {
	dlf.mu.Lock()
	defer dlf.mu.Unlock()

	if dlf.file == nil {
		return nil // Already closed
	}

	if err := dlf.file.Close(); err != nil {
		return fmt.Errorf("failed to close log file %q: %w", dlf.path, err)
	}

	dlf.file = nil
	return nil
}

func (it *singleFileIter) next() (*Entry, int64, error) {
	if it.done {
		return nil, it.position, io.EOF
	}

	if it.position >= it.fileSize {
		it.done = true
		return nil, it.position, io.EOF
	}

	lengthBytes := make([]byte, 4)
	it.logFile.mu.RLock()
	_, err := it.logFile.file.ReadAt(lengthBytes, it.position)
	it.logFile.mu.RUnlock()
	if err != nil {
		if err == io.EOF {
			it.done = true
			return nil, it.position, io.EOF
		}
		return nil, it.position, fmt.Errorf("failed to read length prefix: %w", err)
	}

	entryLength := int64(binary.BigEndian.Uint32(lengthBytes))
	oldPosition := it.position

	entryBytes := make([]byte, entryLength)
	it.logFile.mu.RLock()
	_, err = it.logFile.file.ReadAt(entryBytes, it.position+4)
	it.logFile.mu.RUnlock()
	if err != nil {
		if err == io.EOF {
			it.done = true
			return nil, it.position, io.EOF
		}
		return nil, it.position, fmt.Errorf("failed to read entry data: %w", err)
	}

	entry := &Entry{}
	if err := entry.FromTony(entryBytes); err != nil {
		return nil, it.position, fmt.Errorf("failed to deserialize entry: %w", err)
	}

	it.position = oldPosition + 4 + entryLength
	if it.position >= it.fileSize {
		it.done = true
	}

	return entry, oldPosition, nil
}

// Next reads and returns the next entry from both log files in commit order.
// Returns the entry, its log file ID and position, and any error.
// Returns io.EOF when both files are exhausted.
func (it *DLogIter) Next() (*Entry, LogFileID, int64, error) {
	if it.done {
		return nil, "", 0, io.EOF
	}

	// Refresh next entries if needed
	if it.nextA == nil && !it.iterA.done {
		entry, pos, err := it.iterA.next()
		if err != nil && err != io.EOF {
			return nil, "", 0, fmt.Errorf("failed to read from logA: %w", err)
		}
		if err == nil {
			it.nextA = entry
			it.posA = pos
		}
	}
	if it.nextB == nil && !it.iterB.done {
		entry, pos, err := it.iterB.next()
		if err != nil && err != io.EOF {
			return nil, "", 0, fmt.Errorf("failed to read from logB: %w", err)
		}
		if err == nil {
			it.nextB = entry
			it.posB = pos
		}
	}

	// Choose entry with lower commit number
	var entry *Entry
	var logFile LogFileID
	var pos int64

	if it.nextA == nil && it.nextB == nil {
		it.done = true
		return nil, "", 0, io.EOF
	} else if it.nextA == nil {
		entry = it.nextB
		logFile = LogFileB
		pos = it.posB
		it.nextB = nil
	} else if it.nextB == nil {
		entry = it.nextA
		logFile = LogFileA
		pos = it.posA
		it.nextA = nil
	} else if it.nextA.Commit <= it.nextB.Commit {
		entry = it.nextA
		logFile = LogFileA
		pos = it.posA
		it.nextA = nil
	} else {
		entry = it.nextB
		logFile = LogFileB
		pos = it.posB
		it.nextB = nil
	}

	return entry, logFile, pos, nil
}

// Done returns true if iterator has reached end of both files.
func (it *DLogIter) Done() bool {
	return it.done
}
