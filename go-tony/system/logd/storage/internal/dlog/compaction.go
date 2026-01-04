package dlog

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"
)

// CompactConfig configures compaction behavior.
type CompactConfig struct {
	// GracePeriod is how long to wait for active readers after swap.
	GracePeriod time.Duration
}

// CompactResult contains information about a compacted entry.
type CompactResult struct {
	OldPosition int64     // Position in original file
	NewPosition int64     // Position in compacted file
	LogFile     LogFileID // Which log file
}

// readerRefcount tracks active readers per log file.
type readerRefcount struct {
	logA atomic.Int64
	logB atomic.Int64
}

var readers readerRefcount

// AcquireReader increments the reader count for a log file.
// Call this before reading from a log file.
func (dl *DLog) AcquireReader(id LogFileID) {
	if id == LogFileA {
		readers.logA.Add(1)
	} else {
		readers.logB.Add(1)
	}
}

// ReleaseReader decrements the reader count for a log file.
// Call this when done reading from a log file.
func (dl *DLog) ReleaseReader(id LogFileID) {
	if id == LogFileA {
		readers.logA.Add(-1)
	} else {
		readers.logB.Add(-1)
	}
}

// ActiveReaders returns the count of active readers for a log file.
func (dl *DLog) ActiveReaders(id LogFileID) int64 {
	if id == LogFileA {
		return readers.logA.Load()
	}
	return readers.logB.Load()
}

// CompactInactive compacts the inactive log by writing only the specified
// entries to a new file, then atomically swapping.
//
// positions is a list of entry positions to keep (must be sorted ascending).
// Returns the mapping from old positions to new positions.
//
// The caller is responsible for determining which entries to keep.
// After this returns, the caller should update the index with new positions.
func (dl *DLog) CompactInactive(positions []int64, config *CompactConfig) ([]CompactResult, error) {
	if config == nil {
		config = &CompactConfig{GracePeriod: 5 * time.Second}
	}

	dl.mu.Lock()
	// Determine inactive log (opposite of active)
	inactiveID := LogFileA
	if dl.activeLog == LogFileA {
		inactiveID = LogFileB
	}
	var inactiveLog *DLogFile
	if inactiveID == LogFileA {
		inactiveLog = dl.logA
	} else {
		inactiveLog = dl.logB
	}
	dl.mu.Unlock()

	if len(positions) == 0 {
		// Nothing to compact - truncate the file
		return nil, dl.truncateLog(inactiveLog, config.GracePeriod)
	}

	// Write surviving entries to temp file
	results, tempPath, err := dl.writeCompactedEntries(inactiveLog, positions)
	if err != nil {
		return nil, fmt.Errorf("failed to write compacted entries: %w", err)
	}

	// Atomic swap
	if err := dl.swapLogFile(inactiveLog, tempPath, config.GracePeriod); err != nil {
		os.Remove(tempPath)
		return nil, fmt.Errorf("failed to swap log file: %w", err)
	}

	return results, nil
}

// writeCompactedEntries copies entries at specified positions to a temp file.
// Returns the position mapping and temp file path.
func (dl *DLog) writeCompactedEntries(logFile *DLogFile, positions []int64) ([]CompactResult, string, error) {
	// Create temp file in same directory
	tempPath := logFile.path + ".compact.tmp"
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tempFile.Close()

	results := make([]CompactResult, 0, len(positions))
	var newPosition int64

	for _, oldPos := range positions {
		// Read entry length
		lengthBytes := make([]byte, 4)
		logFile.mu.RLock()
		_, err := logFile.file.ReadAt(lengthBytes, oldPos)
		logFile.mu.RUnlock()
		if err != nil {
			return nil, "", fmt.Errorf("failed to read entry length at %d: %w", oldPos, err)
		}

		lengthOrMagic := binary.BigEndian.Uint32(lengthBytes)

		// Check for blob (snapshot data)
		if lengthOrMagic == BlobHeaderMagic {
			// Read blob length
			blobLenBytes := make([]byte, 4)
			logFile.mu.RLock()
			_, err := logFile.file.ReadAt(blobLenBytes, oldPos+4)
			logFile.mu.RUnlock()
			if err != nil {
				return nil, "", fmt.Errorf("failed to read blob length at %d: %w", oldPos+4, err)
			}
			blobLength := int64(binary.BigEndian.Uint32(blobLenBytes))

			// Copy blob header + data
			totalSize := BlobHeaderSize + blobLength
			if err := dl.copyBytes(logFile, tempFile, oldPos, totalSize); err != nil {
				return nil, "", fmt.Errorf("failed to copy blob at %d: %w", oldPos, err)
			}

			results = append(results, CompactResult{
				OldPosition: oldPos,
				NewPosition: newPosition,
				LogFile:     logFile.id,
			})
			newPosition += totalSize

			// The entry follows the blob - find and copy it too
			entryPos := oldPos + totalSize
			entryLen, entrySize, err := dl.readEntrySize(logFile, entryPos)
			if err != nil {
				return nil, "", fmt.Errorf("failed to read entry after blob at %d: %w", entryPos, err)
			}
			_ = entryLen // unused, entrySize includes length prefix

			if err := dl.copyBytes(logFile, tempFile, entryPos, entrySize); err != nil {
				return nil, "", fmt.Errorf("failed to copy entry at %d: %w", entryPos, err)
			}
			newPosition += entrySize
		} else {
			// Regular entry
			entryLength := int64(lengthOrMagic)
			totalSize := 4 + entryLength // length prefix + data

			if err := dl.copyBytes(logFile, tempFile, oldPos, totalSize); err != nil {
				return nil, "", fmt.Errorf("failed to copy entry at %d: %w", oldPos, err)
			}

			results = append(results, CompactResult{
				OldPosition: oldPos,
				NewPosition: newPosition,
				LogFile:     logFile.id,
			})
			newPosition += totalSize
		}
	}

	// Sync to disk
	if err := tempFile.Sync(); err != nil {
		return nil, "", fmt.Errorf("failed to sync temp file: %w", err)
	}

	return results, tempPath, nil
}

