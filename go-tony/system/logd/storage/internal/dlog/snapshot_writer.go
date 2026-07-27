package dlog

import (
	"encoding/binary"
	"fmt"
	"io"
)

// BlobHeaderMagic is the magic marker that indicates a blob header follows.
// This is 0xFFFFFFFF which is impossible as a normal entry length (4GB+ entries).
const BlobHeaderMagic uint32 = 0xFFFFFFFF

// BlobHeaderSize is the total size of the blob header (magic + length).
const BlobHeaderSize = 8 // 4 bytes magic + 4 bytes length

// SnapshotWriter is a writer for creating snapshots in the inactive log.
// It implements io.WriteCloser and io.Seeker for use with snap.Builder.
// When closed, it writes the snapshot Entry metadata to the log.
//
// Log format for snapshots:
//
//	[blob header: 8 bytes]     - magic marker (0xFFFFFFFF) + blob length
//	[snapshot data: N bytes]   - binary event stream from snap.Builder
//	[entry: 4+M bytes]         - length prefix + Entry with SnapPos pointing to snapshot data
//
// The blob header allows the dlog iterator to skip over binary snapshot data.
type SnapshotWriter struct {
	dl          *DLog
	logFile     *DLogFile
	logFileID   LogFileID
	headerPos   int64 // where blob header is (for patching length on Close)
	startPos    int64 // where snapshot data starts (after blob header)
	endPos      int64 // where snapshot data ends (tracked on Write)
	commit      int64
	timestamp   string
	entryPos    int64        // set on Close
	closed      bool
	schemaEntry *SchemaEntry // optional schema change entry
	scopeID     *string      // optional scope ID for scoped snapshots
}

// ErrSnapshotInProgress is returned when attempting to start a snapshot
// while another snapshot is already running on the same log file.
var ErrSnapshotInProgress = fmt.Errorf("snapshot already in progress on this log file")

// NewSnapshotWriter creates a writer for building a snapshot in the inactive log.
// Returns ErrSnapshotInProgress if a snapshot is already running on the inactive log.
// The caller should create a snap.Builder with this writer, feed events to it,
// close the builder, then close this writer to finalize the Entry.
func (dl *DLog) NewSnapshotWriter(commit int64, timestamp string) (*SnapshotWriter, error) {
	dl.mu.Lock()
	activeLog := dl.activeLog

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

	// Try to acquire snapMu - don't block if snapshot already in progress
	if !logFileObj.snapMu.TryLock() {
		dl.mu.Unlock()
		return nil, ErrSnapshotInProgress
	}
	dl.mu.Unlock()

	// snapMu is now held - get current position and write blob header placeholder
	logFileObj.mu.Lock()
	headerPos := logFileObj.position

	// Write blob header placeholder: [magic marker][placeholder length]
	// The actual length will be patched in Close() once we know the blob size
	header := make([]byte, BlobHeaderSize)
	binary.BigEndian.PutUint32(header[0:4], BlobHeaderMagic)
	binary.BigEndian.PutUint32(header[4:8], 0) // placeholder, will be patched

	if _, err := logFileObj.file.WriteAt(header, headerPos); err != nil {
		logFileObj.mu.Unlock()
		logFileObj.snapMu.Unlock()
		return nil, fmt.Errorf("failed to write blob header: %w", err)
	}
	logFileObj.position = headerPos + BlobHeaderSize
	startPos := logFileObj.position // snapshot data starts after header
	logFileObj.mu.Unlock()

	return &SnapshotWriter{
		dl:        dl,
		logFile:   logFileObj,
		logFileID: inactiveLog,
		headerPos: headerPos,
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
	n, err = sw.logFile.file.WriteAt(p, sw.logFile.position)
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

// Seek implements io.Seeker.
//
// The cursor is virtual: it moves logFile.position only, and never seeks the shared file
// descriptor. snap.Builder seeks backwards to patch its own header, and doing that on the
// real fd would relocate the append point for anyone else holding the file.
func (sw *SnapshotWriter) Seek(offset int64, whence int) (int64, error) {
	if sw.closed {
		return 0, fmt.Errorf("seek on closed SnapshotWriter")
	}
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = sw.logFile.position + offset
	case io.SeekEnd:
		stat, err := sw.logFile.file.Stat()
		if err != nil {
			return 0, fmt.Errorf("failed to stat log file: %w", err)
		}
		newPos = stat.Size() + offset
	default:
		return 0, fmt.Errorf("invalid whence %d", whence)
	}
	if newPos < 0 {
		return 0, fmt.Errorf("negative seek position %d", newPos)
	}
	sw.logFile.position = newPos
	// Don't update endPos on seek - only track actual writes
	return newPos, nil
}

// Close writes the snapshot Entry metadata and releases the snapshot lock.
// The Entry is written at the end of the snapshot data (endPos).
// Also patches the blob header with the actual blob length.
func (sw *SnapshotWriter) Close() error {
	if sw.closed {
		return nil
	}
	sw.closed = true
	defer sw.logFile.snapMu.Unlock()

	// Calculate blob length (snapshot data only, not including header or entry)
	blobLength := sw.endPos - sw.startPos

	// Patch the blob header with the actual length, in place at headerPos+4 (skipping the
	// magic marker). This in-place patch is why the log cannot be opened with O_APPEND.
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(blobLength))
	if _, err := sw.logFile.file.WriteAt(lenBuf, sw.headerPos+4); err != nil {
		return fmt.Errorf("failed to patch blob header length: %w", err)
	}

	// The Entry goes after the snapshot data; the builder may have left the cursor
	// somewhere earlier, so reset it to the high-water mark.
	sw.logFile.position = sw.endPos

	// Create snapshot entry pointing to the snapshot data (after blob header)
	snapPos := sw.startPos
	entry := &Entry{
		Commit:      sw.commit,
		Timestamp:   sw.timestamp,
		Patch:       nil,
		SnapPos:     &snapPos,
		TxSource:    nil,
		LastCommit:  nil,
		ScopeID:     sw.scopeID,
		SchemaEntry: sw.schemaEntry,
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

	// Frame the entry in one buffer and write it in a single call, as AppendEntry does.
	entryPos := sw.logFile.position
	rec := make([]byte, 4+len(entryBytes))
	binary.BigEndian.PutUint32(rec[:4], uint32(len(entryBytes)))
	copy(rec[4:], entryBytes)

	if _, err := sw.logFile.file.WriteAt(rec, entryPos); err != nil {
		return fmt.Errorf("failed to write entry at %d: %w", entryPos, err)
	}

	sw.logFile.position = entryPos + int64(len(rec))
	sw.entryPos = entryPos

	return nil
}

// Abandon closes the SnapshotWriter without writing Entry metadata.
// Used when snapshot creation fails and we need to release the snapshot lock.
func (sw *SnapshotWriter) Abandon() {
	if !sw.closed {
		sw.closed = true
		sw.logFile.snapMu.Unlock()
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

// SetSchemaEntry sets the schema entry for this snapshot.
// Must be called before Close().
func (sw *SnapshotWriter) SetSchemaEntry(schemaEntry *SchemaEntry) {
	sw.schemaEntry = schemaEntry
}

// SetScopeID sets the scope ID for this snapshot.
// Must be called before Close().
func (sw *SnapshotWriter) SetScopeID(scopeID *string) {
	sw.scopeID = scopeID
}
