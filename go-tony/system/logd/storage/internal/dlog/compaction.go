package dlog

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
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

// AcquireReader increments the reader count for a log file.
// Call this before reading from a log file.
func (dl *DLog) AcquireReader(id LogFileID) {
	if id == LogFileA {
		dl.readersA.Add(1)
	} else {
		dl.readersB.Add(1)
	}
}

// ReleaseReader decrements the reader count for a log file.
// Call this when done reading from a log file.
func (dl *DLog) ReleaseReader(id LogFileID) {
	if id == LogFileA {
		dl.readersA.Add(-1)
	} else {
		dl.readersB.Add(-1)
	}
}

// ActiveReaders returns the count of active readers for a log file.
func (dl *DLog) ActiveReaders(id LogFileID) int64 {
	if id == LogFileA {
		return dl.readersA.Load()
	}
	return dl.readersB.Load()
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
// For snapshot entries (those with SnapPos), also copies the preceding blob data.
// Returns the position mapping and temp file path.
//
// positions contains entry positions (not blob positions). For snapshots, the
// blob header is at (SnapPos - 8) and must be copied along with the entry.
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

	for _, oldEntryPos := range positions {
		// Read the entry to check if it has associated blob data
		entry, err := logFile.ReadEntryAt(oldEntryPos)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read entry at %d: %w", oldEntryPos, err)
		}

		// Get entry size for copying
		_, entrySize, err := dl.readEntrySize(logFile, oldEntryPos)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read entry size at %d: %w", oldEntryPos, err)
		}

		if entry.SnapPos != nil {
			// Snapshot entry - copy blob header + blob data, then write updated entry
			// Blob structure: [header 8 bytes][data N bytes][entry M bytes]
			// SnapPos points to start of blob data (after header)
			blobHeaderPos := *entry.SnapPos - BlobHeaderSize
			blobLength := oldEntryPos - *entry.SnapPos
			blobTotalSize := BlobHeaderSize + blobLength

			// Copy blob header + blob data
			if err := dl.copyBytes(logFile, tempFile, blobHeaderPos, blobTotalSize); err != nil {
				return nil, "", fmt.Errorf("failed to copy blob at %d: %w", blobHeaderPos, err)
			}

			// Update SnapPos to point to new blob data position
			newSnapPos := newPosition + BlobHeaderSize
			entry.SnapPos = &newSnapPos

			// Serialize and write updated entry
			if err := dl.writeEntry(tempFile, entry); err != nil {
				return nil, "", fmt.Errorf("failed to write entry at %d: %w", oldEntryPos, err)
			}

			// Get the new entry size after serialization
			newEntryBytes, _ := entry.ToTony()
			newEntrySize := int64(4 + len(newEntryBytes))

			// Result maps old entry position to new entry position
			results = append(results, CompactResult{
				OldPosition: oldEntryPos,
				NewPosition: newPosition + blobTotalSize, // entry position in new file
				LogFile:     logFile.id,
			})
			newPosition += blobTotalSize + newEntrySize
		} else {
			// Regular entry (patch) - just copy the entry
			if err := dl.copyBytes(logFile, tempFile, oldEntryPos, entrySize); err != nil {
				return nil, "", fmt.Errorf("failed to copy entry at %d: %w", oldEntryPos, err)
			}

			results = append(results, CompactResult{
				OldPosition: oldEntryPos,
				NewPosition: newPosition,
				LogFile:     logFile.id,
			})
			newPosition += entrySize
		}
	}

	// Sync to disk
	if err := tempFile.Sync(); err != nil {
		return nil, "", fmt.Errorf("failed to sync temp file: %w", err)
	}

	return results, tempPath, nil
}

// writeEntry writes an entry to the file with length prefix.
func (dl *DLog) writeEntry(file *os.File, entry *Entry) error {
	entryBytes, err := entry.ToTony()
	if err != nil {
		return fmt.Errorf("failed to serialize entry: %w", err)
	}

	// Write length prefix
	lengthBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBytes, uint32(len(entryBytes)))
	if _, err := file.Write(lengthBytes); err != nil {
		return fmt.Errorf("failed to write length prefix: %w", err)
	}

	// Write entry data
	if _, err := file.Write(entryBytes); err != nil {
		return fmt.Errorf("failed to write entry data: %w", err)
	}

	return nil
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
		// Try to reopen original file
		reopenErr := dl.reopenLogFile(logFile)
		logFile.mu.Unlock()
		if reopenErr != nil {
			dl.logger.Error("failed to reopen log file after rename failure",
				"path", logFile.path, "renameErr", err, "reopenErr", reopenErr)
		}
		return fmt.Errorf("failed to rename log file: %w", err)
	}

	// Rename temp -> current
	if err := os.Rename(tempPath, logFile.path); err != nil {
		// Try to restore original file
		if restoreErr := os.Rename(oldPath, logFile.path); restoreErr != nil {
			dl.logger.Error("failed to restore log file after temp rename failure",
				"path", logFile.path, "renameErr", err, "restoreErr", restoreErr)
		}
		reopenErr := dl.reopenLogFile(logFile)
		logFile.mu.Unlock()
		if reopenErr != nil {
			dl.logger.Error("failed to reopen log file after temp rename failure",
				"path", logFile.path, "renameErr", err, "reopenErr", reopenErr)
		}
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Open new file
	newFile, err := os.OpenFile(logFile.path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		logFile.mu.Unlock()
		return fmt.Errorf("failed to open new log file: %w", err)
	}

	// Update file handle and position
	stat, err := newFile.Stat()
	if err != nil {
		newFile.Close()
		logFile.mu.Unlock()
		return fmt.Errorf("failed to stat new log file: %w", err)
	}
	logFile.file = newFile
	logFile.position = stat.Size()
	logFile.mu.Unlock()

	// Increment generation to invalidate any stale index entries
	dl.IncrementGeneration(logFile.id)

	// Wait for readers then delete old file
	dl.waitAndDeleteOld(logFile.id, oldPath, gracePeriod)

	return nil
}

// reopenLogFile attempts to reopen a log file after a failed operation.
// Must be called with logFile.mu held.
func (dl *DLog) reopenLogFile(logFile *DLogFile) error {
	file, err := os.OpenFile(logFile.path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	logFile.file = file
	logFile.position = stat.Size()
	return nil
}

// truncateLog truncates an empty log file.
func (dl *DLog) truncateLog(logFile *DLogFile, gracePeriod time.Duration) error {
	logFile.mu.Lock()

	// Wait for any active readers first
	dl.waitForReaders(logFile.id, gracePeriod)

	if err := logFile.file.Truncate(0); err != nil {
		logFile.mu.Unlock()
		return fmt.Errorf("failed to truncate log file: %w", err)
	}

	if _, err := logFile.file.Seek(0, io.SeekStart); err != nil {
		logFile.mu.Unlock()
		return fmt.Errorf("failed to seek to start: %w", err)
	}

	logFile.position = 0
	logFile.mu.Unlock()

	// Increment generation to invalidate any stale index entries
	dl.IncrementGeneration(logFile.id)

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