// readEntrySize reads the size of an entry at the given position.
// Returns (entry data length, total size including prefix, error).
func (dl *DLog) readEntrySize(logFile *DLogFile, pos int64) (int64, int64, error) {
	lengthBytes := make([]byte, 4)
	logFile.mu.RLock()
	_, err := logFile.file.ReadAt(lengthBytes, pos)
	logFile.mu.RUnlock()
	if err != nil {
		return 0, 0, err
	}
	length := int64(binary.BigEndian.Uint32(lengthBytes))
	return length, 4 + length, nil
}

// copyBytes copies n bytes from src file at srcPos to dst file at current position.
func (dl *DLog) copyBytes(src *DLogFile, dst *os.File, srcPos, n int64) error {
	buf := make([]byte, min(n, 64*1024)) // 64KB buffer
	remaining := n

	for remaining > 0 {
		toRead := min(remaining, int64(len(buf)))
		src.mu.RLock()
		nRead, err := src.file.ReadAt(buf[:toRead], srcPos+(n-remaining))
		src.mu.RUnlock()
		if err != nil && err != io.EOF {
			return err
		}
		if nRead == 0 {
			return io.ErrUnexpectedEOF
		}

		if _, err := dst.Write(buf[:nRead]); err != nil {
			return err
		}
		remaining -= int64(nRead)
	}
	return nil
}

// swapLogFile atomically swaps the log file with the compacted temp file.
// Waits for active readers to finish (up to gracePeriod), then deletes old file.
func (dl *DLog) swapLogFile(logFile *DLogFile, tempPath string, gracePeriod time.Duration) error {
	oldPath := logFile.path + ".old"

	// Close current file handle
	logFile.mu.Lock()
	if err := logFile.file.Close(); err != nil {
		logFile.mu.Unlock()
		return fmt.Errorf("failed to close log file: %w", err)
	}

	// Rename current -> old
	if err := os.Rename(logFile.path, oldPath); err != nil {
		// Try to reopen
		logFile.file, _ = os.OpenFile(logFile.path, os.O_CREATE|os.O_RDWR, 0644)
		logFile.mu.Unlock()
		return fmt.Errorf("failed to rename log file: %w", err)
	}

	// Rename temp -> current
	if err := os.Rename(tempPath, logFile.path); err != nil {
		// Try to restore
		os.Rename(oldPath, logFile.path)
		logFile.file, _ = os.OpenFile(logFile.path, os.O_CREATE|os.O_RDWR, 0644)
		logFile.mu.Unlock()
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Open new file
	newFile, err := os.OpenFile(logFile.path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		logFile.mu.Unlock()
		return fmt.Errorf("failed to open new log file: %w", err)
	}

	// Update file handle and position
	stat, _ := newFile.Stat()
	logFile.file = newFile
	logFile.position = stat.Size()
	logFile.mu.Unlock()

	// Wait for readers then delete old file
	dl.waitAndDeleteOld(logFile.id, oldPath, gracePeriod)

	return nil
}

// truncateLog truncates an empty log file.
func (dl *DLog) truncateLog(logFile *DLogFile, gracePeriod time.Duration) error {
	logFile.mu.Lock()
	defer logFile.mu.Unlock()

	// Wait for any active readers first
	dl.waitForReaders(logFile.id, gracePeriod)

	if err := logFile.file.Truncate(0); err != nil {
		return fmt.Errorf("failed to truncate log file: %w", err)
	}

	if _, err := logFile.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek to start: %w", err)
	}

	logFile.position = 0
	return nil
}

// waitAndDeleteOld waits for readers to finish, then deletes the old file.
func (dl *DLog) waitAndDeleteOld(id LogFileID, oldPath string, gracePeriod time.Duration) {
	dl.waitForReaders(id, gracePeriod)

	if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
		dl.logger.Warn("failed to remove old log file", "path", oldPath, "error", err)
	}
}

// waitForReaders waits for active readers to finish, up to gracePeriod.
func (dl *DLog) waitForReaders(id LogFileID, gracePeriod time.Duration) {
	deadline := time.Now().Add(gracePeriod)
	for time.Now().Before(deadline) {
		if dl.ActiveReaders(id) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	remaining := dl.ActiveReaders(id)
	if remaining > 0 {
		dl.logger.Warn("compaction: timed out waiting for readers",
			"logFile", id, "remaining", remaining)
	}
}
