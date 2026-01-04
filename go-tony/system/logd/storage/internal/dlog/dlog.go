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
	snapMu   sync.Mutex
	position int64 // Current write position (for appends)

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
	// Open file in read-write mode (create if doesn't exist)
	// Note: Not using O_APPEND since we need to seek for snapshot writes
	// Atomicity is guaranteed by logFile.mu lock
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %q: %w", path, err)
	}

	// Get current file size (position for appends)
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat log file %q: %w", path, err)
	}

	// Seek to end of file for appending
	// (needed since we're not using O_APPEND)
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to seek to end of log file %q: %w", path, err)
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

// OpenReaderAt opens a reader at the specified position in the log file.
// This is used to read inline snapshot data stored in the log.
// All seeks in the returned reader are relative to position (position becomes offset 0).
// The returned reader must be closed when done.
// logFile must be "A" or "B".
func (dl *DLog) OpenReaderAt(logFile LogFileID, position int64) (io.ReadSeekCloser, error) {
	var logFileObj *DLogFile
	switch logFile {
	case LogFileA:
		logFileObj = dl.logA
	case LogFileB:
		logFileObj = dl.logB
	default:
		return nil, fmt.Errorf("invalid log file ID: %q (must be A or B)", logFile)
	}

	return logFileObj.OpenReaderAt(position)
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

// ActiveLogSize returns the current size of the active log file.
func (dl *DLog) ActiveLogSize() (int64, error) {
	dl.mu.RLock()
	defer dl.mu.RUnlock()

	var logFile *DLogFile
	if dl.activeLog == LogFileA {
		logFile = dl.logA
	} else {
		logFile = dl.logB
	}
	return logFile.Size()
}

// SwitchActive switches the active log (A ↔ B).
// Blocks if a snapshot is in progress on the inactive log (which is about to become active).
// Called by the caller when compaction boundaries are reached.
func (dl *DLog) SwitchActive() error {
	dl.mu.Lock()

	// Determine inactive log and acquire its snapMu
	// This blocks if a snapshot is running on that log
	var inactiveLog *DLogFile
	if dl.activeLog == LogFileA {
		inactiveLog = dl.logB
	} else {
		inactiveLog = dl.logA
	}

	// Block until any snapshot on inactive log completes
	inactiveLog.snapMu.Lock()

	// Switch active log
	var newActive LogFileID
	if dl.activeLog == LogFileA {
		newActive = LogFileB
	} else {
		newActive = LogFileA
	}
	dl.activeLog = newActive

	// Release snapMu - the old inactive is now active and can receive writes
	inactiveLog.snapMu.Unlock()
	dl.mu.Unlock()

	// Persist state to disk (use captured value, not dl.activeLog which could race)
	statePath := filepath.Join(dl.baseDir, "dlog.state")
	if err := os.WriteFile(statePath, []byte(string(newActive)), 0644); err != nil {
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

// LogFilePath returns the file path for the specified log file ID.
func (dl *DLog) LogFilePath(id LogFileID) string {
	switch id {
	case LogFileA:
		return dl.logA.path
	case LogFileB:
		return dl.logB.path
	default:
		return ""
	}
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

// OpenReaderAt returns a reader scoped to the section starting at position.
// All seeks in the returned reader are relative to position (position becomes offset 0).
// This is used to read inline snapshot data.
// Uses the existing file handle with ReadAt (pread) - no new file handle opened.
// The returned reader's Close() is a no-op since it doesn't own the file handle.
func (dlf *DLogFile) OpenReaderAt(position int64) (io.ReadSeekCloser, error) {
	dlf.mu.RLock()
	defer dlf.mu.RUnlock()

	// Get file size to determine section size
	stat, err := dlf.file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat log file: %w", err)
	}

	// Create a SectionReader from position to end of file
	// This makes all seeks relative to position (position becomes offset 0)
	// SectionReader uses ReadAt (pread) - concurrent-safe, no file pointer movement
	sectionSize := stat.Size() - position
	section := io.NewSectionReader(dlf.file, position, sectionSize)

	// Wrap in a no-op closer since we don't own the file handle
	return &sectionReadCloser{section}, nil
}

// sectionReadCloser wraps an io.SectionReader with a no-op Close method.
// The underlying file handle is owned by DLogFile, not by this reader.
type sectionReadCloser struct {
	*io.SectionReader
}

func (src *sectionReadCloser) Close() error {
	return nil // no-op - we don't own the file handle
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

	lengthOrMagic := binary.BigEndian.Uint32(lengthBytes)

	// Check for blob header magic marker (snapshot data)
	if lengthOrMagic == BlobHeaderMagic {
		// Read blob length (next 4 bytes after magic)
		blobLenBytes := make([]byte, 4)
		it.logFile.mu.RLock()
		_, err := it.logFile.file.ReadAt(blobLenBytes, it.position+4)
		it.logFile.mu.RUnlock()
		if err != nil {
			if err == io.EOF {
				it.done = true
				return nil, it.position, io.EOF
			}
			return nil, it.position, fmt.Errorf("failed to read blob length: %w", err)
		}
		blobLength := int64(binary.BigEndian.Uint32(blobLenBytes))

		// Skip blob header (8 bytes) + blob data
		it.position += BlobHeaderSize + blobLength
		if it.position >= it.fileSize {
			it.done = true
			return nil, it.position, io.EOF
		}

		// Recursively call next to read the entry after the blob
		return it.next()
	}

	entryLength := int64(lengthOrMagic)
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
