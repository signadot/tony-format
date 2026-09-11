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
	// GracePeriod is how long a swap waits for the file's active readers before it
	// closes the file the previous swap retired.
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
// Returns the mapping from old positions to new positions. A kept snapshot entry
// takes its blob with it. An empty positions leaves the file empty.
//
// Either way the file is swapped for its rewrite and its generation bumped, in one
// critical section under the file's lock. The file it replaced stays open, unlinked, and
// serves reads at the old generation until the next compaction of this file, which first
// waits for the file's readers to finish or config.GracePeriod to pass (swapLogFile).
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
		// Nothing survives: the file is swapped for an empty one, as for a rewrite, so a
		// read in flight keeps the file it was reading. Truncating in place cut it from
		// under them.
		tempPath := inactiveLog.path + ".compact.tmp"
		f, err := os.Create(tempPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create empty log: %w", err)
		}
		if err := f.Sync(); err != nil {
			f.Close()
			os.Remove(tempPath)
			return nil, fmt.Errorf("failed to sync empty log: %w", err)
		}
		f.Close()
		if err := dl.swapLogFile(inactiveLog, tempPath, config.GracePeriod); err != nil {
			os.Remove(tempPath)
			return nil, fmt.Errorf("failed to swap log file: %w", err)
		}
		return nil, nil
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

			// Serialize and write updated entry. writeEntry reports what it wrote:
			// this used to serialize the entry a second time purely to measure it,
			// which assumed the two serializations agreed byte for byte. If they ever
			// did not, newPosition would drift and every later entry would be recorded
			// at the wrong offset — and a re-encode is exactly what happens here when
			// the surviving entry came from a log written in the old text form.
			newEntrySize, err := dl.writeEntry(tempFile, entry)
			if err != nil {
				return nil, "", fmt.Errorf("failed to write entry at %d: %w", oldEntryPos, err)
			}

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

// writeEntry writes an entry to the file with length prefix, returning the total bytes
// written including that prefix, so the caller advances by what actually landed rather
// than by a second guess at it.
//
// Binary event stream, matching AppendEntry: a compacted snapshot entry is rewritten, so
// this is also where an entry from an old text-form log gets carried over into the current
// encoding.
func (dl *DLog) writeEntry(file *os.File, entry *Entry) (int64, error) {
	entryBytes, err := encodeEntry(entry)
	if err != nil {
		return 0, fmt.Errorf("failed to serialize entry: %w", err)
	}

	// Frame the record in one buffer and write it once, as AppendEntry does.
	rec := make([]byte, 4+len(entryBytes))
	binary.BigEndian.PutUint32(rec[:4], uint32(len(entryBytes)))
	copy(rec[4:], entryBytes)
	if _, err := file.Write(rec); err != nil {
		return 0, fmt.Errorf("failed to write entry data: %w", err)
	}
	return int64(len(rec)), nil
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
//
// The swap and the generation bump are one critical section under the file's lock, which
// every read takes to ask the generation: a read indexed under the old generation is
// served from the file it was indexed against, the one this replaces, which stays open --
// unlinked -- as logFile.retired. The file retired by the previous compaction is closed
// first, once its readers finish or gracePeriod passes. It used to be the other way
// about: the handle was closed as the swap began, so a reader holding it failed with
// "file already closed", and the generation was bumped after the lock was released, so a
// read in between read the rewritten file at an old position (d2mq819wh12ksynxmdn0).
func (dl *DLog) swapLogFile(logFile *DLogFile, tempPath string, gracePeriod time.Duration) error {
	oldPath := logFile.path + ".old"

	// Before the lock: a reader holds it for the length of its read.
	dl.waitForReaders(logFile.id, gracePeriod)

	logFile.mu.Lock()

	// Rename current -> old. The open handle goes on reading the same file.
	if err := os.Rename(logFile.path, oldPath); err != nil {
		logFile.mu.Unlock()
		return fmt.Errorf("failed to rename log file: %w", err)
	}

	// Rename temp -> current
	if err := os.Rename(tempPath, logFile.path); err != nil {
		if restoreErr := os.Rename(oldPath, logFile.path); restoreErr != nil {
			dl.logger.Error("failed to restore log file after temp rename failure",
				"path", logFile.path, "renameErr", err, "restoreErr", restoreErr)
		}
		logFile.mu.Unlock()
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Make the two renames durable so a crash can't leave the directory pointing at a
	// half-swapped state that startup recovery would misread (issue 656g8yt5).
	if err := fsyncDir(dl.baseDir); err != nil {
		dl.logger.Warn("failed to fsync dir after log swap", "path", logFile.path, "error", err)
	}

	// Open new file
	newFile, err := os.OpenFile(logFile.path, os.O_CREATE|os.O_RDWR, 0644)
	var stat os.FileInfo
	if err == nil {
		if stat, err = newFile.Stat(); err != nil {
			newFile.Close()
		}
	}
	if err != nil {
		// Put the directory back the way the handle sees it, so appends and the path
		// name the same file.
		if rbErr := os.Rename(logFile.path, tempPath); rbErr == nil {
			if rbErr := os.Rename(oldPath, logFile.path); rbErr != nil {
				dl.logger.Error("failed to restore log file after open failure",
					"path", logFile.path, "openErr", err, "restoreErr", rbErr)
			}
		}
		logFile.mu.Unlock()
		return fmt.Errorf("failed to open new log file: %w", err)
	}

	if logFile.retired != nil {
		logFile.retired.Close()
	}
	logFile.retired, logFile.retiredGen = logFile.file, dl.GetGeneration(logFile.id)
	logFile.file = newFile
	logFile.position = stat.Size()
	dl.bumpGeneration(logFile.id)
	logFile.mu.Unlock()

	// Persist the bump durably: the generation is the token a restart uses to detect a
	// stale index after compaction, so it must survive a crash (issue 656g8yt5).
	if err := dl.writeState(); err != nil {
		dl.logger.Warn("failed to persist generation state", "error", err)
	}

	// The undo copy goes before the caller re-indexes the survivors, as it always has: a
	// crash with it on disk restores the old file (recoverCompactionArtifacts), and that
	// must not meet an index persisted at the new positions. The retired handle does not
	// need the path.
	if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
		dl.logger.Warn("failed to remove old log file", "path", oldPath, "error", err)
	}

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
