package dlog

import (
	"encoding/binary"
	"fmt"
	"io"
)

// SnapshotWriter is a writer for creating snapshots in the inactive log.
// It implements io.WriteCloser and io.Seeker for use with snap.Builder.
// When closed, it writes the snapshot Entry metadata to the log.
type SnapshotWriter struct {
	dl        *DLog
	logFile   *DLogFile
	logFileID LogFileID
	startPos  int64 // where snapshot data starts
	endPos    int64 // where snapshot data ends (tracked on Write)
	commit    int64
	timestamp string
	entryPos  int64 // set on Close
	closed    bool
}

// NewSnapshotWriter creates a writer for building a snapshot in the inactive log.
// The caller should create a snap.Builder with this writer, feed events to it,
// close the builder, then close this writer to finalize the Entry.
func (dl *DLog) NewSnapshotWriter(commit int64, timestamp string) (*SnapshotWriter, error) {
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
	startPos := logFileObj.position
	// Don't unlock yet - caller will write through this writer

	return &SnapshotWriter{
		dl:        dl,
		logFile:   logFileObj,
		logFileID: inactiveLog,
		startPos:  startPos,
		endPos:    startPos, // Initialize to start, will be updated by Write()
		commit:    commit,
		timestamp: timestamp,
	}, nil
}

// Write implements io.Writer
func (sw *SnapshotWriter) Write(p []byte) (n int, err error) {
	if sw.closed {
		return 0, fmt.Errorf("write to closed SnapshotWriter")
	}
	n, err = sw.logFile.file.Write(p)
	if err != nil {
		return n, err
	}
	sw.logFile.position += int64(n)
	// Track the highest position reached (end of snapshot data)
	if sw.logFile.position > sw.endPos {
		sw.endPos = sw.logFile.position
	}
	return n, nil
}

// Seek implements io.Seeker
func (sw *SnapshotWriter) Seek(offset int64, whence int) (int64, error) {
	if sw.closed {
		return 0, fmt.Errorf("seek on closed SnapshotWriter")
	}
	// Seek within the log file
	newPos, err := sw.logFile.file.Seek(offset, whence)
	if err != nil {
		return 0, err
	}
	sw.logFile.position = newPos
	// Don't update endPos on seek - only track actual writes
	return newPos, nil
}

// Close writes the snapshot Entry metadata and unlocks the log file.
// The Entry is written at the end of the snapshot data (endPos).
func (sw *SnapshotWriter) Close() error {
	if sw.closed {
		return nil
	}
	sw.closed = true
	defer sw.logFile.mu.Unlock()

	// Seek to end of snapshot data to write Entry
	// (builder may have seeked back to write header)
	_, err := sw.logFile.file.Seek(sw.endPos, io.SeekStart)
	if err != nil {
		return fmt.Errorf("failed to seek to end of snapshot: %w", err)
	}
	sw.logFile.position = sw.endPos

	// Create snapshot entry pointing to the snapshot data
	snapPos := sw.startPos
	entry := &Entry{
		Commit:     sw.commit,
		Timestamp:  sw.timestamp,
		Patch:      nil,
		SnapPos:    &snapPos,
		TxSource:   nil,
		LastCommit: nil,
	}

	// Serialize entry to Tony format
	entryBytes, err := entry.ToTony()
	if err != nil {
		return fmt.Errorf("failed to serialize entry: %w", err)
	}

	// Check length fits in uint32
	if len(entryBytes) > 0xFFFFFFFF {
		return fmt.Errorf("entry too large: %d bytes", len(entryBytes))
	}

	// Write 4-byte length prefix
	entryPos := sw.logFile.position
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(entryBytes)))

	// Write length prefix
	if _, err := sw.logFile.file.Write(lenBuf); err != nil {
		return fmt.Errorf("failed to write entry length: %w", err)
	}

	// Write entry data
	if _, err := sw.logFile.file.Write(entryBytes); err != nil {
		return fmt.Errorf("failed to write entry: %w", err)
	}

	sw.logFile.position += int64(4 + len(entryBytes))
	sw.entryPos = entryPos

	return nil
}

// Abandon closes the SnapshotWriter without writing Entry metadata.
// Used when snapshot creation fails and we need to unlock the log file.
func (sw *SnapshotWriter) Abandon() {
	if !sw.closed {
		sw.closed = true
		sw.logFile.mu.Unlock()
	}
}

// EntryPosition returns the position of the Entry in the log (available after Close).
func (sw *SnapshotWriter) EntryPosition() int64 {
	return sw.entryPos
}

// LogFileID returns the log file ID where this snapshot was written.
func (sw *SnapshotWriter) LogFileID() LogFileID {
	return sw.logFileID
}
